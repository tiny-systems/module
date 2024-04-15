package runner

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/goccy/go-json"
	"github.com/pkg/errors"
	"github.com/spyzhov/ajson"
	"github.com/swaggest/jsonschema-go"
	"github.com/tiny-systems/errorpanic"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/tracker"
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
	"reflect"
	"sort"
	"sync"
	"time"
)

type Runner struct {
	// unique instance name

	name string
	// underlying component
	component m.Component
	//
	log logr.Logger
	// to control K8s resources

	closeCh chan struct{}

	node v1alpha1.TinyNode

	ports m.NodePort

	//
	tracer  trace.Tracer
	tracker tracker.Manager

	meter    metric.Meter
	nodeLock *sync.Mutex
}

func NewRunner(name string, component m.Component) *Runner {
	return &Runner{
		name:      name,
		component: component,
		nodeLock:  &sync.Mutex{},
		closeCh:   make(chan struct{}),
	}
}

func (c *Runner) SetTracer(t trace.Tracer) *Runner {
	c.tracer = t
	return c
}

func (c *Runner) SetTracker(t tracker.Manager) *Runner {
	c.tracker = t
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

func (c *Runner) Component() m.Component {
	return c.component
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
	status.Error = false
	status.Ports = make([]v1alpha1.TinyNodePortStatus, 0)

	// sort ports
	sort.SliceStable(ports, func(i, j int) bool {
		return ports[i].Name < ports[j].Name
	})

	var (
		configurableDefinitions             = make(map[string]*ajson.Node)
		sharedConfigurableSchemaDefinitions = make(map[string]jsonschema.Schema)
	)

	// populate shared definitions with configurable schemas first (settings port
	for _, np := range ports {
		if np.Name != m.SettingsPort {
			continue
		}
		_, _ = schema.CreateSchema(np.Configuration, sharedConfigurableSchemaDefinitions)
		break
	}

	// source ports and not settings
	for _, np := range ports {
		if !np.Source || np.Name == m.SettingsPort {
			continue
		}
		_, _ = schema.CreateSchema(np.Configuration, sharedConfigurableSchemaDefinitions)
	}

	// populate shared definitions with configurable schemas first (target ports)
	for _, np := range ports {
		if np.Source {
			continue
		}
		_, _ = schema.CreateSchema(np.Configuration, sharedConfigurableSchemaDefinitions)
	}
	// sharedConfigurableSchemaDefinitions collected

	// now do it again
	for _, np := range ports {
		// processing settings port first, then source, then target
		var portSchema []byte //port schema settings

		// find if we have port settings which might extend the json schema of the port with configurable definitions
		for _, sp := range c.node.Spec.Ports {
			// for own configs, from is empty
			if np.Name == sp.Port || sp.From == "" { // exact port or just any internal port (not edge's one) @todo might need improvement
				portSchema = sp.Schema
				break
			}
		}

		portStatus := v1alpha1.TinyNodePortStatus{
			Name:     np.Name,
			Label:    np.Label,
			Position: v1alpha1.Position(np.Position),
			Source:   np.Source,
		}

		if np.Configuration != nil {
			// define default schema and config using reflection
			schemaConf, err := schema.CreateSchema(np.Configuration, sharedConfigurableSchemaDefinitions)

			if err == nil {
				schemaData, _ := schemaConf.MarshalJSON()
				portStatus.Schema = schemaData
			} else {
				c.log.Error(err, "create schema error")
			}

			confData, _ := json.Marshal(np.Configuration)
			portStatus.Configuration = confData
		}

		if len(portStatus.Schema) > 0 && len(portSchema) > 0 {
			// our schema is original re-generated schema + updatable (configurable) definitions
			updatedSchema, err := UpdateWithConfigurableDefinitions(portStatus.Schema, portSchema, configurableDefinitions)
			if err != nil {
				c.log.Error(err, "update schema")
			} else {
				portStatus.Schema = updatedSchema
			}
		}
		status.Ports = append(status.Ports, portStatus)
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

func (c *Runner) getPortConfig(from string, port string) *v1alpha1.TinyNodePortConfig {
	c.nodeLock.Lock()
	defer c.nodeLock.Unlock()
	//
	for _, pc := range c.node.Spec.Ports {
		if pc.From == from && pc.Port == port {
			return &pc
		}
	}
	return nil
}

// Input processes input to the inherited component
// applies port config for the given port if any
func (c *Runner) Input(ctx context.Context, msg *Msg, outputHandler Handler) (err error) {
	_, port := utils.ParseFullPortName(msg.To)

	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer cancel()
		// close all ongoing requests
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
		// component has no such port
		return nil
	}

	var (
		portConfig = c.getPortConfig(msg.From, port)
		portData   interface{}
		data       []byte
	)

	ctx, span := c.tracer.Start(ctx, msg.EdgeID)

	defer func() {
		if err != nil {
			span.RecordError(err, trace.WithStackTrace(true))
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()

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
		// adaptive
		data, err = c.jsonEncodeDecode(configurationMap, portInputData.Addr().Interface())
		if err != nil {
			return errors.Wrap(err, "map decode from config map to port input type")
		}
		portData = portInputData.Interface()

	} else {

		data, err = json.Marshal(nodePort.Configuration)
		if err != nil {
			return fmt.Errorf("unable to encode nodeport configuration: %v", err)
		}
		portData = nodePort.Configuration
	}

	// send span data only is tracker is on
	if c.tracker.Active(c.Node().Labels[v1alpha1.FlowIDLabel]) {
		span.AddEvent("data", trace.WithAttributes(attribute.String("payload", string(data))))
	}

	err = errorpanic.Wrap(func() error {
		// panic safe
		c.log.Info("component call", "port", port, "node", c.name)
		c.setGauge(1, msg.EdgeID, metrics.MetricEdgeBusy)

		defer func() {
			c.setGauge(0, msg.EdgeID, metrics.MetricEdgeBusy)
		}()

		return c.component.Handle(ctx, func(ctx context.Context, outputPort string, outputData interface{}) error {
			c.log.Info("component callback handler", "port", outputPort, "node", c.name)

			if err = c.outputHandler(ctx, outputPort, outputData, outputHandler); err != nil {
				c.log.Error(err, "handler output error", "name", c.name)
				return err
			}
			return err
		}, port, portData)
	})

	return err
}

func (c *Runner) Node() v1alpha1.TinyNode {
	c.nodeLock.Lock()
	defer c.nodeLock.Unlock()
	return c.node
}

// SetNode updates specs and decides do we need to restart which handles by Run method
func (c *Runner) SetNode(node v1alpha1.TinyNode) {
	c.nodeLock.Lock()
	defer c.nodeLock.Unlock()

	c.node = node
}

// Cancel cancels all ongoing requests
func (c *Runner) Cancel() {
	close(c.closeCh)
}

func (c *Runner) outputHandler(ctx context.Context, port string, data interface{}, handler Handler) error {

	// system ports have no edges connected to it so send empty signal outside
	if port == m.ReconcilePort {
		return handler(ctx, &Msg{
			To: utils.GetPortFullName(c.name, port),
		})
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	c.nodeLock.Lock()
	edges := c.node.Spec.Edges[:]
	c.nodeLock.Unlock()

	// get all edges to connected nodes

	wg, ctx := errgroup.WithContext(ctx)

	for _, e := range edges {
		var edge = e
		//
		if edge.Port != port {
			// edge is not configured for this port as source
			continue
		}
		if handler == nil {
			continue
		}
		fromPort := utils.GetPortFullName(c.name, port)

		wg.Go(func() error {
			// send to destination
			// track how many messages component send

			return handler(ctx, &Msg{
				To:     edge.To,
				From:   fromPort,
				EdgeID: edge.ID,
				Data:   dataBytes,
			})
		})
	}
	// wait while all port will be unblocked
	return wg.Wait()
}

func (c *Runner) jsonEncodeDecode(input interface{}, output interface{}) ([]byte, error) {
	b, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	return b, json.Unmarshal(b, output)
}

//Int64UpDownCounter

func (c *Runner) setGauge(val int64, name string, m metrics.Metric) {
	if name == "" {
		return
	}
	gauge, _ := c.meter.Int64ObservableGauge(string(m),
		metric.WithUnit("1"),
	)

	var (
		r   metric.Registration
		err error
	)

	if r, err = c.meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			o.ObserveInt64(gauge, val,
				metric.WithAttributes(
					attribute.String("element", name),
					attribute.String("flowID", c.Node().Labels[v1alpha1.FlowIDLabel])),
			)
			return nil
		},
		gauge,
	); err != nil {
		c.log.Error(err, "metric gauge err")
		return
	}

	go func() {
		time.Sleep(time.Second)
		_ = r.Unregister()
	}()
}

func (c *Runner) incCounter(ctx context.Context, val int64, port string, m metrics.Metric) {

	counter, err := c.meter.Int64Counter(
		string(m),
		metric.WithUnit("1"),
	)
	if err != nil {
		c.log.Error(err, "metric counter err")
		return
	}

	var attrs = []metric.AddOption{
		metric.WithAttributes(
			attribute.String("element", c.name),
			attribute.String("port", port),
			attribute.String("flowID", c.Node().Labels[v1alpha1.FlowIDLabel]),
		),
	}
	counter.Add(ctx, val, attrs...)
}
