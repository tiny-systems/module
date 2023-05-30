package instance

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/orcaman/concurrent-map/v2"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/spyzhov/ajson"
	"github.com/tiny-systems/errorpanic"
	"github.com/tiny-systems/module/pkg/api/module-go"
	"github.com/tiny-systems/module/pkg/evaluator"
	m "github.com/tiny-systems/module/pkg/module"
	"github.com/tiny-systems/module/pkg/utils"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"reflect"
	"sort"
	"sync/atomic"
	"time"
)

const maxPortState = 1204 * 1024

type Runner struct {
	config *Configuration

	nats *nats.Conn

	cmpID string

	runnerConfig *module.RunnerConfig
	//
	component m.Component
	module    m.Info
	//

	log zerolog.Logger

	startStopCtx        context.Context
	startStopCancelFunc context.CancelFunc
	//
	errors    cmap.ConcurrentMap[string, error]
	stats     cmap.ConcurrentMap[string, *int64]
	portState cmap.ConcurrentMap[string, []byte]
}

func NewRunner(component m.Component, module m.Info) *Runner {
	return &Runner{
		component: component,
		module:    module,
		errors:    cmap.New[error](),
		stats:     cmap.New[*int64](),
		portState: cmap.New[[]byte](),
		config:    new(Configuration),
	}
}

func (c *Runner) SetConfig(config *module.RunnerConfig) *Runner {
	c.runnerConfig = config
	return c
}

func (c *Runner) SetLogger(l zerolog.Logger) *Runner {
	c.log = l
	return c
}

func (c *Runner) IsRunning() bool {
	if c.startStopCtx == nil || c.startStopCtx.Err() != nil {
		return false
	}
	select {
	case <-c.startStopCtx.Done():
	default:
		return true
	}
	return false
}

func (c *Runner) Run(ctx context.Context, runConfigMsg *Msg, inputCh chan *Msg, outputCh chan *Msg) (chan struct{}, error) {
	//
	if err := c.updateConfiguration(runConfigMsg.Data); err != nil {
		c.sendConfigureResponse(runConfigMsg, err)
		return nil, err
	}

	if err := c.applyConfigurationToComponent(ctx); err != nil {
		c.log.Error().Err(err).Msg("apply component conf error")
		c.sendConfigureResponse(runConfigMsg, err)
		return nil, err
	}

	// send success response
	// override maybe with ports actual values
	c.sendConfigureResponse(runConfigMsg, nil, &module.GraphChange{
		Op:       module.GraphChangeOp_UPDATE,
		Elements: c.renderElements(),
		// to make platform know which server acquired this instance
		Server: c.getServerMsg(),
		// and module info of this instance
		Module: c.getModuleMsg(),
	})

	wait := make(chan struct{})

	if c.config.ShouldRun() {
		// should run at start
		// @todo check which context it should be
		go c.run(ctx, runConfigMsg, outputCh)
	}

	go func() {
		defer close(wait)
		for {
			select {
			case msg := <-inputCh:
				c.log.Debug().Str("port", msg.Subject).Msg("incoming request")

				switch msg.Subject {
				case m.RunPort:
					c.run(ctx, msg, outputCh)
				case m.ConfigurePort:
					c.Configure(ctx, msg)
				case m.DestroyPort:
					c.destroy(ctx, msg)
					return
				case m.StopPort:
					c.stop(ctx, msg)
				default:
					c.input(ctx, msg.Subject, msg, outputCh)
				}
				///
			case <-ctx.Done():
				return
			}
		}
	}()
	return wait, nil
}

