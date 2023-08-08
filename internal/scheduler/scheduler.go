package scheduler

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/utils"
	"golang.org/x/sync/errgroup"
)

type Scheduler interface {
	//Install makes component available for running nodes
	Install(component module.Component) error
	//Instance creates a new instance by using unique name, if instance is already running - updates it
	//as soon as node started callback trigger goes
	Instance(node v1alpha1.TinyNode) (v1alpha1.TinyNodeStatus, error)
	//Invoke sends data to the port
	Invoke(name string, port string, data []byte) (v1alpha1.TinyNodeStatus, error)
	//Destroy stops goroutine
	Destroy(name string) error
	//Start starts scheduler
	Start(ctx context.Context) error
}

type Schedule struct {
	log               logr.Logger
	componentsMap     cmap.ConcurrentMap[string, module.Component]
	instancesMap      cmap.ConcurrentMap[string, *runner.Runner]
	instanceRequestCh chan *instanceRequest
	ctx               context.Context
}

func New(log logr.Logger) *Schedule {
	return &Schedule{
		instanceRequestCh: make(chan *instanceRequest),
		instancesMap:      cmap.New[*runner.Runner](),
		componentsMap:     cmap.New[module.Component](),
		log:               log,
	}
}

func (s *Schedule) Install(component module.Component) error {
	if component.GetInfo().Name == "" {
		return fmt.Errorf("component name is invalid")
	}
	s.componentsMap.Set(component.GetInfo().Name, component)
	return nil
}

// Instance updates or creates instance
func (s *Schedule) Instance(node v1alpha1.TinyNode) (v1alpha1.TinyNodeStatus, error) {
	cmp, ok := s.componentsMap.Get(node.Spec.Component)
	if !ok {
		return v1alpha1.TinyNodeStatus{}, fmt.Errorf("component %s is not presented in the module", node.Spec.Component)
	}
	statusCh := make(chan v1alpha1.TinyNodeStatus)
	defer close(statusCh)
	s.instanceRequestCh <- &instanceRequest{
		Name:      node.Name,
		Component: cmp,
		Spec:      node.Spec,
		StatusCh:  statusCh,
	}
	return <-statusCh, nil
}

// Invoke sends data to the port of given instance name
func (s *Schedule) Invoke(name string, port string, data []byte) (v1alpha1.TinyNodeStatus, error) {
	return v1alpha1.TinyNodeStatus{}, nil
}

func (s *Schedule) Destroy(name string) error {
	if instance, ok := s.instancesMap.Get(name); ok && instance != nil {
		//stop
		s.instancesMap.Remove(name)
	}
	return nil
}

func (s *Schedule) Start(ctx context.Context) error {
	wg, ctx := errgroup.WithContext(ctx)

	eventBus := make(chan *runner.Msg)
	defer close(eventBus)

	inputChMap := cmap.New[chan *runner.Msg]()
	for {
		select {

		case msg := <-eventBus:
			// check subject
			node, _ := utils.ParseFullPortName(msg.To)
			if instanceCh, ok := inputChMap.Get(node); ok {
				// send to nodes' personal channel
				instanceCh <- msg
			} else {
				s.log.Error(fmt.Errorf("unable to find node: %s", node), "unable to find node")
			}
			//
			// send outside or into some instance
			// decide if we make external request or send im

		case instanceReq := <-s.instanceRequestCh:
			s.log.Info("create/update instance", "name", instanceReq.Name)

			s.instancesMap.Upsert(instanceReq.Name, nil, func(exist bool, instance *runner.Runner, _ *runner.Runner) *runner.Runner {
				if !exist || instance == nil {
					// new instance

					instance = runner.NewRunner(instanceReq.Name, instanceReq.Component.Instance()).SetLogger(s.log)

					wg.Go(func() error {
						inputCh := make(chan *runner.Msg)
						// then close
						defer close(inputCh)
						// delete first
						inputChMap.Set(instanceReq.Name, inputCh)
						defer inputChMap.Remove(instanceReq.Name)
						return instance.Process(ctx, inputCh, eventBus)
					})

				}
				//configure || reconfigure
				if err := instance.Configure(instanceReq.Spec, eventBus); err != nil {
					s.log.Error(err, "configure error", "node", instanceReq.Name)
				}
				if err := instance.Run(ctx, wg, eventBus); err != nil {
					s.log.Error(err, "run error")
				}
				//as instance for status
				instanceReq.StatusCh <- instance.GetStatus()
				return instance
			})

		case <-ctx.Done():
			return wg.Wait()
		}
	}
}
