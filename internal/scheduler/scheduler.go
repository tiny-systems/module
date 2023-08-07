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

type IScheduler interface {
	//Install makes component available for running nodes
	Install(component module.Component) error
	//Instance creates a new instance by using unique name, if instance is already running - updates it
	//as soon as node started callback trigger goes
	Instance(name string, node v1alpha1.TinyNodeSpec) (v1alpha1.TinyNodeStatus, error)
	//Invoke sends data to the port
	Invoke(name string, port string, data []byte) (v1alpha1.TinyNodeStatus, error)
	//Destroy stops goroutine
	Destroy(name string) error
	//Start starts scheduler
	Start(ctx context.Context) error
}

type Scheduler struct {
	log               logr.Logger
	componentsMap     cmap.ConcurrentMap[string, module.Component]
	instancesMap      cmap.ConcurrentMap[string, *runner.Runner]
	instanceRequestCh chan *instanceRequest
	ctx               context.Context
}

func New(log logr.Logger) *Scheduler {
	return &Scheduler{
		instanceRequestCh: make(chan *instanceRequest),
		instancesMap:      cmap.New[*runner.Runner](),
		componentsMap:     cmap.New[module.Component](),
		log:               log,
	}
}

func (s *Scheduler) Install(component module.Component) error {
	if component.GetInfo().Name == "" {
		return fmt.Errorf("component name is invalid")
	}
	s.componentsMap.Set(component.GetInfo().Name, component)
	return nil
}

// Instance updates or creates instance
func (s *Scheduler) Instance(name string, node v1alpha1.TinyNodeSpec) (v1alpha1.TinyNodeStatus, error) {
	cmp, ok := s.componentsMap.Get(node.Component)
	if !ok {
		return v1alpha1.TinyNodeStatus{}, fmt.Errorf("component %s is not presented in the module", node.Component)
	}
	statusCh := make(chan v1alpha1.TinyNodeStatus)
	defer close(statusCh)
	s.instanceRequestCh <- &instanceRequest{
		Name:      name,
		Component: cmp,
		Spec:      node,
		StatusCh:  statusCh,
	}
	return <-statusCh, nil
}

// Invoke sends data to the port of given instance name
func (s *Scheduler) Invoke(name string, port string, data []byte) (v1alpha1.TinyNodeStatus, error) {
	return v1alpha1.TinyNodeStatus{}, nil
}

func (s *Scheduler) Destroy(name string) error {
	if instance, ok := s.instancesMap.Get(name); ok && instance != nil {
		//stop
		s.instancesMap.Remove(name)
	}
	return nil
}

func (s *Scheduler) Start(ctx context.Context) error {
	wg, ctx := errgroup.WithContext(ctx)

	outputCh := make(chan *runner.Msg)
	defer close(outputCh)

	for {
		select {

		case output := <-outputCh:
			s.log.Info("output!", "msg", output.Data, "subj", output.Subject)

			node, port := utils.ParseFullPortName(output.Subject)
			if instance, ok := s.instancesMap.Get(node); ok {
				instance.Input(ctx, port, output, outputCh)
			}
			// check subject
			// send outside or into some instance

			// decide if we make external request or send im
		case instanceReq := <-s.instanceRequestCh:
			s.instancesMap.Upsert(instanceReq.Name, nil, func(exist bool, instance *runner.Runner, _ *runner.Runner) *runner.Runner {
				if !exist || instance == nil {
					// new instance
					inputCh := make(chan *runner.Msg)
					instance = runner.NewRunner(instanceReq.Name, inputCh, instanceReq.Component.Instance())
					instance.SetLogger(s.log)
					wg.Go(func() error {
						defer close(inputCh)
						return instance.Process(ctx, outputCh)
					})
				}
				//configure || reconfigure
				if err := instance.Configure(instanceReq.Spec); err != nil {
					s.log.Error(err, "configure error")
				} else if err := instance.Run(ctx, wg, outputCh); err != nil {
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
