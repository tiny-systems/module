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
	m "github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/evaluator"
	"github.com/tiny-systems/module/pkg/metrics"
	"github.com/tiny-systems/module/pkg/schema"
	"github.com/tiny-systems/module/pkg/utils"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
	"oya.to/namedlocker"
	"reflect"
	"sort"
	"sync/atomic"
	"time"
)

const (
	nodeLock = "_node_lock"
)

type Runner struct {
	// unique instance name

	flowID string
	//

	runnerLocks namedlocker.Store

	name string

	// underlying component
	component m.Component
	//
	log logr.Logger
	// to control K8s resources

	closeCh chan struct{}

	//
	stats cmap.ConcurrentMap[string, *int64]

	node v1alpha1.TinyNode
	//

	tracer trace.Tracer

	meter metric.Meter
}

func NewRunner(name string, component m.Component) *Runner {
	return &Runner{

		name:      name,
		component: component,
		stats:     cmap.New[*int64](),

		//
		runnerLocks: namedlocker.Store{},
		closeCh:     make(chan struct{}),
	}
}

//

func (c *Runner) SetTracer(t trace.Tracer) *Runner {
	c.tracer = t
	return c
}

func (c *Runner) SetMeter(m metric.Meter) *Runner {
	c.meter = m
	return c
}

func (c *Runner) SetLogger(l logr.Logger) *Runner {
	c.log = l
	return c
}

// UpdateStatus apply status changes
func (c *Runner) UpdateStatus(status *v1alpha1.TinyNodeStatus) error {

	var ports []m.NodePort
	// trim system ports
	for _, p := range c.component.Ports() {
		if p.Name == m.HttpPort {
			continue
		}
		ports = append(ports, p)
	}

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

// Input processes input to the inherited component
func (c *Runner) Input(ctx context.Context, msg *Msg, outputHandler Handler) error {
	_, port := utils.ParseFullPortName(msg.To)

	c.log.Info("input", "port", port, "data", msg.Data, "node", c.name)
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer cancel()
		<-c.closeCh
	}()

	var nodePort *m.NodePort
	for _, p := range c.component.Ports() {
		if p.Name == port {
			nodePort = &p
			break
		}
	}
	if nodePort == nil {
		return nil
	}

	var portConfig *v1alpha1.TinyNodePortConfig
	//
	c.runnerLocks.Lock(nodeLock)
	//
	for _, pc := range c.node.Spec.Ports {
		if pc.From == msg.From && pc.Port == port {
			portConfig = &pc
			break
		}
	}
	//
	c.runnerLocks.Unlock(nodeLock)

	var (
		portData interface{}
		err      error
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
		err = c.jsonEncodeDecode(configurationMap, portInputData.Addr().Interface())
		if err != nil {
			return errors.Wrap(err, "map decode from config map to port input type")
		}
		portData = portInputData.Interface()

	} else {
		portData = nodePort.Configuration
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

	// invoke component call
	func() {
		spanCtx, span := c.tracer.Start(ctx, fmt.Sprintf("%s:%s", c.name, port), trace.WithAttributes(
			attribute.String("service_name", c.name)),
		)

		defer span.End()
		err = errorpanic.Wrap(func() error {
			// panic safe
			c.log.Info("component call", "port", port, "node", c.name, "data", portData)

			return c.component.Handle(spanCtx, func(outputPort string, outputData interface{}) error {
				c.log.Info("component callback handler", "port", outputPort, "node", c.name, "data", outputData)
				if e := c.outputHandler(spanCtx, outputPort, outputData, outputHandler); e != nil {
					c.log.Error(e, "handler output error", "name", c.name)
				}
				return nil
			}, port, portData)
		})

		if err != nil {
			span.RecordError(err, trace.WithStackTrace(true))
			span.SetStatus(codes.Error, err.Error())
		}

	}()

	if err != nil {
		return err
	}
	return err
}

func (c *Runner) Node() v1alpha1.TinyNode {
	c.runnerLocks.Lock(nodeLock)
	defer c.runnerLocks.Unlock(nodeLock)
	return c.node
}

// Configure updates specs and decides do we need to restart which handles by Run method
func (c *Runner) Configure(ctx context.Context, node v1alpha1.TinyNode) error {
	c.runnerLocks.Lock(nodeLock)
	c.node = node
	c.runnerLocks.Unlock(nodeLock)

	// 3s to apply settings sounds fair
	ctx, cancel := context.WithTimeout(ctx, time.Second*3)
	defer cancel()
	// we do not send anything to settings port outside
	// we rely on totally internal settings of the settings port
	// todo consider flow envs here

	return c.Input(ctx, &Msg{
		To:   utils.GetPortFullName(c.name, m.SettingsPort),
		Data: []byte("{}"),
	}, nil)
}

// Cancel cancels all entity requests
func (c *Runner) Cancel() {
	var ok = true
	select {
	case _, ok = <-c.closeCh:
	default:
	}
	if !ok {
		return
	}
	close(c.closeCh)
}

func (c *Runner) outputHandler(ctx context.Context, port string, data interface{}, handler Handler) error {

	// system port, no edges connected send it out
	if port == m.RefreshPort {
		return handler(ctx, &Msg{
			To: utils.GetPortFullName(c.name, port),
		})
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	c.runnerLocks.Lock(nodeLock)
	edges := c.node.Spec.Edges[:]
	c.runnerLocks.Unlock(nodeLock)

	wg, ctx := errgroup.WithContext(ctx)

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

		if handler == nil {
			continue
		}
		fromPort := utils.GetPortFullName(c.name, port)
		wg.Go(func() error {
			return handler(ctx, &Msg{
				To:     e.To,
				From:   fromPort,
				EdgeID: e.ID,
				Data:   dataBytes,
			})
		})
	}
	return wg.Wait()
}

func (c *Runner) jsonEncodeDecode(input interface{}, output interface{}) error {
	b, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, output)
}

// GetStats @todo deprecate
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

//main.AddEvent("log", trace.WithAttributes(
// // Log severity and message.
// attribute.String("log.severity", "ERROR"),
// attribute.String("log.message", "request failed"),
// // Optional.
// attribute.String("code.function", "org.FetchUser"),
// attribute.String("code.filepath", "org/user.go"),
// attribute.Int("code.lineno", 123),
//
// // Additional details.
// attribute.String("foo", "hello world"),
//))
