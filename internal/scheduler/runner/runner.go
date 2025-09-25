package runner

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/goccy/go-json"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/spyzhov/ajson"
	"github.com/tiny-systems/errorpanic"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/tracker"
	m "github.com/tiny-systems/module/module"
	"github.com/tiny-systems/module/pkg/evaluator"
	"github.com/tiny-systems/module/pkg/metrics"
	"github.com/tiny-systems/module/pkg/resource"
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
	"time"
)

type Runner struct {

	// CRD
	node v1alpha1.TinyNode

	// to control CRD
	manager resource.ManagerInterface

	// unique instance name
	name      string
	flowID    string
	projectID string

	// underlying component
	component m.Component
	//
	log logr.Logger
	// to control K8s resources

	closeCh chan struct{}

	//
	nodePorts     []m.Port
	nodePortsLock *sync.RWMutex

	//
	tracer  trace.Tracer
	tracker tracker.Manager

	meter metric.Meter
	//
	nodeLock *sync.Mutex

	// if second request to the port comes we cancel previous if it is still running to avoid goroutine leak
	portCancel cmap.ConcurrentMap[string, context.CancelFunc]

	// caching requests => responses, errors
	portMsg   cmap.ConcurrentMap[string, any]
	portRes   cmap.ConcurrentMap[string, any]
	portErr   cmap.ConcurrentMap[string, error]
	portNonce cmap.ConcurrentMap[string, string]
	//
	reconciling *atomic.Bool
}

const FromSignal = "signal"

