package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/spyzhov/ajson"
	"github.com/tiny-systems/errorpanic"
	"github.com/tiny-systems/module/api/v1alpha1"
	m "github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/evaluator"
	"github.com/tiny-systems/module/pkg/schema"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"
	"reflect"
	"sort"
	"sync"
	"time"
)

type Runner struct {
	// unique instance name
	name string
	//
	specLock *sync.RWMutex
	spec     v1alpha1.TinyNodeSpec
	//
	component m.Component
	//
	log        logr.Logger
	runError   *atomic.Error
	cancelFunc context.CancelFunc

	//stats cmap.ConcurrentMap[string, *int64]
	needRestart bool
	inputCh     chan *Msg
}

func NewRunner(name string, inputCh chan *Msg, component m.Component) *Runner {
	return &Runner{
		name:      name,
		component: component,
		//stats:     cmap.New[*int64](),
		specLock: new(sync.RWMutex),
		inputCh:  inputCh,
		runError: atomic.NewError(nil),
	}
}

func (c *Runner) SetLogger(l logr.Logger) *Runner {
	c.log = l
	return c
}

func (c *Runner) GetStatus() v1alpha1.TinyNodeStatus {
	status := v1alpha1.TinyNodeStatus{
		Status:  "OK",
		Running: c.runError.Load() == nil && c.spec.Run,
	}
	if err := c.runError.Load(); err != nil {
		status.Error = err.Error()
	}

	ports := c.component.Ports()
	status.Ports = make([]v1alpha1.TinyNodePortStatus, 0)

	// sort ports
	sort.Slice(ports, func(i, j int) bool {
		if ports[i].Settings != ports[j].Settings {
			return true
		}
		return ports[i].Source == ports[j].Source
	})

	var configurableDefinitions = make(map[string]*ajson.Node)

	c.specLock.RLock()
	defer c.specLock.RUnlock()

	for _, np := range ports {

		// processing settings port first, then source, then target
		// @TODO SHOULD WORK FINE WITH ZERO PORTS SPECIFIED

		var ps []byte //port schema settings

		for _, sp := range c.spec.Ports {
			// for own configs, from is empty
			if np.Name == sp.Port || sp.From == "" {
				ps = sp.Schema
				break
			}
		}

		pStatus := v1alpha1.TinyNodePortStatus{
			Name:     np.Name,
			Label:    np.Label,
			Position: v1alpha1.Position(np.Position),
			Settings: np.Settings,
			Status:   np.Status,
			Source:   np.Source,
		}

		if np.Message != nil {
			// define default schema and config using reflection
			schema, err := schema.CreateSchema(np.Message)
			if err != nil {
				c.log.Error(err, "create schema error")
			} else {

				schemaData, _ := schema.MarshalJSON()
				pStatus.Schema = schemaData
			}

			confData, _ := json.Marshal(np.Message)
			pStatus.Configuration = confData
		}

		// get default port schema and value from runtime
		// each request use to actualise knowledge of manager about defaults of a node

		if len(pStatus.Schema) > 0 && len(ps) > 0 {
			// our schema is original re-generated schema + updatable (configurable) definitions
			updatedSchema, err := UpdateWithConfigurableDefinitions(pStatus.Schema, ps, configurableDefinitions)
			if err != nil {
				c.log.Error(err, "update schema")
			} else {
				pStatus.Schema = updatedSchema
			}
		}
		status.Ports = append(status.Ports, pStatus)
	}

	if _, ok := c.component.(m.Emitter); ok {
		status.Emitter = true
	}

	if status.Error != "" {
		status.Status = "ERROR"
	}
	return status
}

func (c *Runner) Process(ctx context.Context, outputCh chan *Msg) error {
	c.log.Info("run process")
	for {
		select {
		case msg := <-c.inputCh:
			c.log.Info("incoming request", "port", msg.Subject)
		case <-ctx.Done():
			return nil
		}
	}
}

// @TODO implement INPUT

