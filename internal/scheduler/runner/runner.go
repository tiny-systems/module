package runner

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/goccy/go-json"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/pkg/errors"
	"github.com/spyzhov/ajson"
	"github.com/swaggest/jsonschema-go"
	"github.com/tiny-systems/errorpanic"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/manager"
	"github.com/tiny-systems/module/internal/tracker"
	m "github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/evaluator"
	"github.com/tiny-systems/module/pkg/metrics"
	"github.com/tiny-systems/module/pkg/schema"
	"github.com/tiny-systems/module/pkg/utils"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Runner struct {
	// unique instance name

	flowID string
	//
	runnerLock *sync.RWMutex

	name string
	// K8s resource

	// underlying component
	component m.Component
	//
	log logr.Logger

	// to control K8s resources
	manager manager.ResourceInterface

	// cancelFunc stops entire instance
	cancelFunc context.CancelFunc
	//
	cancelStopFuncsLock *sync.Mutex

	stats cmap.ConcurrentMap[string, *int64]

	callbacks []tracker.Callback

	node *v1alpha1.TinyNode
}

func NewRunner(name string, flowID string, component m.Component, callbacks ...tracker.Callback) *Runner {

	return &Runner{
		flowID: flowID,
		//node:                node,
		name:      name,
		component: component,
		stats:     cmap.New[*int64](),
		//
		runnerLock:          new(sync.RWMutex),
		cancelStopFuncsLock: new(sync.Mutex),
		callbacks:           callbacks,
	}
}

// InitHTTP if underlying component is http servicer
func (c *Runner) InitHTTP(suggestedPortStr string) {
	if httpEmitter, ok := c.component.(m.HTTPService); ok {
		httpEmitter.HTTPService(func() (int, m.AddressUpgrade) {

			var suggestedPort int
			if annotationPort, err := strconv.Atoi(suggestedPortStr); err == nil {
				suggestedPort = annotationPort
			}

			return suggestedPort, func(httpCtx context.Context, auto bool, hostnames []string, actualLocalPort int) ([]string, error) {
				// limit exposing with a timeout
				exposeCtx, cancel := context.WithTimeout(httpCtx, time.Second*10)
				defer cancel()

				var err error
				// upgrade
				// hostname it's a last part of the node name
				var autoHostName string

				if auto {
					autoHostNameParts := strings.Split(c.name, ".")
					autoHostName = autoHostNameParts[len(autoHostNameParts)-1]
				}

				publicURLs, err := c.manager.ExposePort(exposeCtx, autoHostName, hostnames, actualLocalPort)
				if err != nil {
					return []string{}, err
				}

				go func() {
					// listen when emit ctx ends to clean up
					<-httpCtx.Done()
					c.log.Info("cleaning up exposed port")

					discloseCtx, cancel := context.WithTimeout(context.Background(), time.Second*10)
					defer cancel()
					if err := c.manager.DisclosePort(discloseCtx, actualLocalPort); err != nil {
						c.log.Error(err, "unable to disclose port: %d", actualLocalPort)
					}
				}()
				return publicURLs, err
			}
		})
	}

}

func (c *Runner) SetManager(m manager.ResourceInterface) *Runner {
	c.manager = m
	return c
}

func (c *Runner) SetLogger(l logr.Logger) *Runner {
	c.log = l
	return c
}

