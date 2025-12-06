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
	"golang.org/x/sync/errgroup"
	"time"
)

type Scheduler interface {
	//Install makes component available to run instances
	Install(component module.Component) error
	//Update creates a new instance by using unique name, if instance exists - updates one using its specs and signals
	Update(ctx context.Context, node *v1alpha1.TinyNode) error
	//Handle sync incoming call
	Handle(ctx context.Context, msg *runner.Msg) (any, error)

	//Destroy stops the instance and deletes it
	Destroy(name string) error
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
	msgHandler runner.Handler
}

func New(outsideHandler runner.Handler) *Schedule {
	return &Schedule{
		instancesMap:  cmap.New[*runner.Runner](),
		componentsMap: cmap.New[module.Component](),
		errGroup:      &errgroup.Group{},
		msgHandler:    outsideHandler,
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
	if port == v1alpha1.ReconcilePort {
		// system port; do nothing
		return nil, nil
	}

	instance, ok := s.instancesMap.Get(nodeName)

	if !ok || (instance != nil && !instance.HasPort(port)) {
		// instance is not registered currently, or it's port is not yet available (settings did not enable it yet?)
		// maybe reconcile call did not register it yet
		// sleep and try again
		t := time.NewTimer(time.Millisecond)
		defer t.Stop()

		select {
		// what ever happens first
		case <-ctx.Done():
			return nil, nil
		case <-t.C:
			return s.msgHandler(ctx, msg)
		}
	}
	return s.sendMsg(ctx, instance, msg)
}

func (s *Schedule) sendMsg(ctx context.Context, instance *runner.Runner, msg *runner.Msg) (any, error) {
	return instance.MsgHandler(ctx, msg, func(outCtx context.Context, outMsg *runner.Msg) (any, error) {
		// output
		_, port := utils.ParseFullPortName(outMsg.To)
		if port == "" {
			return nil, fmt.Errorf("empty port in handle's output")
		}
		return s.msgHandler(outCtx, outMsg)
	})
}

func (s *Schedule) Destroy(name string) error {
	if instance, ok := s.instancesMap.Get(name); ok && instance != nil {
		// remove from map first so no one can access it
		s.instancesMap.Remove(name)
		instance.Stop()
	}
	return nil
}

// Update updates node instance or creates one based on tinyNode crd
// updates status resource based on outcome of Update
func (s *Schedule) Update(ctx context.Context, node *v1alpha1.TinyNode) error {

	cmp, ok := s.componentsMap.Get(node.Spec.Component)
	if !ok {
		// not recoverable error
		return fmt.Errorf("component %s is not registered", node.Spec.Component)
	}

	// get or create

	runnerInstance := s.instancesMap.Upsert(node.Name, nil, func(exist bool, runnerInstance *runner.Runner, _ *runner.Runner) *runner.Runner {
		if exist {
			return runnerInstance
		}
		// new instance
		return runner.NewRunner(cmp.Instance()).
			SetNode(*node).
			SetLogger(s.log).
			SetTracer(s.tracer).
			SetManager(s.manager).
			SetTracker(s.tracker).
			SetMeter(s.meter)
	})

	// backup if new not will node work to avoid disruption to working system due to faulty settings
	var nodeBackup = runnerInstance.Node()

	// update instance with copy of the new node
	runnerInstance.SetNode(*node.DeepCopy())

	var err error

	defer func() {
		if err == nil {
			return
		}
		// instance failed with new version of node, backup

		runnerInstance.SetNode(nodeBackup)
		node.Status.Status = err.Error()
		node.Status.Error = true
	}()

	// update system ports
	cmpInstance := runnerInstance.GetComponent()

	for _, p := range cmpInstance.Ports() {
		if p.Source {
			continue
		}

		var portResp any

		// if node has http port then provide addressGetter
		switch p.Name {
		case v1alpha1.ReconcilePort:
			// component can use copy of node for its reconciliation
			// it can not update node as it may take, and we try to make Update process as fast as possible
			portResp = cmpInstance.Handle(ctx, runnerInstance.DataHandler(s.msgHandler), p.Name, *node)

		case v1alpha1.ClientPort:
			// provide kubernetes resource manager client
			portResp = cmpInstance.Handle(ctx, nil, p.Name, s.manager)
		}

		respErr := utils.CheckForError(portResp)
		if respErr == nil {
			continue
		}
		err = respErr

		return nil
	}

	// sendMsg signal to the settings ports with no data so node will apply own configs (own means "from" is empty for those configs)
	if _, err = s.sendMsg(ctx, runnerInstance, &runner.Msg{
		To:   utils.GetPortFullName(node.Name, v1alpha1.SettingsPort),
		Data: []byte("{}"), // no external data sent to port, rely solely on a node's port config
	}); err != nil {
		return nil
	}

	// do we need to rebuild status in case of rollback?
	return runnerInstance.ReadStatus(&node.Status)
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
