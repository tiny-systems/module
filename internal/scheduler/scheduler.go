package scheduler

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/manager"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/internal/tracker"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/utils"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
	"strconv"
	"strings"
	"time"
)

type Scheduler interface {
	//Install makes component available for running nodes
	Install(component module.Component) error

	//Upsert creates a new instance by using unique name, if instance is already running - updates it
	Upsert(ctx context.Context, node *v1alpha1.TinyNode) error

	//Handle incoming
	Handle(ctx context.Context, msg *runner.Msg) error
	//Destroy stops the instance
	Destroy(name string) error
	//Start starts scheduler
	Start(ctx context.Context) error
}

type Schedule struct {
	log logr.Logger
	// registered components map
	componentsMap cmap.ConcurrentMap[string, module.Component]

	// instances map
	instancesMap cmap.ConcurrentMap[string, *runner.Runner]

	// resource manager pass over to runners
	manager manager.ResourceInterface

	tracer trace.Tracer
	meter  metric.Meter

	errGroup *errgroup.Group

	outsideHandler runner.Handler
	callbacks      []tracker.Callback
}

func New(outsideHandler runner.Handler, callbacks ...tracker.Callback) *Schedule {
	return &Schedule{
		instancesMap:   cmap.New[*runner.Runner](),
		componentsMap:  cmap.New[module.Component](),
		errGroup:       &errgroup.Group{},
		outsideHandler: outsideHandler,
		callbacks:      callbacks,
	}
}

func (s *Schedule) SetLogger(l logr.Logger) *Schedule {
	s.log = l
	return s
}

func (s *Schedule) SetManager(m manager.ResourceInterface) *Schedule {
	s.manager = m
	return s
}

func (s *Schedule) SetMeter(m metric.Meter) *Schedule {
	s.meter = m
	return s
}

func (s *Schedule) SetTracer(t trace.Tracer) *Schedule {
	s.tracer = t
	return s
}

func (s *Schedule) Install(component module.Component) error {
	if component.GetInfo().Name == "" {
		return fmt.Errorf("component name is invalid")
	}
	s.componentsMap.Set(utils.SanitizeResourceName(component.GetInfo().Name), component)
	return nil
}

// Handle handles incoming
func (s *Schedule) Handle(ctx context.Context, msg *runner.Msg) error {
	//ctx context.Context, node string, port string, data []byte
	nodeName, port := utils.ParseFullPortName(msg.To)

	if port == module.RefreshPort {
		// system port; do nothing
		return nil
	}

	instance, ok := s.instancesMap.Get(nodeName)
	if !ok {
		// send outside
		return s.outsideHandler(ctx, msg)
	}
	node := instance.Node()

	// send to nodes' personal channel
	return instance.Input(ctx, msg, func(outCtx context.Context, outMsg *runner.Msg) error {
		// output
		_, p := utils.ParseFullPortName(outMsg.To)

		if p == module.RefreshPort {
			// create tiny signal instead
			if err := s.manager.CreateClusterNodeSignal(context.Background(), node, p, outMsg.Data); err != nil {
				return err
			}
		}

		// we can say now that data successfully applied to input port
		// input ports always have no errors
		for _, callback := range s.callbacks {
			callback(tracker.PortMsg{
				NodeName:  node.Name,
				EdgeID:    outMsg.EdgeID,
				PortName:  outMsg.From, // INPUT PORT OF THE NODE
				Data:      outMsg.Data,
				FlowID:    node.Labels[v1alpha1.FlowIDLabel],
				NodeStats: instance.GetStats(),
			})
		}
		// run itself
		return s.Handle(outCtx, outMsg)
	})
}

func (s *Schedule) Destroy(name string) error {
	s.log.Info("destroy", "node", name)
	if instance, ok := s.instancesMap.Get(name); ok && instance != nil {
		// remove from map
		s.instancesMap.Remove(name)
		instance.Cancel()
	}
	return nil
}

func (s *Schedule) Upsert(ctx context.Context, node *v1alpha1.TinyNode) (err error) {
	cmp, ok := s.componentsMap.Get(node.Spec.Component)
	if !ok {
		return fmt.Errorf("component %s is not registered", node.Spec.Component)
	}

	s.instancesMap.Upsert(node.Name, nil, func(exist bool, instance *runner.Runner, _ *runner.Runner) *runner.Runner {
		//
		if !exist {
			// new instance
			cmpInstance := cmp.Instance()

			instance = runner.NewRunner(node.Name, cmpInstance).
				SetLogger(s.log).
				SetTracer(s.tracer).
				SetMeter(s.meter)

			s.errGroup.Go(func() error {
				return s.run(node, cmpInstance)
			})
		}
		//configure || reconfigure
		err = instance.Configure(ctx, *node.DeepCopy())
		if err != nil {
			err = fmt.Errorf("configure error: %v", err)
		}
		if err = instance.UpdateStatus(&node.Status); err != nil {
			err = fmt.Errorf("update status error: %v", err)
		}
		return instance
	})
	return err
}

func (s *Schedule) run(node *v1alpha1.TinyNode, component module.Component) error {

	for _, p := range component.Ports() {
		if p.Name == module.HttpPort {

			//module.ListenAddressGetter
			_ = component.Handle(context.Background(), nil, module.HttpPort, s.getListener(node))
			return nil
		}
	}
	return nil
}

func (s *Schedule) getListener(node *v1alpha1.TinyNode) module.ListenAddressGetter {

	var (
		suggestedPortStr = node.Labels[v1alpha1.SuggestedHttpPortAnnotation]
	)

	return func() (suggestedPort int, upgrade module.AddressUpgrade) {

		if annotationPort, err := strconv.Atoi(suggestedPortStr); err == nil {
			suggestedPort = annotationPort
		}

		return suggestedPort, func(ctx context.Context, auto bool, hostnames []string, actualLocalPort int) ([]string, error) {
			// limit exposing with a timeout
			exposeCtx, cancel := context.WithTimeout(ctx, time.Second*10)
			defer cancel()

			var err error
			// upgrade
			// hostname it's a last part of the node name
			var autoHostName string

			if auto {
				autoHostNameParts := strings.Split(node.Name, ".")
				autoHostName = autoHostNameParts[len(autoHostNameParts)-1]
			}

			publicURLs, err := s.manager.ExposePort(exposeCtx, autoHostName, hostnames, actualLocalPort)
			if err != nil {
				return []string{}, err
			}

			s.errGroup.Go(func() error {
				// listen when emit ctx ends to clean up
				<-ctx.Done()
				s.log.Info("cleaning up exposed port")

				discloseCtx, cancel := context.WithTimeout(context.Background(), time.Second*10)
				defer cancel()
				if err := s.manager.DisclosePort(discloseCtx, actualLocalPort); err != nil {
					s.log.Error(err, "unable to disclose port", "port", actualLocalPort)
				}
				return nil
			})
			return publicURLs, err
		}
	}
}

func (s *Schedule) Start(ctx context.Context) error {

	<-ctx.Done()
	s.log.Info("waiting for wg done")
	return s.errGroup.Wait()
}
