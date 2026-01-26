package runner

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"time"

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

	// dedupLock protects the dedup check-and-set operation to ensure atomicity.
	// Without this, concurrent signals could race between checking and setting cache values.
	dedupLock *sync.Mutex

	// reconcileDebouncer coalesces rapid reconcile requests to protect K8s API server
	reconcileDebouncer *ReconcileDebouncer

	// pendingNodeUpdaters accumulates metadata update functions during debounce window
	pendingNodeUpdaters   []func(*v1alpha1.TinyNode) error
	pendingNodeUpdatersLock *sync.Mutex
}

const (
	FromSignal = "signal"
)

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
		// dedup lock for atomic check-and-set
		dedupLock: &sync.Mutex{},

		// debounce reconcile requests to protect K8s API (1s window)
		reconcileDebouncer: NewReconcileDebouncer(time.Second),

		// accumulate metadata updaters during debounce window
		pendingNodeUpdatersLock: &sync.Mutex{},

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

// InvalidatePortCache clears the port cache so the next getPorts call
// will fetch fresh ports from the component. This should be called when
// the node spec changes, as port availability may depend on component state.
func (c *Runner) InvalidatePortCache() {
	c.nodePortsLock.Lock()
	defer c.nodePortsLock.Unlock()
	c.nodePorts = nil
}

