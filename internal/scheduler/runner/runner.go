package runner

import (
	"context"
	"fmt"
	"github.com/cenkalti/backoff/v4"
	"github.com/go-logr/logr"
	"github.com/goccy/go-json"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/pkg/errors"
	"github.com/spyzhov/ajson"
	"github.com/tiny-systems/errorpanic"
	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/internal/tracker"
	m "github.com/tiny-systems/module/module"
	perrors "github.com/tiny-systems/module/pkg/errors"
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
	"time"
)

type Runner struct {

	// CRD
	node v1alpha1.TinyNode

	// to control CRD
	manager resource.ManagerInterface

	// unique instance name
	name        string
	flowName    string
	projectName string

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

	// caching requests => responses, errors
	portMsg   cmap.ConcurrentMap[string, any]
	portRes   cmap.ConcurrentMap[string, any]
	portErr   cmap.ConcurrentMap[string, error]
	portNonce cmap.ConcurrentMap[string, string]

	// busy edge tracking (optimized - gauge created once)
	busyEdges     cmap.ConcurrentMap[string, struct{}]
	gaugeInitOnce sync.Once
}

const FromSignal = "signal"

func NewRunner(component m.Component) *Runner {
	return &Runner{
		component: component,
		nodeLock:  &sync.Mutex{},

		nodePortsLock: &sync.RWMutex{},

		// msg cache
		portMsg: cmap.New[any](),
		// response cache
		portRes: cmap.New[any](),
		// response error cache
		portErr: cmap.New[error](),
		// last port message nonce
		portNonce: cmap.New[string](),
		// busy edge tracking
		busyEdges: cmap.New[struct{}](),

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
		if p.Name == v1alpha1.ReconcilePort || p.Name == v1alpha1.ClientPort {
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
			c.log.Info("ReadStatus: port configuration is nil", "port", np.Name, "node", c.name)
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
		return nil, nil
	}

	var (
		portConfig = c.getPortConfig(msg.From, port)
		portData   interface{}
	)

	portInputData := reflect.New(reflect.TypeOf(nodePort.Configuration)).Elem()

	if msg.From == FromSignal {
		if err = json.Unmarshal(msg.Data, portInputData.Addr().Interface()); err != nil {
			return nil, err
		}
		portData = portInputData.Interface()

	} else if portConfig != nil && len(portConfig.Configuration) > 0 {
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
			c.log.Error(err, "msg handler: failed to evaluate edge configuration",
				"port", port,
				"from", msg.From,
				"edgeID", msg.EdgeID,
			)
			return nil, errors.Wrap(err, "eval port edge settings config")
		}
		// all good, we can say that's the data for incoming port
		if err = c.jsonEncodeDecode(configurationMap, portInputData.Addr().Interface()); err != nil {
			return nil, errors.Wrap(err, "map decode from config map to port input type")
		}
		portData = portInputData.Interface()
	} else {
		// default is the state of a port's config
		portData = nodePort.Configuration
	}

	// we do not send data from signals if they are not changed to prevent work disruptions due to periodic reconciliations
	if msg.From == FromSignal || port == v1alpha1.SettingsPort {
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

	u, err := uuid.NewUUID()
	if err != nil {
		return nil, err
	}

	ctx, inputSpan := c.tracer.Start(ctx, u.String(),
		trace.WithAttributes(attribute.String("to", utils.GetPortFullName(c.name, port))),
		trace.WithAttributes(attribute.String("from", msg.From)),
		trace.WithAttributes(attribute.String("flowID", c.flowName)),
		trace.WithAttributes(attribute.String("projectID", c.projectName)),
	)

	defer func() {
		if err != nil {
			c.addSpanError(inputSpan, err)
		}
		inputSpan.End()
	}()

	// send span data only is tracker is on
	if c.tracker.Active(c.projectName) {
		inputData, _ := json.Marshal(portData)
		c.addSpanPortData(inputSpan, string(inputData))
	}

	// track busy edge
	c.setBusyEdge(msg.EdgeID)
	defer c.clearBusyEdge(msg.EdgeID)

	var resp any

	err = errorpanic.Wrap(func() error {
		resp = c.component.Handle(ctx, c.DataHandler(msgHandler), port, portData)
		return utils.CheckForError(resp)
	})

	if err != nil {
		c.log.Error(err, "msg handler: component execution failed",
			"port", port,
			"node", c.name,
			"from", msg.From,
			"edgeID", msg.EdgeID,
		)
	}

	c.portErr.Set(port, err)
	c.portRes.Set(port, resp)

	return resp, err
}

func (c *Runner) DataHandler(outputHandler Handler) func(outputCtx context.Context, outputPort string, outputData any) any {

	return func(outputCtx context.Context, outputPort string, outputData any) any {
		if outputPort == v1alpha1.ReconcilePort {
			nodeUpdater, _ := outputData.(func(node *v1alpha1.TinyNode) error)

			err := c.manager.PatchNode(outputCtx, c.node, func(node *v1alpha1.TinyNode) error {
				if err := c.ReadStatus(&node.Status); err != nil {
					c.log.Error(err, "data handler: failed to read node status",
						"node", c.name,
					)
					return err
				}
				if nodeUpdater == nil {
					return nil
				}
				return nodeUpdater(node)
			})
			if err != nil {
				c.log.Error(err, "data handler: failed to patch node",
					"node", c.name,
				)
			}
			return err
		}

		u, err := uuid.NewUUID()
		if err != nil {
			return err
		}

		outputCtx, outputSpan := c.tracer.Start(outputCtx, u.String(), trace.WithAttributes(
			attribute.String("port", utils.GetPortFullName(c.name, outputPort)),
			attribute.String("flowID", c.flowName),
			attribute.String("projectID", c.projectName),
		))

		defer outputSpan.End()

		// send span data only is tracker is on
		if c.tracker.Active(c.projectName) {
			outputDataBytes, _ := json.Marshal(outputData)
			c.addSpanPortData(outputSpan, string(outputDataBytes))
		}

		res, err := c.outputHandler(trace.ContextWithSpanContext(outputCtx, trace.SpanContextFromContext(outputCtx)), outputPort, outputData, outputHandler)
		if err != nil {
			c.log.Error(err, "data handler: output handler failed",
				"port", outputPort,
				"node", c.name,
			)
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

	c.flowName = node.Labels[v1alpha1.FlowNameLabel]
	c.projectName = node.Labels[v1alpha1.ProjectNameLabel]
	c.name = node.Name

	//
	c.node = node

	return c
}

// Stop cancels all ongoing requests
func (c *Runner) Stop() {
	close(c.closeCh)
}

// sendToEdgeWithRetry sends a message to an edge with retry logic.
// It only retries on transient errors. Permanent errors and context cancellation stop immediately.
func (c *Runner) sendToEdgeWithRetry(ctx context.Context, edge v1alpha1.TinyNodeEdge, fromPort, edgeTo string, dataBytes []byte, responseConfig interface{}, handler Handler) (any, error) {
	msg := &Msg{
		To:     edgeTo,
		From:   fromPort,
		EdgeID: edge.ID,
		Data:   dataBytes,
		Resp:   responseConfig,
	}

	// Fast path: try once without retry overhead
	res, err := handler(ctx, msg)
	if err == nil {
		return res, nil
	}

	// Check if error is permanent - don't retry
	if perrors.IsPermanent(err) {
		c.log.Error(err, "send to edge: permanent error",
			"to", edgeTo,
			"edgeID", edge.ID,
		)
		return nil, err
	}

	// Context cancelled - don't retry
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Transient error - enter retry loop
	c.log.Error(err, "send to edge: transient error, starting retries",
		"to", edgeTo,
		"edgeID", edge.ID,
	)

	b := backoff.NewExponentialBackOff()
	b.InitialInterval = 1 * time.Second
	b.MaxInterval = 30 * time.Second
	b.MaxElapsedTime = 0

	var result any
	retryErr := backoff.Retry(func() error {
		res, err := handler(ctx, msg)
		if err != nil {
			if perrors.IsPermanent(err) {
				return backoff.Permanent(err)
			}
			return err
		}
		result = res
		return nil
	}, backoff.WithContext(b, ctx))

	if retryErr != nil {
		c.log.Error(retryErr, "send to edge: retry failed",
			"to", edgeTo,
			"edgeID", edge.ID,
		)
		return nil, retryErr
	}

	return result, nil
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
		return c.sendToEdgeWithRetry(ctx, v1alpha1.TinyNodeEdge{}, "", port, dataBytes, nil, handler)
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

	c.nodeLock.Unlock()

	wg, ctx := errgroup.WithContext(ctx)

	var (
		results    = make([]any, 0)
		errors     = make([]error, 0)
		resultLock = &sync.Mutex{}
	)

	var sourcePort m.Port
	for _, p := range c.getPorts() {
		if p.Name == port {
			sourcePort = p
			break
		}
	}

	fromPort := utils.GetPortFullName(c.name, port)
	for _, e := range edges {
		edge := e
		if edge.Port != port {
			continue
		}

		wg.Go(func() error {
			res, err := c.sendToEdgeWithRetry(ctx, edge, fromPort, edge.To, dataBytes, sourcePort.ResponseConfiguration, handler)

			resultLock.Lock()
			defer resultLock.Unlock()

			if err != nil {
				errors = append(errors, err)
				return nil
			}
			if res != nil {
				results = append(results, res)
			}
			return nil
		})
	}

	_ = wg.Wait()

	if len(results) > 0 {
		return results[0], nil
	}
	if len(errors) > 0 {
		return nil, errors[0]
	}
	return nil, nil
}

func (c *Runner) jsonEncodeDecode(input interface{}, output interface{}) error {
	b, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, output)
}

func (c *Runner) addSpanError(span trace.Span, err error) {
	span.RecordError(err, trace.WithStackTrace(true))
	span.SetStatus(codes.Error, err.Error())
}

func (c *Runner) addSpanPortData(span trace.Span, data string) {
	span.AddEvent("data",
		trace.WithAttributes(attribute.String("payload", data)))
}

// initGauge initializes the busy edge gauge once
func (c *Runner) initGauge() {
	c.gaugeInitOnce.Do(func() {
		gauge, err := c.meter.Int64ObservableGauge(string(metrics.MetricEdgeBusy),
			metric.WithUnit("1"),
		)
		if err != nil {
			c.log.Error(err, "failed to create busy edge gauge")
			return
		}

		_, err = c.meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
			for item := range c.busyEdges.IterBuffered() {
				o.ObserveInt64(gauge, 1,
					metric.WithAttributes(
						attribute.String("element", item.Key),
						attribute.String("flowID", c.flowName),
						attribute.String("projectID", c.projectName),
					))
			}
			return nil
		}, gauge)

		if err != nil {
			c.log.Error(err, "failed to register busy edge gauge callback")
		}
	})
}

// setBusyEdge marks an edge as busy
func (c *Runner) setBusyEdge(edgeID string) {
	if edgeID == "" {
		return
	}
	c.initGauge()
	c.busyEdges.Set(edgeID, struct{}{})
}

// clearBusyEdge removes an edge from the busy set
func (c *Runner) clearBusyEdge(edgeID string) {
	if edgeID == "" {
		return
	}
	c.busyEdges.Remove(edgeID)
}