func (c *Runner) renderNode(configuration *Configuration) *module.Node {
	node, err := NewApiNode(c.component, configuration)
	if err != nil {
		c.log.Error().Err(err).Msg("render node error")
	}

	//node.Component = c.renderComponent()
	node.PortConfigs = make([]*module.PortConfig, 0)
	node.Destinations = make([]*module.MapPort, 0)
	// update port configs how node sees it
	node.ComponentID = configuration.ComponentID

	// sort ports from high priority to less
	sort.Slice(node.Ports, func(i, j int) bool {
		if node.Ports[i].IsSettings != node.Ports[j].IsSettings {
			return true
		}
		return node.Ports[i].Source == node.Ports[j].Source
	})

	var configurableDefinitions = make(map[string]*ajson.Node)

	for _, np := range node.Ports {
		// processing settings port first, then source, then target
		for from, configs := range c.config.PortConfigMap {
			// for own configs from is empty

			for portName, config := range configs {
				if np.PortName != portName {
					continue
				}

				pc := &module.PortConfig{
					From:                 from,
					PortName:             portName,
					Configuration:        config.Configuration,
					Schema:               config.Schema,
					ConfigurationDefault: np.ConfigurationDefault,
					SchemaDefault:        np.SchemaDefault,
				}

				// get default port schema and value from runtime
				// each request use to actualise knowledge of manager about defaults of a node

				if len(pc.Schema) > 0 {
					// our schema is original re-generated schema + updatable (configurable) definitions
					updatedSchema, err := UpdateWithConfigurableDefinitions(portName, pc.SchemaDefault, pc.Schema, configurableDefinitions)
					if err != nil {
						c.log.Error().Err(err).Msg("update schema")
					} else {
						pc.Schema = updatedSchema
					}
				}
				node.PortConfigs = append(node.PortConfigs, pc)
			}
		}
	}

	for from, destinations := range c.config.DestinationMap {
		for _, d := range destinations {
			node.Destinations = append(node.Destinations, &module.MapPort{
				From:   from,
				To:     d.To,
				EdgeID: d.EdgeID,
			})
		}
	}
	return node
}

func (c *Runner) getServerMsg() *module.GraphChangeServer {
	return &module.GraphChangeServer{
		ID: c.runnerConfig.ServerID,
	}
}

func (c *Runner) getModuleMsg() *module.GraphChangeModule {
	return &module.GraphChangeModule{
		Name:      c.module.Name,
		Version:   c.module.Version,
		VersionID: c.module.VersionID,
	}
}

func (c *Runner) renderElements() []*module.GraphElement {
	c.config.RLock()
	defer c.config.RUnlock()
	configuration := c.config
	return []*module.GraphElement{
		{
			ID: configuration.ID,
			//FlowID: config.FlowID,
			Node: c.renderNode(configuration),
		},
	}
}

func (c *Runner) input(ctx context.Context, port string, msg *Msg, outputCh chan *Msg) {
	// non a system port, find settings and pass to the node
	// execute config for the given port
	// check if node has such port
	// new instance to avoid data confuse types @todo check this
	nodePort := m.GetPortByName(c.component.Ports(), port)
	if nodePort == nil {
		c.sendMessageResponse(msg, fmt.Errorf("port %s is unknown", port), nil)
		return
	}
	// parse input data
	requestData, ok := msg.Data.(*module.MessageRequest)
	if !ok {
		c.sendMessageResponse(msg, fmt.Errorf("invalid input request"), nil)
		return
	}
	// find specific config for a port

	var i int64
	key := GetMetricKey(requestData.EdgeID, MetricEdgeMessageReceived)
	c.stats.SetIfAbsent(key, &i)
	counter, _ := c.stats.Get(key)
	atomic.AddInt64(counter, 1)

	c.config.RLock()
	defer c.config.RUnlock()

	var y int64
	key = GetMetricKey(c.config.ID, MetricNodeMessageReceived)
	c.stats.SetIfAbsent(key, &y)
	counter, _ = c.stats.Get(key)
	atomic.AddInt64(counter, 1)

	portEdgeSettings := c.config.GetPortConfig(port, &requestData.From)
	if portEdgeSettings == nil {
		err := fmt.Errorf("port settings are missing for %s", requestData.From)
		c.addErr(requestData.EdgeID, err)
		c.sendMessageResponse(msg, err, nil)
		return
	}

	if len(portEdgeSettings.Configuration) == 0 {
		err := fmt.Errorf("port '%s' (from '%v') config is empty", port, portEdgeSettings.FromNode)
		c.addErr(requestData.EdgeID, err)
		c.sendMessageResponse(msg, err, nil)
		return
	}
	// create config
	portInputData := reflect.New(reflect.TypeOf(nodePort.Message)).Elem()

	requestDataNode, err := ajson.Unmarshal(requestData.Payload)
	if err != nil {
		c.sendMessageResponse(msg, errors.Wrap(err, "ajson parse requestData payload error"), nil)
		return
	}

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

	//c.log.ComponentInfo().RawJSON("conf", portEdgeSettings.Configuration).Msg("eval")

	configurationMap, err := eval.Eval(portEdgeSettings.Configuration)
	if err != nil {
		c.addErr(requestData.EdgeID, err)
		c.sendMessageResponse(msg, errors.Wrap(err, "evel port edge settings config"), nil)
		return
	}
	c.log.Info().Interface("result", configurationMap).Msg("result")

	err = c.jsonEncodeDecode(configurationMap, portInputData.Addr().Interface())
	if err != nil {
		c.addErr(requestData.EdgeID, err)
		c.sendMessageResponse(msg, errors.Wrap(err, "map decode from config map to port input type"), nil)
		return
	}
	// to avoid nats timeout
	c.sendMessageResponse(msg, nil, nil)

	err = errorpanic.Wrap(func() error {
		return c.component.Handle(ctx, func(port string, data interface{}) error {
			if err = c.outputHandler(ctx, port, data, outputCh); err != nil {
				c.addErr(port, err)
			}
			return nil
		}, port, portInputData.Interface())
	})
	if err != nil {
		c.log.Error().Str("id", c.config.ID).Err(err).Str("port", port).Msg("invoke component")
	}
}

