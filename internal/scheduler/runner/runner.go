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
	"github.com/tiny-systems/module/internal/manager"
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
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Runner struct {
	// unique instance name

	flowID string
	//
	nodeLock *sync.RWMutex

	// K8s resource
	node v1alpha1.TinyNode
	// underlying component
	component m.Component
	//
	log logr.Logger

	// to control K8s resources
	manager manager.ResourceInterface

	// stopFunc stops instance emit
	stopFunc context.CancelFunc

	// cancelFunc stops entire instance
	cancelFunc context.CancelFunc
	//
	cancelStopFuncsLock *sync.Mutex

	// emitCh easy way to tell if emitting is in progress
	emitCh  chan struct{}
	emitErr *uatomic.Error

	stats cmap.ConcurrentMap[string, *int64]

	needRestart bool
	callbacks   []tracker.Callback

	listenPortLock *sync.Mutex

	// if underlying component is HTTPServicer
	listenPort int

	publicURL string
}

func NewRunner(node v1alpha1.TinyNode, component m.Component, callbacks ...tracker.Callback) *Runner {

	r := &Runner{
		flowID:              node.Labels[v1alpha1.FlowIDLabel],
		node:                node,
		component:           component,
		stats:               cmap.New[*int64](),
		nodeLock:            new(sync.RWMutex),
		cancelStopFuncsLock: new(sync.Mutex),
		listenPortLock:      new(sync.Mutex),
		emitErr:             uatomic.NewError(nil),
		callbacks:           callbacks,
	}

	// if underlying component is http servicer

	if httpEmitter, ok := component.(m.HTTPService); ok {
		httpEmitter.HTTPService(func() (int, m.AddressUpgrade) {
			var savedPort int

			r.listenPortLock.Lock()
			defer r.listenPortLock.Unlock()

			savedPort = r.listenPort

			if node.Status.Http.ListenPort > 0 {
				savedPort = node.Status.Http.ListenPort
			}

			return savedPort, func(finalPort int) (string, error) {
				r.listenPortLock.Lock()
				defer r.listenPortLock.Unlock()

				ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
				defer cancel()

				var err error
				// upgrade
				// hostname it's a last part of the node name
				hostname := strings.Split(r.node.Name, ".")

				r.publicURL, err = r.manager.ExposePort(ctx, hostname[len(hostname)-1], finalPort)
				if err != nil {
					return "", err
				}
				r.listenPort = finalPort
				return r.publicURL, err
			}
		})
	}

	return r
}

func (c *Runner) SetManager(m manager.ResourceInterface) *Runner {
	c.manager = m
	return c
}

func (c *Runner) SetLogger(l logr.Logger) *Runner {
	c.log = l
	return c
}