// ApplyStatus recreates status from scratch
func (c *Runner) UpdateStatus(status *v1alpha1.TinyNodeStatus) error {
	c.log.Info("build status", "node", c.name)

	ports := c.component.Ports()

	status.Status = "OK"
	status.Ports = make([]v1alpha1.TinyNodePortStatus, 0)

	// sort ports
	sort.Slice(ports, func(i, j int) bool {
		if ports[i].Settings != ports[j].Settings {
			return true
		}
		return ports[i].Source == ports[j].Source
	})

	configurableDefinitions := make(map[string]*ajson.Node)
	var sharedConfigurableSchemaDefinitions = make(map[string]jsonschema.Schema)

	// populate shared definitions with configurable schemas first
	for _, np := range ports {
		_, _ = schema.CreateSchema(np.Configuration, sharedConfigurableSchemaDefinitions)
	}
	// now do it again
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
			Control:  np.Control,
			Source:   np.Source,
		}

		if np.Configuration != nil {
			// define default schema and config using reflection
			schemaConf, err := schema.CreateSchema(np.Configuration, sharedConfigurableSchemaDefinitions)

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
	return nil
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

	c.runnerLock.RLock()
	for _, pc := range c.node.Spec.Ports {
		if pc.From == msg.From && pc.Port == port {
			portConfig = &pc
			break
		}
	}
	c.runnerLock.RUnlock()

	var (
		portData      interface{}
		portDataBytes []byte
		err           error
	)

	if portConfig != nil && len(portConfig.Configuration) > 0 {
		// create config
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
		portDataBytes, err = c.jsonEncodeDecode(configurationMap, portInputData.Addr().Interface())
		if err != nil {
			return errors.Wrap(err, "map decode from config map to port input type")
		}
		portData = portInputData.Interface()

	} else {
		portData = nodePort.Configuration
		portDataBytes, err = json.Marshal(portData)
		if err != nil {
			return errors.Wrap(err, "unable to encode port data")
		}
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
			if e := c.outputHandler(ctx, port, data, outputCh); e != nil {
				c.log.Error(e, "handler output error")
			}
			return nil
		}, port, portData)

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
			Data:      portDataBytes,
			FlowID:    c.flowID,
			NodeStats: c.GetStats(),
		})
	}

	return err
}

// Configure updates specs and decides do we need to restart which handles by Run method
func (c *Runner) Configure(ctx context.Context, node *v1alpha1.TinyNode, outputCh chan *Msg) error {
	c.runnerLock.Lock()
	c.node = node
	c.runnerLock.Unlock()

	ctx, cancel := context.WithTimeout(ctx, time.Second*3)
	defer cancel()
	// we do not send anything to settings port outside
	// we rely on totally internal settings of the settings port
	// todo consider flow envs here

	return c.input(ctx, m.SettingsPort, &Msg{
		To:       utils.GetPortFullName(c.name, m.SettingsPort),
		Data:     []byte("{}"),
		Callback: EmptyCallback,
	}, outputCh)
}

// Process main instance loop
// read input port and apply it to the component
func (c *Runner) Process(ctx context.Context, instanceCh chan *Msg, outputCh chan *Msg) error {

	c.cancelStopFuncsLock.Lock()
	ctx, c.cancelFunc = context.WithCancel(ctx)
	c.cancelStopFuncsLock.Unlock()

	for {
		select {
		case <-ctx.Done():
			c.log.Info("runner process context is done", "runner", c.name)
			return c.Destroy()

		case msg, ok := <-instanceCh:
			if !ok {
				c.log.Info("instance channel closed, exiting", c.name)
				return c.Destroy()
			}
			_, port := utils.ParseFullPortName(msg.To)
			msg.Callback(c.input(ctx, port, msg, outputCh))
		}
	}
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

func (c *Runner) outputHandler(ctx context.Context, port string, data interface{}, eventBus chan *Msg) error {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	c.log.Info("output", "port", port)

	c.runnerLock.RLock()
	if port == m.RefreshPort {
		// create tiny signal instead
		if err = c.manager.CreateClusterNodeSignal(ctx, c.node, port, dataBytes); err != nil {
			c.log.Error(err, "create signal error")
		}
		c.runnerLock.RUnlock()
		return err
	}

	edges := c.node.Spec.Edges[:]
	c.runnerLock.RUnlock()

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

		// track how many messages passed through edge
		var i int64
		key = metrics.GetMetricKey(e.ID, metrics.MetricEdgeMessageSent)
		c.stats.SetIfAbsent(key, &i)
		counter, _ = c.stats.Get(key)
		atomic.AddInt64(counter, 1)

		fromPort := utils.GetPortFullName(c.name, port)

		go func() {
			// send message
			eventBus <- &Msg{
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
