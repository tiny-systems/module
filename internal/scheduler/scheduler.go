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
	instanceRequestCh chan instanceRequest
	ctx               context.Context
}

func New(log logr.Logger) *Schedule {
	return &Schedule{
		instanceRequestCh: make(chan instanceRequest),
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
	statusCh := make(chan v1alpha1.TinyNodeStatus)
	defer close(statusCh)

	s.instanceRequestCh <- instanceRequest{
		Node:     node,
		StatusCh: statusCh,
	}
	return <-statusCh, nil
}

// Invoke sends data to the port of given instance name
func (s *Schedule) Invoke(name string, port string, data []byte) (v1alpha1.TinyNodeStatus, error) {
	return v1alpha1.TinyNodeStatus{}, nil
}

func (s *Schedule) Destroy(name string) error {
	if instance, ok := s.instancesMap.Get(name); ok && instance != nil {
		// remove from map
		s.instancesMap.Remove(name)
		return instance.Destroy()
	}
	return nil
}

func (s *Schedule) Start(ctx context.Context) error {

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

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

		case req := <-s.instanceRequestCh:

			s.log.Info("create/update instance", "name", req.Node.Name)
			cmp, ok := s.componentsMap.Get(req.Node.Spec.Component)
			if !ok {
				req.StatusCh <- v1alpha1.TinyNodeStatus{Error: fmt.Sprintf("component %s is not presented in the module", req.Node.Spec.Component)}
			}

			s.instancesMap.Upsert(req.Node.Name, nil, func(exist bool, instance *runner.Runner, _ *runner.Runner) *runner.Runner {
				if !exist {
					// new instance
					instance = runner.NewRunner(req.Node.Name, cmp.Instance()).SetLogger(s.log)
					wg.Go(func() error {
						// main lifetime goroutine
						// exit unregisters instance
						//
						inputCh := make(chan *runner.Msg)
						// then close
						defer close(inputCh)
						// delete first
						inputChMap.Set(req.Node.Name, inputCh)
						//
						defer inputChMap.Remove(req.Node.Name)
						// process input ports
						return instance.Process(ctx, inputCh, eventBus)
					})
				}
				s.log.Info("configuring")
				//configure || reconfigure
				if err := instance.Configure(ctx, req.Node.Spec, eventBus); err != nil {
					s.log.Error(err, "configure error", "node", req.Node.Name)
				}
				s.log.Info("running")
				if err := instance.Run(ctx, wg, eventBus); err != nil {
					s.log.Error(err, "run error")
				}
				//as instance for status
				req.StatusCh <- instance.GetStatus()
				return instance
			})

		case <-ctx.Done():
			// what for others
			return wg.Wait()
		}
	}
}