// getPortNames returns list of port names for debugging
func (c *Runner) getPortNames() []string {
	ports := c.getPorts()
	names := make([]string, len(ports))
	for i, p := range ports {
		names[i] = p.Name
	}
	return names
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

	c.log.Info("runner msg handler: entering",
		"port", port,
		"from", msg.From,
		"to", msg.To,
		"node", c.name,
		"ctxErrOnEntry", ctx.Err(),
	)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // Ensure context is cancelled when handler returns

	go func() {
		select {
		case <-c.closeCh:
			c.log.Info("runner msg handler: closeCh received, cancelling context",
				"port", port,
				"node", c.name,
			)
			cancel()
		case <-ctx.Done():
			// Handler completed or parent context cancelled - exit goroutine
		}
	}()

	var nodePort *m.Port
	for _, p := range c.getPorts() {
		if p.Name == port {
			nodePort = &p
			break
		}
	}

	if nodePort == nil {
		c.log.Info("runner msg handler: port not found in component ports",
			"requestedPort", port,
			"availablePorts", c.getPortNames(),
			"from", msg.From,
			"node", c.name,
		)
		return nil, nil
	}

	if nodePort.Configuration == nil {
		c.log.Info("runner msg handler: port has nil configuration",
			"port", port,
			"from", msg.From,
			"node", c.name,
		)
		return nil, nil
	}

	var (
		portConfig = c.getPortConfig(msg.From, port)
		portData   interface{}
	)

	portInputData := reflect.New(reflect.TypeOf(nodePort.Configuration)).Elem()

	if msg.From == FromSignal {
		if err = json.Unmarshal(msg.Data, portInputData.Addr().Interface()); err != nil {
			c.log.Error(err, "runner msg handler: failed to unmarshal signal data",
				"port", port,
				"node", c.name,
				"dataSize", len(msg.Data),
			)
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
		// Lock to ensure atomic check-and-set of dedup cache.
		// Without this, concurrent signals could race: one checks cache, another updates it,
		// leading to incorrect dedup decisions or inconsistent cache state.
		c.dedupLock.Lock()
		if prevPortData, ok := c.portMsg.Get(port); ok && cmp.Equal(portData, prevPortData) {
			if prevPortNonce, ok := c.portNonce.Get(port); ok && msg.Nonce == prevPortNonce {
				c.log.Info("runner msg handler: skipping duplicate signal (same data and nonce)",
					"port", port,
					"node", c.name,
					"nonce", msg.Nonce,
				)
				prevResp, _ := c.portRes.Get(port)
				prevErr, _ := c.portErr.Get(port)
				c.dedupLock.Unlock()
				return prevResp, prevErr
			}
		}
		c.portMsg.Set(port, portData)
		c.portNonce.Set(port, msg.Nonce)
		c.dedupLock.Unlock()
	}

	u, err := uuid.NewUUID()
	if err != nil {
		return nil, err
	}

	// Create non-cancellable context that preserves parent span for proper trace hierarchy
	// This ensures context cancellation doesn't interfere with telemetry submission
	spanCtx := trace.ContextWithSpanContext(context.Background(), trace.SpanContextFromContext(ctx))
	spanCtx, inputSpan := c.tracer.Start(spanCtx, u.String(),
		trace.WithAttributes(attribute.String("to", utils.GetPortFullName(c.name, port))),
		trace.WithAttributes(attribute.String("from", msg.From)),
		trace.WithAttributes(attribute.String("flowID", c.flowName)),
		trace.WithAttributes(attribute.String("projectID", c.projectName)),
	)
	// Update ctx with new span for downstream propagation
	ctx = trace.ContextWithSpanContext(ctx, trace.SpanContextFromContext(spanCtx))

	defer func() {
		if err != nil {
			c.addSpanError(inputSpan, err)
		}
		inputSpan.End()
	}()

	// send span data only if tracker is on
	if c.tracker.Active(c.projectName) {
		inputData, _ := json.Marshal(portData)
		c.addSpanPortData(inputSpan, string(inputData))
	}

	var resp any

	// Add source node to context for ownership tracking
	sourceNode, _ := utils.ParseFullPortName(msg.From)
	if sourceNode != "" {
		ctx = utils.WithSourceNode(ctx, sourceNode)
	}

	handleStart := time.Now()
	c.log.Info("runner msg handler: calling component.Handle",
		"port", port,
		"node", c.name,
		"from", msg.From,
		"sourceNode", sourceNode,
		"ctxErrBeforeHandle", ctx.Err(),
	)

	err = errorpanic.Wrap(func() error {
		// track busy edge only if tracker is on
		ticker := time.NewTicker(time.Second * 3)
		defer ticker.Stop()

		c.setGauge(time.Now().Unix(), msg.EdgeID, metrics.MetricEdgeBusy)

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
		return utils.CheckForError(resp)
	})

	handleDuration := time.Since(handleStart)

	if err != nil {
		c.log.Error(err, "msg handler: component execution failed",
			"port", port,
			"node", c.name,
			"from", msg.From,
			"edgeID", msg.EdgeID,
			"handleDuration", handleDuration.String(),
			"ctxErrAfterHandle", ctx.Err(),
		)
	} else {
		c.log.Info("runner msg handler: component.Handle completed",
			"port", port,
			"node", c.name,
			"from", msg.From,
			"handleDuration", handleDuration.String(),
			"ctxErrAfterHandle", ctx.Err(),
			"hasResponse", resp != nil,
		)
	}

	c.portErr.Set(port, err)
	c.portRes.Set(port, resp)

	return resp, err
}

func (c *Runner) DataHandler(outputHandler Handler) func(outputCtx context.Context, outputPort string, outputData any) any {

	return func(outputCtx context.Context, outputPort string, outputData any) any {
		if outputPort == v1alpha1.ReconcilePort {
			// Node updater function - accumulate for batched execution
			if nodeUpdater, ok := outputData.(func(node *v1alpha1.TinyNode) error); ok && nodeUpdater != nil {
				c.pendingNodeUpdatersLock.Lock()
				c.pendingNodeUpdaters = append(c.pendingNodeUpdaters, nodeUpdater)
				c.pendingNodeUpdatersLock.Unlock()
			}

			// Capture current node state for debounced execution
			currentNode := c.node

			// Debounce node patch to protect K8s API from high RPS
			// Use background context since the debounced func executes async
			debounced := c.reconcileDebouncer.Debounce(outputCtx, func() {
				// Collect all pending updaters atomically
				c.pendingNodeUpdatersLock.Lock()
				updaters := c.pendingNodeUpdaters
				c.pendingNodeUpdaters = nil
				c.pendingNodeUpdatersLock.Unlock()

				err := c.manager.PatchNode(context.Background(), currentNode, func(node *v1alpha1.TinyNode) error {
					// Apply ALL accumulated metadata updates
					for _, updater := range updaters {
						if err := updater(node); err != nil {
							return err
						}
					}
					// THEN read status (so getControl() sees updated metadata)
					if err := c.ReadStatus(&node.Status); err != nil {
						c.log.Error(err, "data handler: failed to read node status",
							"node", c.name,
						)
						return err
					}
					return nil
				})
				if err != nil {
					c.log.Error(err, "data handler: failed to patch node",
						"node", c.name,
					)
				}
			})

			if debounced {
				c.log.Info("data handler: reconcile request debounced",
					"node", c.name,
					"pendingUpdaters", len(c.pendingNodeUpdaters),
				)
			}
			return nil
		}

		u, err := uuid.NewUUID()
		if err != nil {
			return err
		}

		// Create non-cancellable context that preserves parent span for proper trace hierarchy
		// This ensures context cancellation doesn't interfere with telemetry submission
		spanCtx := trace.ContextWithSpanContext(context.Background(), trace.SpanContextFromContext(outputCtx))
		spanCtx, outputSpan := c.tracer.Start(spanCtx, u.String(), trace.WithAttributes(
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

		// Pass span context to downstream for trace propagation
		res, err := c.outputHandler(trace.ContextWithSpanContext(outputCtx, trace.SpanContextFromContext(spanCtx)), outputPort, outputData, outputHandler)
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

	// Invalidate port cache when node spec changes, as component may now
	// expose different ports based on the new configuration
	c.InvalidatePortCache()

	return c
}

// Stop cancels all ongoing requests and calls component cleanup if implemented
func (c *Runner) Stop() {
	// Flush any pending reconcile updates before stopping
	if c.reconcileDebouncer != nil {
		c.reconcileDebouncer.Flush()
	}

	// Call OnDestroy on the component if it implements Destroyer interface
	if destroyer, ok := c.component.(m.Destroyer); ok {
		c.nodeLock.Lock()
		metadata := c.node.Status.Metadata
		c.nodeLock.Unlock()

		if metadata == nil {
			metadata = make(map[string]string)
		}
		destroyer.OnDestroy(metadata)
	}

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
			// Use IsNil to catch typed nils (e.g., (*T)(nil)) which != nil but are meaningless
			if !utils.IsNil(res) {
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
	// Don't record context cancellation as errors - they're expected during shutdown
	if errors.Is(err, context.Canceled) {
		return
	}
	span.RecordError(err, trace.WithStackTrace(true))
	span.SetStatus(codes.Error, err.Error())
}

func (c *Runner) addSpanPortData(span trace.Span, data string) {
	span.AddEvent("data",
		trace.WithAttributes(attribute.String("payload", data)))
}

func (c *Runner) setGauge(val int64, name string, m metrics.Metric) {
	if name == "" {
		return
	}
	gauge, _ := c.meter.Int64ObservableGauge(string(m),
		metric.WithUnit("1"),
	)

	r, err := c.meter.RegisterCallback(
		func(ctx context.Context, o metric.Observer) error {
			o.ObserveInt64(gauge, val,
				metric.WithAttributes(
					attribute.String("element", name),
					attribute.String("flowID", c.flowName),
					attribute.String("projectID", c.projectName),
				))
			return nil
		},
		gauge,
	)

	if err != nil {
		c.log.Error(err, "metric gauge err")
		return
	}

	go func() {
		time.Sleep(time.Second * 6)
		_ = r.Unregister()
	}()
}

// getPortByName returns the port configuration by name
func (c *Runner) getPortByName(portName string) *m.Port {
	for _, p := range c.getPorts() {
		if p.Name == portName {
			return &p
		}
	}
	return nil
}