func (c *Runner) stop(ctx context.Context, msg *Msg) {
	if !c.IsRunning() {
		c.sendConfigureResponse(msg, nil, &module.GraphChange{
			Op:       module.GraphChangeOp_UPDATE,
			Elements: c.renderElements(),
		})
		return
	}
	if c.startStopCancelFunc != nil {
		c.startStopCancelFunc()
	}
	time.Sleep(time.Millisecond * 300)
	// stopping is always a success (at-least so far)
	c.sendConfigureResponse(msg, nil, &module.GraphChange{
		Op:       module.GraphChangeOp_UPDATE,
		Elements: c.renderElements(),
	})
}

func (c *Runner) destroy(ctx context.Context, msg *Msg) {
	if c.startStopCancelFunc != nil {
		c.startStopCancelFunc()
	}
	// destroying is always a success (at-least so far)
	c.sendConfigureResponse(msg, nil, &module.GraphChange{
		Op:       module.GraphChangeOp_DELETE,
		Elements: c.renderElements(),
	})
}

func (c *Runner) Configure(ctx context.Context, msg *Msg) {
	if err := c.updateConfiguration(msg.Data); err != nil {
		c.sendConfigureResponse(msg, err)
		return
	}
	if err := c.applyConfigurationToComponent(ctx); err != nil {
		c.log.Error().Err(err).Msg("apply component conf error")
		c.sendConfigureResponse(msg, err)
		return
	}
	c.sendConfigureResponse(msg, nil, &module.GraphChange{
		Op:       module.GraphChangeOp_UPDATE,
		Elements: c.renderElements(),
	})
}

func (c *Runner) run(ctx context.Context, msg *Msg, outputCh chan *Msg) {
	runnableComponent, ok := c.component.(m.Runnable)
	if !ok {
		c.sendConfigureResponse(msg, fmt.Errorf("component is not runnable"))
		return
	}
	if c.IsRunning() {
		c.sendConfigureResponse(msg, nil, &module.GraphChange{
			Op:       module.GraphChangeOp_UPDATE,
			Elements: c.renderElements(),
		})
		return
	}
	if err := c.updateConfiguration(msg.Data); err != nil {
		c.sendConfigureResponse(msg, err)
		return
	}
	if err := c.applyConfigurationToComponent(ctx); err != nil {
		c.sendConfigureResponse(msg, err)
		return
	}

	go func() {
		if c.startStopCancelFunc != nil {
			c.startStopCancelFunc()
		}
		c.startStopCtx, c.startStopCancelFunc = context.WithCancel(ctx)
		defer func() {
			c.startStopCancelFunc()
		}()
		c.sendConfigureResponse(msg, runnableComponent.Run(c.startStopCtx, func(port string, data interface{}) error {
			return c.outputHandler(ctx, port, data, outputCh)
		}))
	}()
	// give some time to fail
	time.Sleep(time.Millisecond * 300)

	c.sendConfigureResponse(msg, nil, &module.GraphChange{
		Op:       module.GraphChangeOp_UPDATE,
		Elements: c.renderElements(),
	})

}