func (c *Runner) Input(ctx context.Context, port string, msg *Msg, outputCh chan *Msg) {
	//		// non a system port, find settings and pass to the node
	//		// execute config for the given port
	//		// check if node has such port
	//		// new instance to avoid data confuse types @todo check this
	//		nodePort := m.GetPortByName(c.component.Ports(), port)
	//		if nodePort == nil {
	//			c.sendMessageResponse(msg, fmt.Errorf("port %s is unknown", port), nil)
	//			return
	//		}
	//		// parse input data
	//		requestData, ok := msg.Data.(*module.MessageRequest)
	//		if !ok {
	//			c.sendMessageResponse(msg, fmt.Errorf("invalid input request"), nil)
	//			return
	//		}
	//		// find specific config for a port
	//
	//		//var i int64
	//		//key := metrics.GetMetricKey(requestData.EdgeID, metrics.MetricEdgeMessageReceived)
	//		//c.stats.SetIfAbsent(key, &i)
	//		//counter, _ := c.stats.Get(key)
	//		//atomic.AddInt64(counter, 1)
	//		//
	//		//c.config.RLock()
	//		//defer c.config.RUnlock()
	//		//
	//		//var y int64
	//		//key = metrics.GetMetricKey(c.config.ID, metrics.MetricNodeMessageReceived)
	//		//c.stats.SetIfAbsent(key, &y)
	//		//counter, _ = c.stats.Get(key)
	//		//atomic.AddInt64(counter, 1)
	//
	//		portEdgeSettings := c.config.GetPortConfig(port, &requestData.From)
	//		if portEdgeSettings == nil {
	//			err := fmt.Errorf("port settings are missing for %s", requestData.From)
	//			c.addErr(requestData.EdgeID, err)
	//			c.sendMessageResponse(msg, err, nil)
	//			return
	//		}
	//
	//		if len(portEdgeSettings.Configuration) == 0 {
	//			err := fmt.Errorf("port '%s' (from '%v') config is empty", port, portEdgeSettings.FromNode)
	//			c.addErr(requestData.EdgeID, err)
	//			c.sendMessageResponse(msg, err, nil)
	//			return
	//		}
	//		// create config
	//		portInputData := reflect.New(reflect.TypeOf(nodePort.Message)).Elem()
	//
	//		requestDataNode, err := ajson.Unmarshal(requestData.Payload)
	//		if err != nil {
	//			c.sendMessageResponse(msg, errors.Wrap(err, "ajson parse requestData payload error"), nil)
	//			return
	//		}
	//
	//		eval := evaluator.NewEvaluator(func(expression string) (interface{}, error) {
	//			if expression == "" {
	//				return nil, fmt.Errorf("expression is empty")
	//			}
	//			jsonPathResult, err := ajson.Eval(requestDataNode, expression)
	//			if err != nil {
	//				return nil, err
	//			}
	//			resultUnpack, err := jsonPathResult.Unpack()
	//			if err != nil {
	//				return nil, err
	//			}
	//			return resultUnpack, nil
	//		})
	//
	//		//c.log.ComponentInfo().RawJSON("conf", portEdgeSettings.Configuration).Msg("eval")
	//
	//		configurationMap, err := eval.Eval(portEdgeSettings.Configuration)
	//		if err != nil {
	//			c.addErr(requestData.EdgeID, err)
	//			c.sendMessageResponse(msg, errors.Wrap(err, "eval port edge settings config"), nil)
	//			return
	//		}
	//
	//		err = c.jsonEncodeDecode(configurationMap, portInputData.Addr().Interface())
	//		if err != nil {
	//			c.addErr(requestData.EdgeID, err)
	//			c.sendMessageResponse(msg, errors.Wrap(err, "map decode from config map to port input type"), nil)
	//			return
	//		}
	//		// to avoid nats timeout
	//		c.sendMessageResponse(msg, nil, nil)
	//
	//		err = errorpanic.Wrap(func() error {
	//			return c.component.Handle(ctx, func(port string, data interface{}) error {
	//				if err = c.outputHandler(ctx, port, data, outputCh); err != nil {
	//					c.addErr(port, err)
	//				}
	//				return nil
	//			}, port, portInputData.Interface())
	//		})
	//		if err != nil {
	//			c.log.Error(err, "invoke component error", "id", c.config.ID, "port", port)
	//		}
}

//

//

func (c *Runner) Stop() {
	if c.cancelFunc != nil {
		c.cancelFunc()
	}
}

