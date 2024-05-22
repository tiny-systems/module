package scheduler

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/resource"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/internal/tracker"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/utils"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"
	"strconv"
	"strings"
	"time"
)

type Scheduler interface {
	//Install makes component available to run instances
	Install(component module.Component) error

	//Update creates a new instance by using unique name, if instance exists - updates one
	Update(ctx context.Context, node *v1alpha1.TinyNode) error
	//Handle sync incoming call

	Handle(ctx context.Context, msg *runner.Msg) error
	//HandleInternal same as Handle but does not wait until port unblock and does not fall back msg outside
	HandleInternal(ctx context.Context, msg *runner.Msg) error
	//Destroy stops the instance and deletes it
	Destroy(name string) error
	//Start starts scheduler
	Start(ctx context.Context) error
}

type Schedule struct {
	log logr.Logger
	// registered components map
	componentsMap cmap.ConcurrentMap[string, module.Component]
	//
	// instances map
	instancesMap cmap.ConcurrentMap[string, *runner.Runner]

	// K8s resource manager pass over to runners
	manager resource.ManagerInterface
	// open telemetry tracer
	tracer trace.Tracer
	// open telemetry metric meter
	meter metric.Meter

	tracker tracker.Manager

	// meant to be for all background processes
	errGroup *errgroup.Group
	// pretty much self-explanatory
	outsideHandler runner.Handler
}

func New(outsideHandler runner.Handler) *Schedule {
	return &Schedule{
		instancesMap:   cmap.New[*runner.Runner](),
		componentsMap:  cmap.New[module.Component](),
		errGroup:       &errgroup.Group{},
		outsideHandler: outsideHandler,
	}
}

func (s *Schedule) SetLogger(l logr.Logger) *Schedule {
	s.log = l
	return s
}

func (s *Schedule) SetManager(m resource.ManagerInterface) *Schedule {
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

func (s *Schedule) SetTracker(t tracker.Manager) *Schedule {
	s.tracker = t
	return s
}

func (s *Schedule) Install(component module.Component) error {
	if component.GetInfo().Name == "" {
		return fmt.Errorf("component name is invalid")
	}
	s.componentsMap.Set(utils.SanitizeResourceName(component.GetInfo().Name), component)
	return nil
}

// HandleInternal only local and async
func (s *Schedule) HandleInternal(ctx context.Context, msg *runner.Msg) error {
	nodeName, _ := utils.ParseFullPortName(msg.To)
	instance, ok := s.instancesMap.Get(nodeName)
	if !ok {
		return fmt.Errorf("node %s not found", nodeName)
	}
	s.errGroup.Go(func() error {
		return s.send(ctx, instance, msg)
	})
	return nil
}

// Handle could be external and synchronous
func (s *Schedule) Handle(ctx context.Context, msg *runner.Msg) error {
	nodeName, port := utils.ParseFullPortName(msg.To)

	if port == module.ReconcilePort {
		// system port; do nothing
		return nil
	}
	instance, ok := s.instancesMap.Get(nodeName)
	if !ok {
		// instance is not registered currently
		// maybe reconcile call did not register it yet
		// sleep and try again
		t := time.NewTimer(time.Millisecond * 100)
		defer t.Stop()

		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			return s.outsideHandler(ctx, msg)
		}
	}
	return s.send(ctx, instance, msg)
}

func (s *Schedule) send(ctx context.Context, instance *runner.Runner, msg *runner.Msg) error {
	return instance.Input(ctx, msg, func(outCtx context.Context, outMsg *runner.Msg) error {
		return s.handleOutput(outCtx, instance, outMsg)
	})
}

func (s *Schedule) handleOutput(outCtx context.Context, instance *runner.Runner, outMsg *runner.Msg) error {
	node := instance.Node()
	// output
	_, p := utils.ParseFullPortName(outMsg.To)

	if p == module.ReconcilePort {
		// create tiny signal instead which will trigger reconcile
		if err := s.manager.CreateClusterNodeSignal(context.Background(), node, p, outMsg.Data); err != nil {
			return fmt.Errorf("create signal error: %v", err)
		}
	}
	return s.outsideHandler(outCtx, outMsg)
}

func (s *Schedule) Destroy(name string) error {
	s.log.Info("destroy", "node", name)
	if instance, ok := s.instancesMap.Get(name); ok && instance != nil {
		// remove from map first so no one can access it
		s.instancesMap.Remove(name)
		instance.Cancel()
	}
	return nil
}

// Update updates node instance or creates one
func (s *Schedule) Update(ctx context.Context, node *v1alpha1.TinyNode) error {
	cmp, ok := s.componentsMap.Get(node.Spec.Component)
	if !ok {
		return fmt.Errorf("component %s is not registered", node.Spec.Component)
	}
	runnerInstance := s.instancesMap.Upsert(node.Name, nil, func(exist bool, runnerInstance *runner.Runner, _ *runner.Runner) *runner.Runner {
		//
		if exist {
			return runnerInstance
		}
		// new instance
		cmpInstance := cmp.Instance()

		runnerInstance = runner.NewRunner(node.Name, cmpInstance).
			SetLogger(s.log).
			SetTracer(s.tracer).
			SetTracker(s.tracker).
			SetMeter(s.meter)

		s.errGroup.Go(func() error {
			return s.initInstance(context.Background(), node, cmpInstance)
		})

		//configure || reconfigure
		return runnerInstance
	})

	var (
		// backup node instance had before
		nodeBackup = runnerInstance.Node()
	)
	// update instance

	runnerInstance.SetNode(*node.DeepCopy())
	var atomicErr = new(atomic.Error)

	s.errGroup.Go(func() error {
		atomicErr.Store(s.send(ctx, runnerInstance, &runner.Msg{
			EdgeID: utils.GetPortFullName(node.Name, module.SettingsPort),
			To:     utils.GetPortFullName(node.Name, module.SettingsPort),
			Data:   []byte("{}"), // no external data sent to port, rely solely on a node's port config
		}))
		// configure port errors should not fail the entire error group
		return nil
	})

	// give time to node update itself or fail
	time.Sleep(time.Millisecond * 3 * 100)

	var err = runnerInstance.UpdateStatus(&node.Status)

	if atomicErr.Load() != nil {
		// set previous node state to the scheduler because configuration did not end well
		runnerInstance.SetNode(nodeBackup)

		node.Status.Status = atomicErr.Load().Error()
		node.Status.Error = true
	}
	// do we need to rebuild status in case of rollback?
	return err
}

func (s *Schedule) initInstance(ctx context.Context, node *v1alpha1.TinyNode, componentInstance module.Component) error {
	// init system ports
	ctx, cancel := context.WithTimeout(ctx, time.Second*15)
	defer cancel()
	for _, p := range componentInstance.Ports() {
		// if node has http port then provide addressGetter
		if p.Name == module.HttpPort {
			//module.ListenAddressGetter
			_ = componentInstance.Handle(ctx, nil, module.HttpPort, s.getAddressGetter(node))
		}
	}
	return nil
}

func (s *Schedule) getAddressGetter(node *v1alpha1.TinyNode) module.ListenAddressGetter {

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
	s.log.Info("shutting down all scheduler instances")
	for _, name := range s.instancesMap.Keys() {
		_ = s.Destroy(name)
	}
	s.log.Info("scheduler is waiting for errgroup done")
	return s.errGroup.Wait()
}
