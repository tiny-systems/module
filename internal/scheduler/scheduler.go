package scheduler

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/pkg/errors"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/manager"
	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/internal/tracker"
	"github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/utils"
	"golang.org/x/sync/errgroup"
	"time"
)

type Scheduler interface {
	//Install makes component available for running nodes
	Install(component module.Component) error
	//Upsert creates a new instance by using unique name, if instance is already running - updates it
	Upsert(node *v1alpha1.TinyNode) error
	//Invoke sends data to the port of the given instance
	Invoke(ctx context.Context, name string, port string, data []byte) error
	//Destroy stops the instance
	Destroy(name string) error
	//Start starts scheduler
	Start(ctx context.Context, wg *errgroup.Group, inputCh chan *runner.Msg, outputCh chan *runner.Msg, callbacks ...tracker.Callback) error
}

type instanceRequest struct {
	node  *v1alpha1.TinyNode
	err   error
	ready chan struct{}
}

type Schedule struct {
	log logr.Logger
	// registered components map
	componentsMap cmap.ConcurrentMap[string, module.Component]

	// instances map
	instancesMap cmap.ConcurrentMap[string, *runner.Runner]

	// instance commands channel
	newInstanceCh chan *instanceRequest

	// resource manager pass over to runners
	manager manager.ResourceInterface

	inputChMap cmap.ConcurrentMap[string, chan *runner.Msg]
}

func New() *Schedule {
	return &Schedule{
		newInstanceCh: make(chan *instanceRequest),
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
func (s *Schedule) Upsert(node *v1alpha1.TinyNode) error {
	req := &instanceRequest{
		node:  node,
		ready: make(chan struct{}),
	}
	s.newInstanceCh <- req
	<-req.ready
	return req.err
}

// Invoke sends data to the port of given instance name @todo
func (s *Schedule) Invoke(ctx context.Context, node string, port string, data []byte) error {

	instanceCh, ok := s.inputChMap.Get(node)
	if !ok {
		return fmt.Errorf("instance not found in a map of registered instances")
	}
	var err error
	ctx, cancel := context.WithTimeout(ctx, time.Second*3)
	defer cancel()
	// send to nodes' personal channel
	instanceCh <- &runner.Msg{
		To:   utils.GetPortFullName(node, port),
		Data: data,
		Callback: func(e error) {
			cancel()
			if e == nil {
				return
			}
			s.log.Error(e, "invoke callback error")
		},
	}
	<-ctx.Done()
	return err
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

func (s *Schedule) Start(ctx context.Context, wg *errgroup.Group, eventBus chan *runner.Msg, outsideCh chan *runner.Msg, callbacks ...tracker.Callback) error {

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	for {
		select {
		case msg := <-eventBus:

			// check subject
			node, _ := utils.ParseFullPortName(msg.To)
			if instanceCh, ok := s.inputChMap.Get(node); ok {
				// send to nodes' personal channel
				instanceCh <- msg
			} else {
				outsideCh <- msg
			}
			//
			// send outside or into some instance
			// decide if we make external request or send im
		case req := <-s.newInstanceCh:

			func() {
				defer func() {
					close(req.ready)
				}()

				cmp, ok := s.componentsMap.Get(req.node.Spec.Component)
				if !ok {
					req.node.Status = v1alpha1.TinyNodeStatus{
						Error: fmt.Sprintf("component %s not found", req.node.Spec.Component),
					}
					return
				}

				s.instancesMap.Upsert(req.node.Name, nil, func(exist bool, instance *runner.Runner, _ *runner.Runner) *runner.Runner {
					//
					if !exist {
						// new instance
						instance = runner.NewRunner(req.node.Name, req.node.Labels[v1alpha1.FlowIDLabel], cmp.Instance(), callbacks...).SetManager(s.manager).SetLogger(s.log)
						wg.Go(func() error {
							// main instance lifetime goroutine
							// exit unregisters instance
							instance.Init(wg, req.node.Labels[v1alpha1.SuggestedHttpPortAnnotation])

							//prepare own channel
							instanceCh := make(chan *runner.Msg)
							// then close
							defer close(instanceCh)

							s.inputChMap.Set(req.node.Name, instanceCh)
							// delete first to avoid writing into closed channel
							defer s.inputChMap.Remove(req.node.Name)

							defer func() {
								s.log.Info("instance stopped", "name", req.node.Name)
							}()

							// process input ports
							err := instance.Process(ctx, instanceCh, eventBus)
							if errors.Is(err, context.Canceled) {
								return nil
							}
							return err
						})
					}

					//configure || reconfigure
					err := instance.Configure(ctx, req.node.DeepCopy(), eventBus)
					if err != nil {
						req.err = fmt.Errorf("configure error: %v", err)
						return instance
					}
					if err = instance.UpdateStatus(&req.node.Status); err != nil {
						req.err = fmt.Errorf("update status error: %v", err)
					}
					return instance
				})

			}()

		case <-ctx.Done():
			// what for others
			s.log.Info("waiting for wg done")
			return wg.Wait()
		}
	}
}