func NewRunner(component m.Component) *Runner {
	return &Runner{
		component: component,
		nodeLock:  &sync.Mutex{},

		nodePortsLock: &sync.RWMutex{},

		reconciling: &atomic.Bool{},
		//
		portCancel: cmap.New[context.CancelFunc](),
		//
		portMsg:   cmap.New[any](),
		portRes:   cmap.New[any](),
		portErr:   cmap.New[error](),
		portNonce: cmap.New[string](),

		//
		closeCh: make(chan struct{}),
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

func (c *Runner) SetManager(m resource.ManagerInterface) *Runner {
	c.manager = m
	return c
}

func (c *Runner) GetComponent() m.Component {
	return c.component
}

// getPorts get ports cache or an actual node ports
func (c *Runner) getPorts() []m.Port {
	c.nodePortsLock.RLock()

	if c.nodePorts != nil {
		c.nodePortsLock.RUnlock()
		return c.nodePorts
	}

	c.nodePortsLock.RUnlock()
	return c.getNodePorts()
}

// getNodePorts gets actual ports and caches it
func (c *Runner) getNodePorts() []m.Port {
	c.nodePortsLock.Lock()

	defer c.nodePortsLock.Unlock()

	c.nodePorts = c.component.Ports()
	return c.nodePorts
}

// ReadStatus reads status
func (c *Runner) ReadStatus(status *v1alpha1.TinyNodeStatus) error {

	status.Status = "OK"
	status.Error = false
	status.Ports = make([]v1alpha1.TinyNodePortStatus, 0)

	var ports []m.Port
	// trim system ports
	for _, p := range c.getNodePorts() {
		if p.Name == m.ReconcilePort || p.Name == m.ClientPort {
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
				c.log.Error(err, "ReadStatus: create schema error")
			} else {
				schemaData, _ := schemaConf.MarshalJSON()
				portStatus.Schema = schemaData
			}
			// real port data
			confData, err := json.Marshal(np.Configuration)
			if err != nil {
				c.log.Error(err, "ReadStatus: encode port configuration error")
			}
			portStatus.Configuration = confData
		} else {
			log.Warn().Str("port", np.Name).Str("node", c.name).Msg("configuration is nil")
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

func (c *Runner) HasPort(port string) bool {
	for _, p := range c.getPorts() {
		if p.Name == port {
			return true
		}
	}
	return false
}

// MsgHandler processes msg to the embedded component
// applies port config for the given port if any
func (c *Runner) MsgHandler(ctx context.Context, msg *Msg, msgHandler Handler) (res any, err error) {
	_, port := utils.ParseFullPortName(msg.To)

	if port == "" {
		return nil, fmt.Errorf("input port is empty")
	}

	ctx, cancel := context.WithCancel(ctx)
	go func() {
		defer cancel()
		// cancels all ongoing requests
		<-c.closeCh
	}()

	var nodePort *m.Port
	for _, p := range c.getPorts() {
		if p.Name == port {
			nodePort = &p
			break
		}
	}

	if nodePort == nil || nodePort.Configuration == nil {
		// component has no such port
		return nil, nil
	}

	var (
		portConfig = c.getPortConfig(msg.From, port)
		portData   interface{}
	)

	portInputData := reflect.New(reflect.TypeOf(nodePort.Configuration)).Elem()

	if msg.From == FromSignal {
		// from signal controller (outside)

		if err = json.Unmarshal(msg.Data, portInputData.Addr().Interface()); err != nil {
			return nil, err
		}
		portData = portInputData.Interface()

	} else if portConfig != nil && len(portConfig.Configuration) > 0 {
		// we have edge config

		requestDataNode, err := ajson.Unmarshal(msg.Data)
		if err != nil {
			return nil, errors.Wrap(err, "ajson parse requestData payload error")
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
			return nil, errors.Wrap(err, "eval port edge settings config")
		}
		// all good, we can say that's the data for incoming port
		// adaptive
		if err = c.jsonEncodeDecode(configurationMap, portInputData.Addr().Interface()); err != nil {
			return nil, errors.Wrap(err, "map decode from config map to port input type")
		}
		portData = portInputData.Interface()

	} else {
		// default is the state of a port's config
		portData = nodePort.Configuration
	}

	// we do not send data from signals if they are not changed to prevent work disruptions due to periodic reconciliations
	if msg.From == FromSignal || port == m.SettingsPort {
		//
		if prevPortData, ok := c.portMsg.Get(port); ok && cmp.Equal(portData, prevPortData) {
			if prevPortNonce, ok := c.portNonce.Get(port); ok && msg.Nonce == prevPortNonce {
				prevResp, _ := c.portRes.Get(port)
				prevErr, _ := c.portErr.Get(port)
				return prevResp, prevErr
			}
		}
		c.portMsg.Set(port, portData)
		c.portNonce.Set(port, msg.Nonce)
	}

	c.log.Info("exec component handler", "msg", msg)

	if prevCancel, ok := c.portCancel.Get(port); ok && prevCancel != nil {
		// Important
		// we do not wait prev handler goroutine to be close and expect it respects its context
		prevCancel()
	}

	c.portCancel.Set(port, cancel)

	u, err := uuid.NewUUID()
	if err != nil {
		return nil, err
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

	var resp any

	err = errorpanic.Wrap(func() error {
		// panic safe

		ticker := time.NewTicker(time.Second * 2)
		defer ticker.Stop()

		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case t := <-ticker.C:
					c.setGauge(t.Unix(), msg.EdgeID, metrics.MetricEdgeBusy)
				}
			}
		}()

		resp = c.component.Handle(ctx, c.DataHandler(msgHandler), port, portData)

		if err = utils.CheckForError(resp); err != nil {
			return err
		}
		return nil
	})

	c.portErr.Set(port, err)
	c.portRes.Set(port, resp)

	return resp, err
}

func (c *Runner) DataHandler(outputHandler Handler) func(outputCtx context.Context, outputPort string, outputData any) any {

	return func(outputCtx context.Context, outputPort string, outputData any) any {
		c.log.Info("component callback handler", "port", outputPort, "node", c.name)

		if outputPort == m.ReconcilePort {

			if nodeUpdater, ok := outputData.(func(node *v1alpha1.TinyNode) error); ok {
				if c.reconciling.Load() {
					c.log.Info("already reconciling", c.name)
					// reconciling is already in progress
					return nil
				}

				c.reconciling.Store(true)

				return c.manager.PatchNode(outputCtx, c.node, func(node *v1alpha1.TinyNode) error {
					if err := c.ReadStatus(&node.Status); err != nil {
						return err
					}
					return nodeUpdater(node)
				})
			} else {
				c.log.Info("no tiny node updater given for reconciliation")
			}
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

		res, err := c.outputHandler(trace.ContextWithSpanContext(outputCtx, trace.SpanContextFromContext(outputCtx)), outputPort, outputData, outputHandler)
		if err != nil {
			return err
		}
		return res
	}

}

func (c *Runner) Node() v1alpha1.TinyNode {
	c.nodeLock.Lock()

	defer c.nodeLock.Unlock()
	return c.node
}

// SetNode updates specs and decides do we need to restart which handles by Run method
func (c *Runner) SetNode(node v1alpha1.TinyNode) *Runner {

	c.nodeLock.Lock()
	defer c.nodeLock.Unlock()
	//
	c.reconciling.Store(false)
	c.flowID = node.Labels[v1alpha1.FlowIDLabel]
	c.projectID = node.Labels[v1alpha1.ProjectIDLabel]
	c.name = node.Name

	//
	c.node = node

	return c
}

// Cancel cancels all ongoing requests
func (c *Runner) Cancel() {
	close(c.closeCh)
}

func (c *Runner) outputHandler(ctx context.Context, port string, data interface{}, handler Handler) (any, error) {

	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	if handler == nil {
		return nil, nil
	}

	if node, _ := utils.ParseFullPortName(port); node != "" {
		// we already have full port name, means no need to check edges (useful for input/outputs)
		return handler(ctx, &Msg{
			To:   port,
			Data: dataBytes,
		})
	}

	var (
		uniqueTo = make(map[string]struct{})
		edges    = make([]v1alpha1.TinyNodeEdge, 0)
	)

	c.nodeLock.Lock()
	// unique destinations
	// same nodes may have multiple edges togethers on a different flows
	for _, e := range c.node.Spec.Edges[:] {
		if _, ok := uniqueTo[e.Port+e.To]; ok {
			continue
		}
		uniqueTo[e.Port+e.To] = struct{}{}
		edges = append(edges, e)
	}

	// unique
	c.nodeLock.Unlock()

	// get all edges to connected nodes
	wg, ctx := errgroup.WithContext(ctx)

	var (
		firstResult any
		resLock     = &sync.Mutex{}
	)

	var sourcePort m.Port

	for _, p := range c.getPorts() {
		if p.Name != port {
			continue
		}
		sourcePort = p
	}

	for _, e := range edges {
		var edge = e
		//
		if edge.Port != port {
			// edge is not configured for this port as source
			continue
		}

		fromPort := utils.GetPortFullName(c.name, port)
		wg.Go(func() error {
			// send to destination
			// track how many messages component send

			res, er := handler(ctx, &Msg{
				To:     edge.To,
				From:   fromPort,
				EdgeID: edge.ID,
				Data:   dataBytes,
				Resp:   sourcePort.ResponseConfiguration,
			})
			if er != nil {
				return er
			}

			resLock.Lock()
			defer resLock.Unlock()
			firstResult = res

			return er
		})
	}
	// wait while all port will be unblocked
	return firstResult, wg.Wait()
}

func (c *Runner) jsonEncodeDecode(input interface{}, output interface{}) error {
	b, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, output)
}

func (c *Runner) setGauge(val int64, name string, m metrics.Metric) {
	if name == "" {
		return
	}
	gauge, _ := c.meter.Int64ObservableGauge(string(m),
		metric.WithUnit("1"),
	)
	_, err := c.meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			o.ObserveInt64(gauge, val,
				metric.WithAttributes(
					// element could be nodeID or edgeID
					attribute.String("element", name),
					attribute.String("flowID", c.flowID),
					attribute.String("projectID", c.projectID),
				))
			return nil
		},
		gauge,
	)

	if err != nil {
		c.log.Error(err, "metric gauge err")
		return
	}
}

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
