package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-logr/logr"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/pkg/errors"
	"github.com/spyzhov/ajson"
	"github.com/tiny-systems/errorpanic"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/tracker"
	m "github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/evaluator"
	"github.com/tiny-systems/module/pkg/metrics"
	"github.com/tiny-systems/module/pkg/schema"
	"github.com/tiny-systems/module/pkg/utils"
	uatomic "go.uber.org/atomic"
	"golang.org/x/sync/errgroup"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type Runner struct {
	// unique instance name
	name string

	flowID string
	//
	nodeLock *sync.RWMutex

	node v1alpha1.TinyNode
	//
	component m.Component
	//
	log logr.Logger

	// stopFunc stops instance emit
	stopFunc context.CancelFunc

	// cancelFunc stops entire instance
	cancelFunc context.CancelFunc
	//
	cancelFuncsLock *sync.Mutex

	emitCh  chan struct{}
	emitErr *uatomic.Error

	stats cmap.ConcurrentMap[string, *int64]

	needRestart bool

	callbacks []tracker.Callback
}

func NewRunner(node v1alpha1.TinyNode, component m.Component, callbacks ...tracker.Callback) *Runner {

	return &Runner{
		name:            node.Name,
		flowID:          node.Labels[v1alpha1.FlowIDLabel],
		node:            node,
		component:       component,
		stats:           cmap.New[*int64](),
		nodeLock:        new(sync.RWMutex),
		cancelFuncsLock: new(sync.Mutex),
		emitErr:         uatomic.NewError(nil),
		callbacks:       callbacks,
	}
}

func (c *Runner) SetLogger(l logr.Logger) *Runner {
	c.log = l
	return c
}