func (c *Runner) Configure(node v1alpha1.TinyNodeSpec) error {
	c.needRestart = false

	c.specLock.Lock()
	defer c.specLock.Unlock()

	if reflect.DeepEqual(c.spec, node) {
		// nothing to configure
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	if err := c.applyConfigurationToComponent(ctx, node, m.SettingsPort); err != nil {
		return err
	}
	c.spec = node
	// @todo
	// require restart only if setting really changed
	// what?

	c.log.Info("needs restart")
	c.needRestart = true
	return nil
}

func (c *Runner) Run(ctx context.Context, wg *errgroup.Group, outputCh chan *Msg) error {
	emitterComponent, ok := c.component.(m.Emitter)
	if !ok {
		// not runnable
		return fmt.Errorf("component %s is not runnable", c.component.GetInfo().Name)
	}

	if c.cancelFunc != nil {
		// seems to be running
		if c.needRestart {
			c.cancelFunc()
			c.cancelFunc = nil
		} else {
			return nil
		}
	}

	if !c.spec.Run {
		// no need to run and not need to stop
		c.log.Info("no need to run")
		// sleep here to give some time other goroutine update is running atomic
		time.Sleep(time.Second)
		return nil
	}

	var runCtx context.Context
	runCtx, c.cancelFunc = context.WithCancel(ctx)

	wg.Go(func() error {
		// spawn new goroutine
		// reset run error
		c.runError.Store(nil)

		c.log.Info("trying to start emitter", "cmp", c.component.GetInfo().Name, "restart", c.needRestart)
		//
		c.runError.Store(errorpanic.Wrap(func() error {
			// catch panic here
			return emitterComponent.Emit(runCtx, func(port string, data interface{}) error {
				return c.outputHandler(ctx, port, data, outputCh)
			})
		}))
		return nil
	})
	// give some time to fail
	time.Sleep(time.Second * 2)
	// return run error
	c.log.Error(c.runError.Load(), "run error")
	return c.runError.Load()
}

func (c *Runner) outputHandler(ctx context.Context, port string, data interface{}, outputCh chan *Msg) error {

	dataBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	c.specLock.RLock()
	defer c.specLock.RUnlock()

	for _, e := range c.spec.Edges {
		if e.Port == port {
			// edge configured for this port as source
			// send to destination
			outputCh <- &Msg{
				Subject: e.To,
				EdgeID:  e.ID,
				Data:    dataBytes,
			}
		}
	}
	// @todo metrics
	//var y int64
	//key :=fmt.Sprintf("node-%s-%s", c.name, "sent")
	//c.stats.SetIfAbsent(key, &y)
	//counter, _ := c.stats.Get(key)
	//atomic.AddInt64(counter, 1)
	return nil
}

func (c *Runner) jsonEncodeDecode(input interface{}, output interface{}) error {
	b, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, output)
}

func (c *Runner) applyConfigurationToComponent(ctx context.Context, node v1alpha1.TinyNodeSpec, settingsPortName string) error {

	var componentSettingsPort m.NodePort
	for _, p := range c.component.Ports() {
		if p.Name == settingsPortName {
			componentSettingsPort = p
			break
		}
	}

	var nodePortSettings v1alpha1.TinyNodePortConfig
	for _, p := range node.Ports {
		// own port settings
		if p.Port == settingsPortName && p.From == "" {
			nodePortSettings = p
		}
	}

	if len(nodePortSettings.Configuration) == 0 {
		return nil
	}

	v := reflect.New(reflect.TypeOf(componentSettingsPort.Message)).Elem()
	// adapt first
	// just get values, does not care about expression cause there should be none for
	e := evaluator.NewEvaluator(evaluator.DefaultCallback)

	conf, err := e.Eval(nodePortSettings.Configuration)
	if err != nil {
		return err
	}

	err = c.jsonEncodeDecode(conf, v.Addr().Interface())
	if err != nil {
		return err
	}

	err = errorpanic.Wrap(func() error {
		return c.component.Handle(ctx, func(port string, data interface{}) error {
			// fake response handler
			// we don't care how component respond to settings port
			return nil
		}, m.SettingsPort, v.Interface())
	})
	return err
}