// GetStatus recreates status from scratch
func (c *Runner) GetStatus() v1alpha1.TinyNodeStatus {
	status := v1alpha1.TinyNodeStatus{
		Status:   "OK",
		Emitting: c.isEmitting(),
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

	configurableDefinitions := make(map[string]*ajson.Node)

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

	cmpInfo := c.component.GetInfo()
	//
	status.Component = v1alpha1.TinyNodeComponentStatus{
		Description: cmpInfo.Description,
		Info:        cmpInfo.Info,
		Tags:        cmpInfo.Tags,
	}
	if _, ok := c.component.(m.HTTPService); ok {
		status.Http.Available = true
	} else {
		return status
	}

	c.listenPortLock.Lock()
	defer c.listenPortLock.Unlock()

	port := c.listenPort
	if port == 0 {
		// we have not made up any port
		// fallback to node port
		port = c.node.Status.Http.ListenPort
	}

	status.Http.ListenPort = port
	status.Http.PublicURL = c.publicURL
	return status
}

// input processes input to the inherited component
func (c *Runner) input(ctx context.Context, port string, msg *Msg, outputCh chan *Msg) error {

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
		// nothing to configure
		return nil
	}

	if len(portConfig.Configuration) == 0 {
		return nil
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
	key := metrics.GetMetricKey(c.node.Name, metrics.MetricNodeMessageReceived)
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
			NodeName:  c.node.Name,
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

	if !reflect.DeepEqual(c.node.Spec.Ports, node.Spec.Ports) {
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

	return c.input(ctx, m.SettingsPort, &Msg{
		To:       utils.GetPortFullName(c.node.Name, m.SettingsPort),
		Data:     []byte("{}"),
		Callback: EmptyCallback,
	}, outputCh)

}

// Process main instance loop
// read input port and apply it to the component
func (c *Runner) Process(ctx context.Context, wg *errgroup.Group, instanceCh chan *Msg, outputCh chan *Msg) error {

	c.cancelStopFuncsLock.Lock()
	ctx, c.cancelFunc = context.WithCancel(ctx)
	c.cancelStopFuncsLock.Unlock()

	for {
		select {
		case <-ctx.Done():
			c.log.Info("runner process context is done", "runner", c.node.Name)
			return c.cleanup()

		case msg, ok := <-instanceCh:
			if !ok {
				c.log.Info("instance channel closed, exiting", c.node.Name)
				return c.cleanup()
			}
			_, port := utils.ParseFullPortName(msg.To)
			// check system ports
			switch port {
			case m.RunPort:
				msg.Callback(c.run(ctx, wg, outputCh))
			case m.StopPort:
				msg.Callback(c.stop())
			default:
				msg.Callback(c.input(ctx, port, msg, outputCh))
			}
		}
	}
}

func (c *Runner) cleanup() error {

	c.cancelStopFuncsLock.Lock()
	defer c.cancelStopFuncsLock.Unlock()

	if c.stopFunc != nil {
		c.stopFunc()
		c.stopFunc = nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*15)
	defer cancel()

	c.listenPortLock.Lock()
	c.listenPortLock.Unlock()

	if c.listenPort == 0 {
		return nil
	}

	if err := c.manager.DisclosePort(ctx, c.listenPort); err != nil {
		return err
	}

	c.listenPort = 0
	return nil
}

// Destroy stops the instance inclusing emit
func (c *Runner) Destroy() error {

	c.cancelStopFuncsLock.Lock()
	defer c.cancelStopFuncsLock.Unlock()

	if c.cancelFunc != nil {
		c.cancelFunc()
		c.cancelFunc = nil
	}
	return nil
}

func (c *Runner) stop() error {

	c.cancelStopFuncsLock.Lock()
	defer c.cancelStopFuncsLock.Unlock()

	if c.stopFunc != nil {
		c.stopFunc()
		c.stopFunc = nil
	}
	return nil
}

func (c *Runner) run(ctx context.Context, wg *errgroup.Group, outputCh chan *Msg) error {
	emitterComponent, ok := c.component.(m.Emitter)
	if !ok {
		// not runnable underlying component
		return nil
	}

	c.cancelStopFuncsLock.Lock()
	defer c.cancelStopFuncsLock.Unlock()

	if c.stopFunc != nil {
		// seems to be running
		// to apply settings we decided earlier that we need a restart
		c.stopFunc()
	}

	// looks like we about to run
	// spawn new goroutine
	// create new context
	var emitCtx context.Context

	emitCtx, c.stopFunc = context.WithCancel(ctx)

	// emitCh easy way to tell if emitting is in progress
	c.emitCh = make(chan struct{})

	wg.Go(func() error {
		// reset run error
		c.log.Info("emitter start", "cmp", c.component.GetInfo().Name, "restart", c.needRestart)
		defer close(c.emitCh)
		defer func() {
			c.log.Info("emitter stopped")
		}()
		// store emitErr atomic
		c.emitErr.Store(
			emitterComponent.Emit(emitCtx, func(port string, data interface{}) error {
				// output of the emitter
				return c.outputHandler(port, data, outputCh)
			}))
		return nil
	})
	// give some time to fail
	time.Sleep(time.Millisecond * 1500)
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
		key := metrics.GetMetricKey(c.node.Name, metrics.MetricNodeMessageSent)
		c.stats.SetIfAbsent(key, &y)
		counter, _ := c.stats.Get(key)
		atomic.AddInt64(counter, 1)

		// track how many messages passed through edge
		var i int64
		key = metrics.GetMetricKey(e.ID, metrics.MetricEdgeMessageSent)
		c.stats.SetIfAbsent(key, &i)
		counter, _ = c.stats.Get(key)
		atomic.AddInt64(counter, 1)

		fromPort := utils.GetPortFullName(c.node.Name, port)

		go func() {
			// send message
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
							NodeName:  c.node.Name,
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
		}()
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