func (c *Runner) GetStatus() v1alpha1.TinyNodeStatus {
	status := v1alpha1.TinyNodeStatus{
		Status:  "OK",
		Running: c.isEmitting(),
	}
	if err := c.emitErr.Load(); err != nil {
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

	c.nodeLock.RLock()
	defer c.nodeLock.RUnlock()

	for _, np := range ports {
		// processing settings port first, then source, then target
		var ps []byte //port schema settings

		for _, sp := range c.node.Spec.Ports {
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

		if np.Configuration != nil {
			// define default schema and config using reflection
			schemaConf, err := schema.CreateSchema(np.Configuration)

			if err == nil {
				schemaData, _ := schemaConf.MarshalJSON()
				pStatus.Schema = schemaData
			} else {
				c.log.Error(err, "create schema error")
			}

			confData, _ := json.Marshal(np.Configuration)
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
		status.Status = fmt.Sprintf("ERROR: %s", status.Error)
	}
	return status
}

// input processes input to the inherited component
func (c *Runner) input(ctx context.Context, msg *Msg, outputCh chan *Msg) error {

	_, port := utils.ParseFullPortName(msg.To)
	//c.log.Info("process input", "port", port, "data", msg.Data, "node", c.name)

	var nodePort *m.NodePort
	for _, p := range c.component.Ports() {
		if p.Name == port {
			nodePort = &p
			break
		}
	}
	if nodePort == nil {
		// ignore if we have no such port
		return nil
	}

	var portConfig *v1alpha1.TinyNodePortConfig

	c.nodeLock.RLock()
	for _, pc := range c.node.Spec.Ports {
		if pc.From == msg.From {
			portConfig = &pc
			break
		}
	}
	c.nodeLock.RUnlock()

	if portConfig == nil {
		return fmt.Errorf("port configuration is missing for port: %s", port)
	}

	if len(portConfig.Configuration) == 0 {
		return fmt.Errorf("port '%s' (from '%v') config is empty", port, portConfig.From)
	}

	//		// create config
	portInputData := reflect.New(reflect.TypeOf(nodePort.Configuration)).Elem()

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

	// all good, we can say that's the data for incoming port
	// adapt
	portData, err := c.jsonEncodeDecode(configurationMap, portInputData.Addr().Interface())
	if err != nil {
		return errors.Wrap(err, "map decode from config map to port input type")
	}

	// stats
	if msg.EdgeID != "" {
		var i int64
		key := metrics.GetMetricKey(msg.EdgeID, metrics.MetricEdgeMessageReceived)
		c.stats.SetIfAbsent(key, &i)
		counter, _ := c.stats.Get(key)
		atomic.AddInt64(counter, 1)
	}

	var i int64
	key := metrics.GetMetricKey(c.name, metrics.MetricNodeMessageReceived)
	c.stats.SetIfAbsent(key, &i)
	counter, _ := c.stats.Get(key)
	atomic.AddInt64(counter, 1)

	err = errorpanic.Wrap(func() error {
		// panic safe
		//c.log.Info("component call", "port", port, "node", c.name)
		return c.component.Handle(ctx, func(port string, data interface{}) error {
			//c.log.Info("component callback handler", "port", port, "node", c.name)
			if e := c.outputHandler(port, data, outputCh); e != nil {
				c.log.Error(e, "handler output error")
			}
			return nil
		}, port, portInputData.Interface())

	})

	if err != nil {
		return err
	}

	// we can say now that data successfully applied to input port
	// input ports always have no errors
	for _, callback := range c.callbacks {
		callback(tracker.PortMsg{
			NodeName:  c.name,
			EdgeID:    msg.EdgeID,
			PortName:  msg.To, // INPUT PORT OF THE NODE
			Data:      portData,
			FlowID:    c.flowID,
			NodeStats: c.GetStats(),
		})
	}

	return err
}

// Configure updates specs and decides do we need to restart which handles by Run method
func (c *Runner) Configure(ctx context.Context, node v1alpha1.TinyNode, outputCh chan *Msg) error {
	c.needRestart = false

	// apply spec anyway
	c.nodeLock.Lock()

	if (c.isEmitting() && !node.Spec.Run || !c.isEmitting() && node.Spec.Run) || !reflect.DeepEqual(c.node.Spec.Ports, node.Spec.Ports) {
		c.needRestart = true
	}
	//
	c.node = node
	c.nodeLock.Unlock()

	ctx, cancel := context.WithTimeout(ctx, time.Second*3)
	defer cancel()
	// we do not send anything to settings port outside
	// we rely on totally internal settings of the settings port
	// todo consider flow envs here

	return c.input(ctx, &Msg{
		To:       utils.GetPortFullName(c.name, m.SettingsPort),
		Data:     []byte("{}"),
		Callback: EmptyCallback,
	}, outputCh)

}

// Process main instance loop
// read input port and push it to the component
func (c *Runner) Process(ctx context.Context, inputCh chan *Msg, outputCh chan *Msg) error {

	c.cancelFuncsLock.Lock()
	ctx, c.cancelFunc = context.WithCancel(ctx)
	c.cancelFuncsLock.Unlock()

	defer c.Destroy()

	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-inputCh:
			if !ok {
				c.log.Info("channel closed, exiting", c.name)
				return nil
			}
			// configuration error
			msg.Callback(c.input(ctx, msg, outputCh))
		}
	}
}

// Destroy stops the instance inclusing emit
func (c *Runner) Destroy() error {

	c.cancelFuncsLock.Lock()
	defer c.cancelFuncsLock.Unlock()

	if c.stopFunc != nil {
		c.stopFunc()
		c.stopFunc = nil
	}
	if c.cancelFunc != nil {
		c.cancelFunc()
		c.cancelFunc = nil
	}
	return nil
}

func (c *Runner) Run(ctx context.Context, wg *errgroup.Group, outputCh chan *Msg) error {
	emitterComponent, ok := c.component.(m.Emitter)
	if !ok {
		// not runnable
		// don't waste our time
		return nil
	}

	c.cancelFuncsLock.Lock()
	defer c.cancelFuncsLock.Unlock()

	if c.stopFunc != nil {
		// seems to be running
		if c.needRestart {
			// to apply settings we decided earlier that we need a restart
			c.stopFunc()
			// other goroutine should be stooped now
		} else {
			return nil
		}
	}

	if !c.node.Spec.Run {
		// no need to run and not need to stop
		// sleep here to give some time other goroutine update `emitErr` atomic
		time.Sleep(time.Second)
		return nil
	}

	// looks like we about to run
	// spawn new goroutine
	// create new context
	var runCtx context.Context

	runCtx, c.stopFunc = context.WithCancel(ctx)
	// emitCh easy way to tell if emitting is in progress
	c.emitCh = make(chan struct{})

	if httpEmitter, ok := c.component.(m.HTTPService); ok {
		httpEmitter.HTTPService(func(port int) (public string, err error) {
			fmt.Println("port exchange", c.name)
			return "", fmt.Errorf("no ingress")
		})
	}
	wg.Go(func() error {
		// reset run error
		c.log.Info("trying to start emitter", "cmp", c.component.GetInfo().Name, "restart", c.needRestart)
		defer close(c.emitCh)
		// store emitErr atomic
		c.emitErr.Store(
			emitterComponent.Emit(runCtx, func(port string, data interface{}) error {
				// output of the emitter
				return c.outputHandler(port, data, outputCh)
			}))
		return nil
	})
	// give some time to fail
	time.Sleep(time.Second)
	return c.emitErr.Load()
}

func (c *Runner) outputHandler(port string, data interface{}, outputCh chan *Msg) error {

	dataBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	c.nodeLock.RLock()

	edges := c.node.Spec.Edges[:]
	c.nodeLock.RUnlock()

	for _, e := range edges {
		if e.Port != port {
			// edge is not configured for this port as source
			continue
		}
		// send to destination
		// track how many messages component send
		var y int64
		key := metrics.GetMetricKey(c.name, metrics.MetricNodeMessageSent)
		c.stats.SetIfAbsent(key, &y)
		counter, _ := c.stats.Get(key)
		atomic.AddInt64(counter, 1)

		// track how many message sedge passed
		var i int64
		key = metrics.GetMetricKey(e.ID, metrics.MetricEdgeMessageSent)
		c.stats.SetIfAbsent(key, &i)
		counter, _ = c.stats.Get(key)
		atomic.AddInt64(counter, 1)

		fromPort := utils.GetPortFullName(c.name, port)
		outputCh <- &Msg{
			To:     e.To,
			From:   fromPort,
			EdgeID: e.ID,
			Data:   dataBytes,
			Callback: func(err error) {
				// output port
				// call to say port FROM is successfully send data
				// only output ports may have errors
				for _, callback := range c.callbacks {
					callback(tracker.PortMsg{
						NodeName:  c.name,
						EdgeID:    e.ID,
						PortName:  fromPort, // OUTPUT PORT OF THE NODE
						Data:      dataBytes,
						FlowID:    c.flowID,
						NodeStats: c.GetStats(),
						Err:       err,
					})
				}

			},
		}
	}
	return nil
}

func (c *Runner) jsonEncodeDecode(input interface{}, output interface{}) ([]byte, error) {
	b, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	return b, json.Unmarshal(b, output)
}

func (c *Runner) isEmitting() bool {
	if c.emitCh == nil {
		return false
	}
	select {
	case <-c.emitCh:
		return false
	default:
		return true
	}
}

func (c *Runner) GetStats() map[string]interface{} {

	statsMap := map[string]interface{}{}

	for _, k := range c.stats.Keys() {
		counter, _ := c.stats.Get(k)
		if counter == nil {
			continue
		}
		val := *counter
		entityID, metric, err := metrics.GetEntityAndMetric(k)
		if err != nil {
			continue
		}

		if statsMap[entityID] == nil {
			statsMap[entityID] = map[string]interface{}{}
		}
		if statsMapSub, ok := statsMap[entityID].(map[string]interface{}); ok {
			statsMapSub[metric.String()] = val
		}
	}
	return statsMap
}
