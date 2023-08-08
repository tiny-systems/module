package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/davecgh/go-spew/spew"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	"github.com/spyzhov/ajson"
	"github.com/tiny-systems/errorpanic"
	"github.com/tiny-systems/module/api/v1alpha1"
	m "github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/evaluator"
	"github.com/tiny-systems/module/pkg/schema"
	"github.com/tiny-systems/module/pkg/utils"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"
	"reflect"
	"sort"
	"sync"
	"time"
)

type Runner struct {
	// unique instance name
	name      string
	destroyCh chan struct{}
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
}

func NewRunner(name string, component m.Component) *Runner {
	return &Runner{
		name:      name,
		destroyCh: make(chan struct{}),
		component: component,
		//stats:     cmap.New[*int64](),
		specLock: new(sync.RWMutex),
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

			if err == nil {
				schemaData, _ := schema.MarshalJSON()
				pStatus.Schema = schemaData
			} else {
				c.log.Error(err, "create schema error")
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

func (c *Runner) input(ctx context.Context, port string, msg *Msg, outputCh chan *Msg) error {
	c.log.Info("process input", "port", port, "node", c.name)

	var nodePort *m.NodePort
	for _, p := range c.component.Ports() {
		if p.Name == port {
			nodePort = &p
			break
		}
	}
	if nodePort == nil {
		return fmt.Errorf("port %s is unknown", port)
	}

	//var i int64
	//key := metrics.GetMetricKey(requestData.EdgeID, metrics.MetricEdgeMessageReceived)
	//c.stats.SetIfAbsent(key, &i)
	//counter, _ := c.stats.Get(key)
	//atomic.AddInt64(counter, 1)
	//

	//c.log.Info("specification locked")
	//var y int64
	//key = metrics.GetMetricKey(c.config.ID, metrics.MetricNodeMessageReceived)
	//c.stats.SetIfAbsent(key, &y)
	//counter, _ = c.stats.Get(key)
	//atomic.AddInt64(counter, 1)
	var portConfig *v1alpha1.TinyNodePortConfig

	c.specLock.RLock()

	for _, pc := range c.spec.Ports {
		if pc.From == msg.From {
			portConfig = &pc
			break
		}
	}
	c.specLock.RUnlock()

	spew.Dump(portConfig)

	if portConfig == nil {
		return fmt.Errorf("port configuration is missing for port: %s", port)
	}

	if len(portConfig.Configuration) == 0 {
		return fmt.Errorf("port '%s' (from '%v') config is empty", port, portConfig.From)
	}

	//		// create config
	portInputData := reflect.New(reflect.TypeOf(nodePort.Message)).Elem()

	requestDataNode, err := ajson.Unmarshal(msg.Data)
	if err != nil {
		return errors.Wrap(err, "ajson parse requestData payload error")
	}
	//
	eval := evaluator.NewEvaluator(func(expression string) (interface{}, error) {
		if expression == "" {
			return nil, fmt.Errorf("expression is empty")
		}
		jsonPathResult, err := ajson.Eval(requestDataNode, expression)
		if err != nil {
			return nil, err
		}
		resultUnpack, err := jsonPathResult.Unpack()
		if err != nil {
			return nil, err
		}
		return resultUnpack, nil
	})

	configurationMap, err := eval.Eval(portConfig.Configuration)
	if err != nil {
		return errors.Wrap(err, "eval port edge settings config")
	}
	// adapt
	err = c.jsonEncodeDecode(configurationMap, portInputData.Addr().Interface())
	if err != nil {
		return errors.Wrap(err, "map decode from config map to port input type")
	}

	err = errorpanic.Wrap(func() error {
		// panic safe
		c.log.Info("make component call", "port", port, "node", c.name)

		return c.component.Handle(ctx, func(port string, data interface{}) error {
			c.log.Info("component answered", "port", port, "node", c.name)
			if err = c.outputHandler(ctx, port, data, outputCh); err != nil {
				c.log.Error(err, "handler error")
			}
			return nil
		}, port, portInputData.Interface())
	})
	if err != nil {
		c.log.Error(err, "invoke component error", "id", c.name, "port", port)
	}
	return nil
}

//

func (c *Runner) Stop() {
	if c.cancelFunc != nil {
		c.cancelFunc()
	}
}

func (c *Runner) Configure(node v1alpha1.TinyNodeSpec, outputCh chan *Msg) error {
	c.needRestart = false

	if reflect.DeepEqual(c.spec, node) {
		// nothing to configure
		return nil
	}

	c.specLock.Lock()
	c.spec = node
	c.specLock.Unlock()

	c.log.Info("needs restart")
	c.needRestart = true

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()
	// we do not send anything to settings port outside
	// we rely on totally internal settings of the settings port
	if err := c.input(ctx, m.SettingsPort, &Msg{Data: []byte("{}")}, outputCh); err != nil {
		return err
	}
	// if component ate it, update runner

	c.log.Info("configuration applied")
	// @todo
	// require restart only if setting really changed
	// what?

	return nil
}

func (c *Runner) Process(ctx context.Context, inputCh chan *Msg, outputCh chan *Msg) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-inputCh:
			if !ok {
				return nil
			}
			_, port := utils.ParseFullPortName(msg.To)
			c.input(ctx, port, msg, outputCh)
		}
	}
}

func (c *Runner) Run(ctx context.Context, wg *errgroup.Group, outputCh chan *Msg) error {

	c.log.Info("trying to run", "instance", c.name, "needed", c.needRestart)
	emitterComponent, ok := c.component.(m.Emitter)
	if !ok {
		// not runnable
		// don't waste our time
		c.log.Info("not runnable")
		return nil
	}

	if c.needRestart {
		c.log.Info("cancelling as need restart")
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
		// sleep here to give some time other goroutine update `is running` atomic
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

	edges := c.spec.Edges
	c.specLock.RUnlock()

	for _, e := range edges {
		if e.Port == port {
			// edge configured for this port as source
			// send to destination
			outputCh <- &Msg{
				To:     e.To,
				From:   utils.GetPortFullName(c.name, port),
				EdgeID: e.ID,
				Data:   dataBytes,
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
