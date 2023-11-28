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
	"golang.org/x/sync/errgroup"
)

type Scheduler interface {
	//Install makes component available for running nodes
	Install(component module.Component) error
	//Upsert creates a new instance by using unique name, if instance is already running - updates it
	Upsert(node v1alpha1.TinyNode) (v1alpha1.TinyNodeStatus, error)
	//Invoke sends data to the port of the given instance
	Invoke(name string, port string, data []byte) (v1alpha1.TinyNodeStatus, error)
	//Destroy stops the instance
	Destroy(name string) error
	//Start starts scheduler
	Start(ctx context.Context, inputCh chan *runner.Msg, outputCh chan *runner.Msg, callbacks ...tracker.Callback) error
}

type Schedule struct {
	log logr.Logger
	// registered components map
	componentsMap cmap.ConcurrentMap[string, module.Component]

	// instances map
	instancesMap cmap.ConcurrentMap[string, *runner.Runner]

	// instance commands channel
	newInstanceCh chan instanceRequest

	// resource manager pass over to runners
	manager manager.ResourceInterface

	inputChMap cmap.ConcurrentMap[string, chan *runner.Msg]
}

func New() *Schedule {
	return &Schedule{
		newInstanceCh: make(chan instanceRequest),
		instancesMap:  cmap.New[*runner.Runner](),
		componentsMap: cmap.New[module.Component](),
		inputChMap:    cmap.New[chan *runner.Msg](),
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

func (s *Schedule) Install(component module.Component) error {
	if component.GetInfo().Name == "" {
		return fmt.Errorf("component name is invalid")
	}
	s.componentsMap.Set(utils.SanitizeResourceName(component.GetInfo().Name), component)
	return nil
}

// Upsert updates or creates instance
func (s *Schedule) Upsert(node v1alpha1.TinyNode) (v1alpha1.TinyNodeStatus, error) {
	statusCh := make(chan v1alpha1.TinyNodeStatus)
	defer close(statusCh)

	s.newInstanceCh <- instanceRequest{
		Node:     node,
		StatusCh: statusCh,
	}
	return <-statusCh, nil
}

// Invoke sends data to the port of given instance name @todo
func (s *Schedule) Invoke(node string, port string, data []byte) (v1alpha1.TinyNodeStatus, error) {

	if instanceCh, ok := s.inputChMap.Get(node); ok {
		// send to nodes' personal channel

		instanceCh <- &runner.Msg{
			To:       utils.GetPortFullName(node, port),
			Data:     data,
			Callback: runner.EmptyCallback,
		}
		return v1alpha1.TinyNodeStatus{}, nil
	}
	return v1alpha1.TinyNodeStatus{}, fmt.Errorf("node not found")
}

func (s *Schedule) Destroy(name string) error {
	s.log.Info("destroy", "node", name)
	if instance, ok := s.instancesMap.Get(name); ok && instance != nil {
		// remove from map
		s.instancesMap.Remove(name)
		return instance.Destroy()
	}
	return nil
}

func (s *Schedule) Start(ctx context.Context, eventBus chan *runner.Msg, outsideCh chan *runner.Msg, callbacks ...tracker.Callback) error {

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	wg, ctx := errgroup.WithContext(ctx)

	for {
		select {
		case msg := <-eventBus:

			// check subject
			node, _ := utils.ParseFullPortName(msg.To)

			if instanceCh, ok := s.inputChMap.Get(node); ok {
				// send to nodes' personal channel
				instanceCh <- msg
			} else {
				s.log.Info("instance not found in a local map of channels", "node", node)
				outsideCh <- msg
			}
			//
			// send outside or into some instance
			// decide if we make external request or send im
		case req := <-s.newInstanceCh:

			cmp, ok := s.componentsMap.Get(req.Node.Spec.Component)
			if !ok {
				req.StatusCh <- v1alpha1.TinyNodeStatus{
					Error: fmt.Sprintf("component %s not found", req.Node.Spec.Component),
				}
				continue
			}
			s.instancesMap.Upsert(req.Node.Name, nil, func(exist bool, instance *runner.Runner, _ *runner.Runner) *runner.Runner {
				//
				if !exist {
					// new instance
					instance = runner.NewRunner(req.Node, cmp.Instance(), callbacks...).
						// add K8s resource manager
						SetManager(s.manager).
						SetLogger(s.log)

					wg.Go(func() error {
						// main instance lifetime goroutine
						// exit unregisters instance
						//
						inputCh := make(chan *runner.Msg)
						// then close
						defer close(inputCh)

						s.inputChMap.Set(req.Node.Name, inputCh)
						// delete first to avoid writing into closed channel
						defer s.inputChMap.Remove(req.Node.Name)

						defer func() {
							s.log.Info("instance stopped", "name", req.Node.Name)
						}()

						// process input ports
						return instance.Process(ctx, wg, inputCh, eventBus)
					})
				}
				//configure || reconfigure
				if err := instance.Configure(ctx, req.Node, eventBus); err != nil {
					s.log.Error(err, "configure error", "node", req.Node.Name)
				}

				req.StatusCh <- instance.GetStatus()
				return instance
			})

		case <-ctx.Done():
			// what for others
			return wg.Wait()
		}
	}
}