func (c *Runner) outputHandler(ctx context.Context, port string, data interface{}, outputCh chan *Msg) (err error) {
	c.config.RLock()
	defer c.config.RUnlock()

	defer func() {
		if rErr := recover(); rErr != nil {
			err = fmt.Errorf("%v", rErr)
		}
	}()

	portFullName := utils.GetPortFullName(c.config.ID, port)
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	if len(dataBytes) < maxPortState {
		c.portState.Set(portFullName, dataBytes)
	}

	destinations, ok := c.config.DestinationMap[portFullName]
	if !ok {
		return nil
	}

	id, err := uuid.NewUUID()
	if err != nil {
		return err
	}
	for _, d := range destinations {
		req := &module.MessageRequest{
			ID:      id.String(),
			From:    portFullName,
			Payload: dataBytes,
			EdgeID:  d.EdgeID,
		}

		// track how many messages component send
		var y int64
		key := GetMetricKey(c.config.ID, MetricNodeMessageSent)
		c.stats.SetIfAbsent(key, &y)
		counter, _ := c.stats.Get(key)
		atomic.AddInt64(counter, 1)

		// track how many message sedge passed
		var i int64
		key = GetMetricKey(req.EdgeID, MetricEdgeMessageSent)
		c.stats.SetIfAbsent(key, &i)
		counter, _ = c.stats.Get(key)
		atomic.AddInt64(counter, 1)
		select {
		case <-ctx.Done():
			return nil
		case outputCh <- NewMsgWithSubject(d.To, req, nil):
		}
	}
	return nil
}

func (c *Runner) jsonEncodeDecode(input interface{}, output interface{}) error {
	b, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, output)
}

