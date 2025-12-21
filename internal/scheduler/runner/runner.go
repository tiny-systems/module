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
	"github.com/rs/zerolog/log"
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
	//
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

	c.log.Info("msg handler: received message",
		"to", msg.To,
		"from", msg.From,
		"port", port,
		"edgeID", msg.EdgeID,
		"dataSize", len(msg.Data),
	)

	if port == "" {
		c.log.Error(fmt.Errorf("input port is empty"), "msg handler: invalid message - empty port",
			"to", msg.To,
			"from", msg.From,
		)
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
		c.log.Info("msg handler: port not found or has no configuration, skipping",
			"port", port,
			"to", msg.To,
			"from", msg.From,
			"nodePortNil", nodePort == nil,
		)
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
			c.log.Error(err, "msg handler: failed to unmarshal signal data",
				"port", port,
				"dataSize", len(msg.Data),
			)
			return nil, err
		}
		portData = portInputData.Interface()
		c.log.Info("msg handler: processed signal data",
			"port", port,
		)

	} else if portConfig != nil && len(portConfig.Configuration) > 0 {
		// we have edge config
		c.log.Info("msg handler: applying edge configuration",
			"port", port,
			"from", msg.From,
			"edgeID", msg.EdgeID,
		)

		requestDataNode, err := ajson.Unmarshal(msg.Data)
		if err != nil {
			c.log.Error(err, "msg handler: failed to parse request data payload",
				"port", port,
				"from", msg.From,
				"dataSize", len(msg.Data),
			)
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
		// adaptive
		if err = c.jsonEncodeDecode(configurationMap, portInputData.Addr().Interface()); err != nil {
			c.log.Error(err, "msg handler: failed to decode configuration to port input type",
				"port", port,
				"from", msg.From,
			)
			return nil, errors.Wrap(err, "map decode from config map to port input type")
		}
		portData = portInputData.Interface()
		c.log.Info("msg handler: edge configuration applied successfully",
			"port", port,
			"from", msg.From,
		)

	} else {
		// default is the state of a port's config
		c.log.Info("msg handler: using default port configuration",
			"port", port,
			"from", msg.From,
		)
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

	c.log.Info("exec component handler", "msg", msg)

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

	var resp any

	err = errorpanic.Wrap(func() error {
		// panic safe

		ticker := time.NewTicker(time.Second * 3)
		defer ticker.Stop()

		// send right away
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

		c.log.Info("msg handler: invoking component handler",
			"port", port,
			"node", c.name,
		)

		resp = c.component.Handle(ctx, c.DataHandler(msgHandler), port, portData)

		if err = utils.CheckForError(resp); err != nil {
			c.log.Error(err, "msg handler: component handler returned error",
				"port", port,
				"node", c.name,
			)
			return err
		}
		return nil
	})

	if err != nil {
		c.log.Error(err, "msg handler: component execution failed",
			"port", port,
			"node", c.name,
			"from", msg.From,
			"edgeID", msg.EdgeID,
		)
	} else {
		c.log.Info("msg handler: component execution completed",
			"port", port,
			"node", c.name,
			"hasResponse", resp != nil,
		)
	}

	c.portErr.Set(port, err)
	c.portRes.Set(port, resp)

	return resp, err
}

func (c *Runner) DataHandler(outputHandler Handler) func(outputCtx context.Context, outputPort string, outputData any) any {

	return func(outputCtx context.Context, outputPort string, outputData any) any {
		c.log.Info("data handler: component output callback",
			"port", outputPort,
			"node", c.name,
			"hasData", outputData != nil,
		)

		if outputPort == v1alpha1.ReconcilePort {
			// special case
			c.log.Info("data handler: processing reconcile port",
				"node", c.name,
			)
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

		c.log.Info("data handler: sending to output handler",
			"port", outputPort,
			"node", c.name,
		)

		res, err := c.outputHandler(trace.ContextWithSpanContext(outputCtx, trace.SpanContextFromContext(outputCtx)), outputPort, outputData, outputHandler)
		if err != nil {
			c.log.Error(err, "data handler: output handler failed",
				"port", outputPort,
				"node", c.name,
			)
			return err
		}

		c.log.Info("data handler: output handler completed",
			"port", outputPort,
			"node", c.name,
			"hasResponse", res != nil,
		)
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

// sendToEdgeWithRetry sends a message to an edge with infinite retry logic.
// It only stops retrying if the error is permanent or the context is cancelled.
func (c *Runner) sendToEdgeWithRetry(ctx context.Context, edge v1alpha1.TinyNodeEdge, fromPort, edgeTo string, dataBytes []byte, responseConfig interface{}, handler Handler) (any, error) {
	c.log.Info("send to edge: starting",
		"to", edgeTo,
		"from", fromPort,
		"edgeID", edge.ID,
		"dataSize", len(dataBytes),
	)

	// Configure exponential backoff: 1s -> 2s -> 4s -> ... -> 30s (max)
	// MaxElapsedTime = 0 means infinite retry
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = 1 * time.Second
	b.MaxInterval = 30 * time.Second
	b.MaxElapsedTime = 0 // Never stop retrying

	var result any
	attempt := 0

	operation := func() error {
		attempt++

		c.log.Info("send to edge: attempt",
			"to", edgeTo,
			"from", fromPort,
			"edgeID", edge.ID,
			"attempt", attempt,
		)

		res, err := handler(ctx, &Msg{
			To:     edgeTo,
			From:   fromPort,
			EdgeID: edge.ID,
			Data:   dataBytes,
			Resp:   responseConfig,
		})

		if err != nil {
			// Check if this is a permanent error that should not be retried
			if perrors.IsPermanent(err) {
				c.log.Error(err, "send to edge: permanent error, will not retry",
					"to", edgeTo,
					"from", fromPort,
					"edgeID", edge.ID,
					"attempt", attempt,
				)
				return backoff.Permanent(err)
			}

			// Transient error - log and retry
			c.log.Error(err, "send to edge: transient error, will retry",
				"to", edgeTo,
				"from", fromPort,
				"edgeID", edge.ID,
				"attempt", attempt,
			)
			return err
		}

		// Success
		if attempt > 1 {
			c.log.Info("send to edge: succeeded after retries",
				"to", edgeTo,
				"from", fromPort,
				"edgeID", edge.ID,
				"attempts", attempt,
			)
		} else {
			c.log.Info("send to edge: succeeded",
				"to", edgeTo,
				"from", fromPort,
				"edgeID", edge.ID,
				"hasResponse", res != nil,
			)
		}

		result = res
		return nil
	}

	// Retry until success, permanent error, or context cancelled
	err := backoff.RetryNotify(
		operation,
		backoff.WithContext(b, ctx),
		func(err error, duration time.Duration) {
			c.log.Info("send to edge: scheduling retry",
				"to", edgeTo,
				"from", fromPort,
				"edgeID", edge.ID,
				"nextRetryIn", duration,
				"error", err.Error(),
				"attempt", attempt,
			)
		},
	)

	if err != nil {
		// Check if it's a permanent error
		if perrors.IsPermanent(err) {
			c.log.Error(err, "send to edge: failed with permanent error",
				"to", edgeTo,
				"from", fromPort,
				"edgeID", edge.ID,
				"attempts", attempt,
			)
			return nil, err
		}
		// Context cancelled
		c.log.Info("send to edge: stopped (context cancelled)",
			"to", edgeTo,
			"from", fromPort,
			"edgeID", edge.ID,
			"attempts", attempt,
		)
		return nil, err
	}

	return result, nil
}

func (c *Runner) outputHandler(ctx context.Context, port string, data interface{}, handler Handler) (any, error) {
	c.log.Info("output handler: processing output",
		"port", port,
		"node", c.name,
	)

	dataBytes, err := json.Marshal(data)
	if err != nil {
		c.log.Error(err, "output handler: failed to marshal output data",
			"port", port,
			"node", c.name,
		)
		return nil, err
	}

	c.log.Info("output handler: marshaled data",
		"port", port,
		"dataSize", len(dataBytes),
	)

	if handler == nil {
		c.log.Info("output handler: no handler provided, skipping",
			"port", port,
		)
		return nil, nil
	}

	if node, _ := utils.ParseFullPortName(port); node != "" {
		// we already have full port name, means no need to check edges (useful for input/outputs)
		c.log.Info("output handler: direct send (full port name)",
			"port", port,
			"targetNode", node,
		)
		// Use retry logic even for direct calls
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

	// unique
	c.nodeLock.Unlock()

	c.log.Info("output handler: found edges for port",
		"port", port,
		"edgeCount", len(edges),
	)

	// get all edges to connected nodes
	wg, ctx := errgroup.WithContext(ctx)

	var (
		results    = make([]any, 0)
		errors     = make([]error, 0)
		resultLock = &sync.Mutex{}
	)

	var sourcePort m.Port

	for _, p := range c.getPorts() {
		if p.Name != port {
			continue
		}
		sourcePort = p
	}

	matchingEdges := 0
	for _, e := range edges {
		var edge = e
		//
		if edge.Port != port {
			// edge is not configured for this port as source
			continue
		}
		matchingEdges++

		fromPort := utils.GetPortFullName(c.name, port)
		c.log.Info("output handler: sending to edge",
			"from", fromPort,
			"to", edge.To,
			"edgeID", edge.ID,
		)

		wg.Go(func() error {
			// Send to destination with infinite retry
			res, err := c.sendToEdgeWithRetry(ctx, edge, fromPort, edge.To, dataBytes, sourcePort.ResponseConfiguration, handler)

			// Collect results and errors
			resultLock.Lock()
			defer resultLock.Unlock()

			if err != nil {
				c.log.Error(err, "output handler: edge send failed",
					"from", fromPort,
					"to", edge.To,
					"edgeID", edge.ID,
				)
				errors = append(errors, err)
				// Don't return error - let other edges continue
				return nil
			}

			c.log.Info("output handler: edge send completed",
				"from", fromPort,
				"to", edge.To,
				"edgeID", edge.ID,
				"hasResponse", res != nil,
			)

			if res != nil {
				results = append(results, res)
			}

			return nil
		})
	}

	if matchingEdges == 0 {
		c.log.Info("output handler: no matching edges for port",
			"port", port,
			"node", c.name,
		)
	}

	// Wait for ALL edges to complete
	_ = wg.Wait()

	c.log.Info("output handler: all edges completed",
		"port", port,
		"successCount", len(results),
		"errorCount", len(errors),
	)

	// Return first non-nil result, or first error if all failed
	if len(results) > 0 {
		return results[0], nil
	}

	if len(errors) > 0 {
		c.log.Error(errors[0], "output handler: returning first error",
			"port", port,
			"totalErrors", len(errors),
		)
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
					// element could be nodeID or edgeID
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
			attribute.String("flowID", c.flowName),
			attribute.String("projectID", c.projectName),
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
