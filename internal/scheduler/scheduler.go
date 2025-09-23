package scheduler

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/internal/tracker"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/resource"
	"github.com/tiny-systems/module/pkg/utils"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"
	"time"
)

type Scheduler interface {
	//Install makes component available to run instances
	Install(component module.Component) error
	//Update creates a new instance by using unique name, if instance exists - updates one using its specs and signals
	Update(ctx context.Context, node *v1alpha1.TinyNode, signals *v1alpha1.TinySignalList) error
	//Handle sync incoming call
	Handle(ctx context.Context, msg *runner.Msg) (any, error)

	//Destroy stops the instance and deletes it
	Destroy(name string) error
}

type Schedule struct {
	log logr.Logger
	ctx context.Context
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

func New(ctx context.Context, outsideHandler runner.Handler) *Schedule {
	return &Schedule{
		ctx:            ctx,
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
	s.componentsMap.Set(component.GetInfo().Name, component)
	return nil
}

// Handle could be external and synchronous
// @todo use retry with backoff here
func (s *Schedule) Handle(ctx context.Context, msg *runner.Msg) (any, error) {

	nodeName, port := utils.ParseFullPortName(msg.To)
	if port == module.ReconcilePort {
		// system port; do nothing
		return nil, nil
	}

	instance, ok := s.instancesMap.Get(nodeName)

	if !ok || (instance != nil && !instance.HasPort(port)) {
		// instance is not registered currently, or it's port is not yet available (setting did not enable it yet?)
		// maybe reconcile call did not register it yet
		// sleep and try again
		t := time.NewTimer(time.Millisecond)
		defer t.Stop()

		select {
		// what ever happens first
		case <-ctx.Done():
			return nil, nil
		case <-t.C:
			return s.outsideHandler(ctx, msg)
		}
	}
	return s.send(ctx, instance, msg)
}

func (s *Schedule) send(ctx context.Context, instance *runner.Runner, msg *runner.Msg) (any, error) {
	return instance.Input(ctx, msg, func(outCtx context.Context, outMsg *runner.Msg) (any, error) {

		_, port := utils.ParseFullPortName(outMsg.To)
		if port == "" {
			return nil, fmt.Errorf("empty port in handle's output")
		}
		return s.outsideHandler(outCtx, outMsg)
	})
}

func (s *Schedule) Destroy(name string) error {
	if instance, ok := s.instancesMap.Get(name); ok && instance != nil {
		// remove from map first so no one can access it
		s.instancesMap.Remove(name)
		instance.Cancel()
	}
	return nil
}

// Update updates node instance or creates one
func (s *Schedule) Update(ctx context.Context, node *v1alpha1.TinyNode, signals *v1alpha1.TinySignalList) error {

	cmp, ok := s.componentsMap.Get(node.Spec.Component)
	if !ok {
		return fmt.Errorf("component %s is not registered", node.Spec.Component)
	}

	runnerInstance := s.instancesMap.Upsert(node.Name, nil, func(exist bool, runnerInstance *runner.Runner, _ *runner.Runner) *runner.Runner {
		if exist {
			return runnerInstance
		}
		// new instance
		//configure || reconfigure
		return runner.NewRunner(cmp.Instance()).
			SetNode(*node).
			SetLogger(s.log).
			SetTracer(s.tracer).
			SetManager(s.manager).
			SetTracker(s.tracker).
			SetMeter(s.meter)
	})

	var (
		// backup node instance had before
		nodeBackup = runnerInstance.Node()
		atomicErr  = new(atomic.Error)
	)

	ctx = utils.WithLeader(s.ctx, utils.IsLeader(ctx))

	// update system ports
	cmpInstance := runnerInstance.GetComponent()

	// update instance with copy of the node
	runnerInstance.SetNode(*node.DeepCopy())

	for _, p := range cmpInstance.Ports() {
		if p.Source {
			continue
		}

		s.errGroup.Go(func() error {
			// if node has http port then provide addressGetter
			switch p.Name {
			case module.ReconcilePort:
				// component reconciles its node
				_ = cmpInstance.Handle(ctx, nil, p.Name, *node)
			case module.ClientPort:
				_ = cmpInstance.Handle(ctx, nil, p.Name, s.manager)
			}
			return nil
		})
	}

	s.errGroup.Go(func() error {
		// send signal to the settings ports with no data so node will apply own configs (own means "from" is empty for those configs)
		if _, err := s.send(ctx, runnerInstance, &runner.Msg{
			To:   utils.GetPortFullName(node.Name, module.SettingsPort),
			Data: []byte("{}"), // no external data sent to port, rely solely on a node's port config
		}); err != nil {
			atomicErr.Store(err)
			// not allow errGroup fail due to settings
		}
		// configure port errors should not fail the entire error group
		return nil
	})

	if signals != nil {
		// send existing signals to the instance

		for _, signal := range signals.Items {
			// each signal in own goroutine
			s.errGroup.Go(func() error {

				if _, err := s.send(ctx, runnerInstance, &runner.Msg{
					From:  runner.FromSignal,
					To:    utils.GetPortFullName(node.Name, signal.Spec.Port),
					Data:  signal.Spec.Data,
					Nonce: signal.Annotations[v1alpha1.SignalNonceAnnotation],
				}); err != nil {
					atomicErr.Store(err)
				}
				return nil
			})
		}
	}

	// give time to node update itself or fail
	time.Sleep(time.Second)

	var err = runnerInstance.ReadStatus(&node.Status)

	if atomicErr.Load() != nil {
		// set previous node state to the scheduler because configuration did not end well
		runnerInstance.SetNode(nodeBackup)

		node.Status.Status = atomicErr.Load().Error()
		node.Status.Error = true
	}
	// do we need to rebuild status in case of rollback?
	return err
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