func (c *Runner) applyConfigurationToComponent(ctx context.Context) error {
	c.config.RLock()
	defer c.config.RUnlock()
	var (
		// new instance to avoid data confuse types @todo check this
		settingsPort = m.GetPortByName(c.component.Ports(), m.SettingsPort)
		settings     = c.config.GetPortConfig(m.SettingsPort, nil)
		err          error
	)

	if settings == nil || settingsPort == nil {
		return nil
	}

	v := reflect.New(reflect.TypeOf(settingsPort.Message)).Elem()
	// adapt first

	// just get values, does not care about expression cause there should be none for
	e := evaluator.NewEvaluator(evaluator.DefaultCallback)

	c.log.Info().RawJSON("conf", settings.Configuration).Msg("config")
	conf, err := e.Eval(settings.Configuration)
	if err != nil {
		c.log.Error().Err(err).Msg("config eval error")
		return err
	}
	c.log.Info().Interface("result", conf).Msg("result")
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

func (c *Runner) updateConfiguration(data interface{}) error {
	conf, ok := data.(*module.ConfigureInstanceRequest)
	if !ok {
		return fmt.Errorf("invalid configuration request")
	}
	for _, p := range c.component.Ports() {
		if p.Source {
			continue
		}
	}

	c.config.Lock()
	defer c.config.Unlock()

	if conf.InstanceID != "" {
		c.config.ID = conf.InstanceID
	}
	if conf.Data != nil {
		c.config.Data = conf.Data.AsMap()
	}

	if conf.ComponentID != "" {
		c.config.ComponentID = conf.ComponentID
	}

	if conf.FlowID != "" {
		c.config.FlowID = conf.FlowID
	}
	// let runner know where send messages next
	c.config.DestinationMap = make(map[string][]MapPort)
	for _, v := range conf.Destinations {
		if _, ok := c.config.DestinationMap[v.From]; !ok {
			c.config.DestinationMap[v.From] = []MapPort{}
		}
		c.config.DestinationMap[v.From] = append(c.config.DestinationMap[v.From], MapPort{
			From:   v.From,
			EdgeID: v.EdgeID,
			To:     v.To,
		})
	}
	// configurations of the node's port, may be different depend on FROM
	if len(conf.PortConfigs) > 0 {
		// apply port config only if ports presented
		// reset
		c.config.PortConfigMap = make(map[string]map[string]PortConfig)
		for _, pc := range conf.PortConfigs {
			if c.config.PortConfigMap[pc.From] == nil {
				c.config.PortConfigMap[pc.From] = make(map[string]PortConfig)
			}
			c.config.PortConfigMap[pc.From][pc.PortName] = PortConfig{
				Configuration: pc.Configuration,
				Schema:        pc.Schema,
				FromNode:      pc.From,
			}
		}
	}
	c.config.Run = conf.Run
	c.config.Revision = conf.Revision
	return nil
}

// addErr elementID could be Edge or Port or NodeID
func (c *Runner) addErr(elementID string, err error) {
	c.errors.Set(elementID, err)
}

func (c *Runner) sendMessageResponse(msg *Msg, err error, data []byte) {
	if err != nil {
		c.log.Error().Err(err).Msg("send message response")
	}
	resp := &module.MessageResponse{
		Data: data,
	}
	if err != nil {
		resp.HasError = true
		resp.Error = err.Error()
	}

	if err = msg.Respond(resp); err != nil {
		c.log.Error().Err(err).Msg("message response")
	}
}

func (c *Runner) sendConfigureResponse(msg *Msg, err error, changes ...*module.GraphChange) {
	resp := &module.ConfigureInstanceResponse{
		IsRunning: c.IsRunning(),
		Changes:   changes,
		//ServerID:  c.serverConfig.ID,
	}
	if err != nil {
		resp.HasError = true
		resp.Error = err.Error()
	}
	if err = msg.Respond(resp); err != nil {
		c.log.Error().Err(err).Msg("configure response")
	}
}

func (c *Runner) GetDiscoveryNode(full bool) *module.DiscoveryNode {
	// read statistics
	statsMap := map[string]interface{}{}
	c.config.RLock()
	defer c.config.RUnlock()

	for _, k := range c.stats.Keys() {
		counter, _ := c.stats.Get(k)
		if counter == nil {
			continue
		}
		val := *counter
		entityID, metric, isPrev, err := GetEntityAndMetric(k)
		if err != nil {
			continue
		}
		var prevVal int64

		if !isPrev {
			// current metric is not previous
			// get prev
			prevMetricKey := GetPrevMetricKey(entityID, metric)
			if prevCounter, ok := c.stats.Get(prevMetricKey); ok {
				prevVal = *prevCounter
			}
			// update prev
			c.stats.Set(prevMetricKey, &val)
		}

		if statsMap[entityID] == nil {
			statsMap[entityID] = map[string]interface{}{}
		}
		if statsMapSub, ok := statsMap[entityID].(map[string]interface{}); ok {
			statsMapSub[metric.String()] = val
			if !isPrev {
				// current metric is not prev
				// add special metric
				if metric == MetricEdgeMessageReceived && val > prevVal {
					statsMapSub[MetricEdgeActive.String()] = 1
				} //else {
				//statsMapSub[MetricEdgeActive.String()] = 0
				//}
			}
		}
	}
	if _, ok := c.component.(m.Runnable); ok {
		if statsMap[c.config.ID] == nil {
			statsMap[c.config.ID] = map[string]interface{}{}
		}
		if statsMapSub, ok := statsMap[c.config.ID].(map[string]interface{}); ok {
			if c.IsRunning() {
				statsMapSub[MetricNodeRunning.String()] = 1
			} else {
				statsMapSub[MetricNodeRunning.String()] = 0
			}
		}
	}

	stats, err := structpb.NewStruct(statsMap)
	if err != nil {
		c.log.Error().Err(err).Msg("stats struct error")
	}

	var portLastState []*module.PortState
	if full {
		for _, k := range c.portState.Keys() {
			v, _ := c.portState.Get(k)
			portLastState = append(portLastState, &module.PortState{
				Data:     v,
				Date:     timestamppb.Now(),
				PortName: k,
				NodeID:   c.config.ID,
			})
		}
	}

	n := &module.DiscoveryNode{
		ID:            c.config.ID,
		FlowID:        c.config.FlowID,
		Stats:         stats,
		PortLastState: portLastState,
	}
	//
	// non dev
	n.ServerID = c.runnerConfig.ServerID
	n.WorkspaceID = c.runnerConfig.WorkspaceID
	return n
}
