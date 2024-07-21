package runner

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/goccy/go-json"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
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
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
)

type Runner struct {
	// unique instance name

	name string

	flowID    string
	projectID string

	// underlying component
	component m.Component
	//
	log logr.Logger
	// to control K8s resources

	closeCh chan struct{}

	node v1alpha1.TinyNode

	//
	portsCache     []m.Port
	portsCacheLock *sync.RWMutex

	//
	tracer  trace.Tracer
	tracker tracker.Manager

	meter metric.Meter
	//
	nodeLock *sync.Mutex

	previousSettings interface{}

	reconciled *atomic.Bool
}

func NewRunner(name string, component m.Component) *Runner {
	return &Runner{
		name:           name,
		component:      component,
		nodeLock:       &sync.Mutex{},
		closeCh:        make(chan struct{}),
		portsCacheLock: &sync.RWMutex{},
		reconciled:     &atomic.Bool{},
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

func (c *Runner) getPorts() []m.Port {
	c.portsCacheLock.RLock()

	if c.portsCache != nil {
		c.portsCacheLock.RUnlock()
		return c.portsCache
	}

	c.portsCacheLock.RUnlock()
	return c.getUpdatePorts()
}

func (c *Runner) getUpdatePorts() []m.Port {
	c.portsCacheLock.Lock()
	defer c.portsCacheLock.Unlock()

	c.portsCache = c.component.Ports()
	return c.portsCache
}

// UpdateStatus apply status changes
func (c *Runner) UpdateStatus(status *v1alpha1.TinyNodeStatus) error {

	status.Status = "OK"
	status.Error = false
	status.Ports = make([]v1alpha1.TinyNodePortStatus, 0)

	var ports []m.Port
	// trim system ports
	for _, p := range c.getUpdatePorts() {
		if p.Name == m.NodePort || p.Name == m.ClientPort {
			continue
		}
		ports = append(ports, p)
	}

	// sort ports
	sort.SliceStable(ports, func(i, j int) bool {
		return ports[i].Name < ports[j].Name
	})

	// now do it again
	for _, np := range ports {

		portStatus := v1alpha1.TinyNodePortStatus{
			Name:     np.Name,
			Label:    np.Label,
			Position: v1alpha1.Position(np.Position),
			Source:   np.Source,
		}

		if np.Configuration != nil {
			// get real schema and config using reflection
			schemaConf, err := schema.CreateSchema(np.Configuration)
			if err != nil {
				c.log.Error(err, "create schema error")
			} else {
				schemaData, _ := schemaConf.MarshalJSON()
				portStatus.Schema = schemaData
			}
			// real port data
			confData, _ := json.Marshal(np.Configuration)
			portStatus.Configuration = confData
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
	if port == "" {
		return fmt.Errorf("input port is empty")
	}

	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer cancel()
		// close all ongoing requests
		<-c.closeCh
	}()

	var nodePort *m.Port
	for _, p := range c.getPorts() {
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
	)

	// @todo add const
	if msg.From == "signal" {
		// from signal controller (outside)

		portInputData := reflect.New(reflect.TypeOf(nodePort.Configuration)).Elem()
		if err = json.Unmarshal(msg.Data, portInputData.Addr().Interface()); err != nil {
			return err
		}
		portData = portInputData.Interface()

	} else if portConfig != nil && len(portConfig.Configuration) > 0 {
		// we have edge config

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
		if err = c.jsonEncodeDecode(configurationMap, portInputData.Addr().Interface()); err != nil {
			return errors.Wrap(err, "map decode from config map to port input type")
		}
		portData = portInputData.Interface()

	} else {
		// default is the state of a port's config
		portData = nodePort.Configuration
	}

	////
	if port == m.SettingsPort {
		if cmp.Equal(portData, c.previousSettings) {
			// check cache, we do not want to update settings if they did not change
			return nil
		}
		c.previousSettings = portData
	}

	u, err := uuid.NewUUID()
	if err != nil {
		return err
	}

	ctx, inputSpan := c.tracer.Start(ctx, u.String(),
		trace.WithAttributes(attribute.String("to", utils.GetPortFullName(c.name, port))),
		trace.WithAttributes(attribute.String("from", msg.From)),
		trace.WithAttributes(attribute.String("flowID", c.flowID)),
		trace.WithAttributes(attribute.String("projectID", c.projectID)),
	)

	defer func() {
		if err != nil {
			c.addSpanError(inputSpan, err)
		}
		inputSpan.End()
	}()

	// send span data only is tracker is on
	if c.tracker.Active(c.projectID) {
		inputData, _ := json.Marshal(portData)
		c.addSpanPortData(inputSpan, string(inputData))
	}

	err = errorpanic.Wrap(func() error {
		// panic safe
		c.log.Info("component call", "port", port, "node", c.name)
		c.incCounter(ctx, 1, msg.EdgeID, metrics.MetricEdgeMsgIn)

		defer func() {
			c.incCounter(ctx, 1, msg.EdgeID, metrics.MetricEdgeMsgOut)
		}()

		return c.component.Handle(ctx, func(outputCtx context.Context, outputPort string, outputData interface{}) error {
			c.log.Info("component callback handler", "port", outputPort, "node", c.name)

			if outputPort == m.ReconcilePort {
				if !c.reconciled.Load() {
					return nil
				}

				c.reconciled.Store(false)
				// do not trace reconcile port
				return outputHandler(ctx, &Msg{
					To: utils.GetPortFullName(c.name, outputPort),
				})

			}

			u, err := uuid.NewUUID()
			if err != nil {
				return err
			}

			outputCtx, outputSpan := c.tracer.Start(outputCtx, u.String(), trace.WithAttributes(
				attribute.String("port", utils.GetPortFullName(c.name, outputPort)),
				attribute.String("flowID", c.flowID),
				attribute.String("projectID", c.projectID),
			))

			defer outputSpan.End()

			// send span data only is tracker is on
			if c.tracker.Active(c.projectID) {
				outputDataBytes, _ := json.Marshal(outputData)
				c.addSpanPortData(outputSpan, string(outputDataBytes))
			}

			// all handler calls have app level context (not a request level one)
			if err = c.outputHandler(trace.ContextWithSpanContext(ctx, trace.SpanContextFromContext(outputCtx)), outputPort, outputData, outputHandler); err != nil {
				return err
			}
			return nil
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
	//
	c.reconciled.Store(true)
	c.flowID = node.Labels[v1alpha1.FlowIDLabel]
	c.projectID = node.Labels[v1alpha1.ProjectIDLabel]
	//
	c.node = node
}

// Cancel cancels all ongoing requests
func (c *Runner) Cancel() {
	close(c.closeCh)
}

func (c *Runner) outputHandler(ctx context.Context, port string, data interface{}, handler Handler) error {

	dataBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	if node, _ := utils.ParseFullPortName(port); node != "" {
		// we already have full port name, means no need to check edges (useful for input/outputs)
		return handler(ctx, &Msg{
			To:   port,
			Data: dataBytes,
		})
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

func (c *Runner) jsonEncodeDecode(input interface{}, output interface{}) error {
	b, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, output)
}

//Int64UpDownCounter

//func (c *Runner) setGauge(val int64, name string, m metrics.Metric) {
//	if name == "" {
//		return
//	}
//	gauge, _ := c.meter.Int64ObservableGauge(string(m),
//		metric.WithUnit("1"),
//	)
//
//	r, err := c.meter.RegisterCallback(
//		func(ctx context.Context, o metric.Observer) error {
//			o.ObserveInt64(gauge, val,
//				metric.WithAttributes(
//					attribute.String("element", name),
//					attribute.String("flowID", c.flowID),
//				))
//			return nil
//		},
//		gauge,
//	)
//
//	if err != nil {
//		c.log.Error(err, "metric gauge err")
//		return
//	}
//
//	go func() {
//		time.Sleep(time.Second)
//		_ = r.Unregister()
//	}()
//}

func (c *Runner) incCounter(ctx context.Context, val int64, element string, m metrics.Metric) {

	if element == "" {
		return
	}

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
			attribute.String("element", element),
			attribute.String("flowID", c.flowID),
			attribute.String("projectID", c.projectID),
			attribute.String("metric", string(m)),
		),
	}
	counter.Add(ctx, val, attrs...)
}

func (c *Runner) addSpanError(span trace.Span, err error) {
	span.RecordError(err, trace.WithStackTrace(true))
	span.SetStatus(codes.Error, err.Error())
}

func (c *Runner) addSpanPortData(span trace.Span, data string) {
	span.AddEvent("data",
		trace.WithAttributes(attribute.String("payload", data)))
}
