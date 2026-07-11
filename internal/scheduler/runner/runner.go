package runner

import (
	"context"
	stdjson "encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/goccy/go-json"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/pkg/errors"
	"github.com/tiny-systems/ajson"
	"github.com/tiny-systems/errorpanic"
	"github.com/tiny-systems/module/api/v1alpha1"
	m "github.com/tiny-systems/module/module"
	perrors "github.com/tiny-systems/module/pkg/errors"
	"github.com/tiny-systems/module/pkg/evaluator"
	"github.com/tiny-systems/module/pkg/metrics"
	"github.com/tiny-systems/module/pkg/resource"
	"github.com/tiny-systems/module/pkg/secret"
	"github.com/tiny-systems/module/pkg/schema"
	"github.com/tiny-systems/module/pkg/utils"
	"github.com/tiny-systems/module/pkg/wire"
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

	// execDurable caches the node's ExecutionModeLabel == durable check
	// (set in SetNode under nodeLock). Durable nodes mint/continue a run
	// identity in MsgHandler, which flips edge sends to fire-and-forget
	// durable publishing.
	execDurable bool

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
	tracer trace.Tracer
	meter  metric.Meter
	//
	nodeLock *sync.Mutex

	// settings dedup: skip Handle() when _settings data hasn't changed
	portMsg cmap.ConcurrentMap[string, any]

	// settingsSecretAt records (port → unix-nano) the last time settings
	// carrying a [[secret:...]] placeholder were delivered. Byte-identical
	// settings are normally deduped, but the Secret behind a placeholder can
	// be created or rotated without the raw bytes changing — so such settings
	// are re-delivered (OnSettings re-runs, re-resolving) once secretResolveTTL
	// has elapsed, rather than being skipped forever.
	settingsSecretAt cmap.ConcurrentMap[string, int64]

	// reconcileDebouncer coalesces rapid reconcile requests to protect K8s API server
	reconcileDebouncer *ReconcileDebouncer

	// pendingNodeUpdaters accumulates metadata update functions during debounce window
	pendingNodeUpdaters     []func(*v1alpha1.TinyNode) error
	pendingNodeUpdatersLock *sync.Mutex

	// activeEdges tracks busy edge timestamps for the single gauge callback
	activeEdges       cmap.ConcurrentMap[string, int64]
	gaugeOnce         sync.Once
	gaugeRegistration metric.Registration

	// retryEdges tracks live retry attempts per edge with the latest error string.
	// Populated while backoff.Retry loops inside sendToEdgeWithRetry; cleared on
	// success, permanent failure, or context cancellation.
	retryEdges             cmap.ConcurrentMap[string, *retryState]
	retryGaugeOnce         sync.Once
	retryGaugeRegistration metric.Registration

	// edgeCancels tracks cancel funcs for in-flight edge sends.
	// When SetNode detects an edge was removed, it cancels the in-flight send,
	// which propagates through gRPC to the target component.
	edgeCancels cmap.ConcurrentMap[string, context.CancelFunc]

	// runCtx is the runner's lifecycle context, cancelled when the runner stops.
	// Used for async operations (debounced patches) instead of context.Background()
	// to prevent stale writes after runner shutdown.
	runCtx    context.Context
	runCancel context.CancelFunc

	// state is the State backend injected via the Stateful capability interface.
	// nil if the component does not implement Stateful.
	state m.State
}

// snapshotRefresher is implemented by State backends that maintain an
// in-memory snapshot of node metadata (currently MetadataState). The runner
// type-asserts on this to refresh the snapshot after each reconcile.
//
// Backends without a snapshot (Redis, Postgres) don't implement it.
type snapshotRefresher interface {
	UpdateSnapshot(metadata map[string]string)
}

const (
	// FromSignal aliases wire.FromSignal so external publishers and
	// the runner's signal check can never drift apart.
	FromSignal = wire.FromSignal
)

// secretResolveTTL bounds how often byte-identical settings carrying a
// [[secret:...]] placeholder are re-delivered so OnSettings re-resolves them.
// Well under the 5-minute reconcile requeue, so re-resolution effectively
// happens once per reconcile (created/rotated Secrets picked up without a pod
// restart) while rapid reconcile bursts don't hammer the Secret API.
const secretResolveTTL = 60 * time.Second

// secretResolveTTLLapsed reports whether secretResolveTTL has elapsed since
// settings carrying a secret placeholder were last delivered for port. A
// missing timestamp (never recorded) counts as lapsed so the first re-delivery
// isn't withheld.
func (c *Runner) secretResolveTTLLapsed(port string) bool {
	last, ok := c.settingsSecretAt.Get(port)
	if !ok {
		return true
	}
	return time.Now().UnixNano()-last >= int64(secretResolveTTL)
}

func NewRunner(component m.Component) *Runner {
	runCtx, runCancel := context.WithCancel(context.Background())
	return &Runner{
		component: component,
		nodeLock:  &sync.Mutex{},

		nodePortsLock: &sync.RWMutex{},

		// settings dedup cache
		portMsg:          cmap.New[any](),
		settingsSecretAt: cmap.New[int64](),

		// debounce reconcile requests to protect K8s API (1s window)
		reconcileDebouncer: NewReconcileDebouncer(time.Second),

		// accumulate metadata updaters during debounce window
		pendingNodeUpdatersLock: &sync.Mutex{},

		// single gauge callback tracks all active edges
		activeEdges: cmap.New[int64](),

		// retry counters exposed as an observable gauge with "error" attribute
		retryEdges: cmap.New[*retryState](),

		// cancel funcs for in-flight edge sends
		edgeCancels: cmap.New[context.CancelFunc](),

		closeCh: make(chan struct{}),

		// lifecycle context for async operations (debounced patches)
		runCtx:    runCtx,
		runCancel: runCancel,
	}
}

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

func (c *Runner) SetManager(m resource.ManagerInterface) *Runner {
	c.manager = m
	return c
}

func (c *Runner) GetComponent() m.Component {
	return c.component
}

// SetState attaches a State backend to the runner. Called by the scheduler
// after Stateful.OnState is invoked on the component, so the runner can
// notify the backend about reconciles for snapshot refresh.
func (c *Runner) SetState(s m.State) *Runner {
	c.state = s
	return c
}

// GetState returns the attached State backend, or nil.
func (c *Runner) GetState() m.State {
	return c.state
}

// NotifyReconcile refreshes the State backend's snapshot from the latest
// node metadata. No-op if no state is attached or the backend doesn't
// expose a snapshot.
func (c *Runner) NotifyReconcile(metadata map[string]string) {
	if c.state == nil {
		return
	}
	if r, ok := c.state.(snapshotRefresher); ok {
		r.UpdateSnapshot(metadata)
	}
}

// dispatchCapability is authoritative for all five system ports. For
// non-system ports it returns handled=false, telling the caller to route
// the message through Component.Handle as usual.
//
// For system ports it always returns handled=true, even when the component
// does not implement the corresponding capability interface — in that case
// the call is a no-op. There is no legacy fallback: a component that needs
// settings/control/reconcile/identity/client must implement the respective
// capability interface (SettingsHandler, ControlHandler, ReconcileHandler,
// IdentityAware, ClientAware).
func (c *Runner) dispatchCapability(ctx context.Context, port string, data any) (m.Result, bool) {
	switch port {
	case v1alpha1.SettingsPort:
		if h, ok := c.component.(m.SettingsHandler); ok {
			if err := h.OnSettings(ctx, data); err != nil {
				return m.Fail(err), true
			}
		}
		return m.Result{}, true
	case v1alpha1.ControlPort:
		if h, ok := c.component.(m.ControlHandler); ok {
			if err := h.OnControl(ctx, data); err != nil {
				return m.Fail(err), true
			}
		}
		return m.Result{}, true
	case v1alpha1.ReconcilePort, v1alpha1.ClientPort, v1alpha1.IdentityPort:
		// These are dispatched by scheduler.Update directly via Phase 1.
		// Block any signal-based or stray delivery from reaching legacy
		// Handle as a safety net.
		return m.Result{}, true
	}
	return m.Result{}, false
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
		if p.Name == v1alpha1.ReconcilePort || p.Name == v1alpha1.ClientPort || p.Name == v1alpha1.IdentityPort {
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

// GetPort returns a copy of the named Port if the component declares it,
// or (zero, false) otherwise.
func (c *Runner) GetPort(port string) (m.Port, bool) {
	for _, p := range c.getPorts() {
		if p.Name == port {
			return p, true
		}
	}
	return m.Port{}, false
}

// MsgHandler processes msg to the embedded component
// applies port config for the given port if any
func (c *Runner) MsgHandler(ctx context.Context, msg *Msg, msgHandler Handler) (res any, err error) {
	_, port := utils.ParseFullPortName(msg.To)
	if port == "" {
		return nil, fmt.Errorf("input port is empty")
	}

	// Durable-run identity. A message carrying a RunID continues that run;
	// a business-port message arriving at a durable node without one is the
	// run's entry — mint. Either way the identity rides the context down to
	// sendToEdgeWithRetry, which stamps outgoing edge messages so emits ride
	// the durable (fire-and-forget, work-queue) path instead of blocking.
	// System ports (_control, _settings, …) never mint runs.
	if msg.RunID != "" {
		// Redelivery protection beyond the broker's duplicate window: a hop
		// whose ledger record already exists ran to completion (or failed
		// terminally) — skip the component so its side effects never
		// re-execute. The record was written AFTER the step's emits were
		// durably stored, so skipping loses nothing downstream.
		if msg.StepKey != "" && c.state != nil {
			exec := c.state.Scoped(m.ScopeExecution, msg.RunID)
			if _, recorded, _ := exec.Get(ctx, StepLedgerKey(msg.StepKey)); recorded {
				c.log.Info("runner msg handler: step already recorded, skipping durable replay",
					"runID", msg.RunID,
					"stepKey", msg.StepKey,
					"node", c.name,
				)
				return nil, nil
			}
		}
		ctx = WithRun(ctx, NewRunInfo(msg.RunID, msg.StepKey))
	} else if c.isDurable() && !strings.HasPrefix(port, "_") {
		run := MintRun()
		ctx = WithRun(ctx, run)
		c.log.Info("runner msg handler: minted durable run",
			"runID", run.RunID,
			"port", port,
			"node", c.name,
		)
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

	// Cache Done channel before spawning goroutine to avoid Go context race
	// where concurrent access to cancelCtx.Done() can panic if the internal
	// atomic.Value holds a function instead of chan struct{} (Go issue #73866).
	done := ctx.Done()

	go func() {
		select {
		case <-c.closeCh:
			c.log.Info("runner msg handler: closeCh received, cancelling context",
				"port", port,
				"node", c.name,
			)
			cancel()
		case <-done:
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

	type exprError struct {
		expr string
		err  error
	}

	var (
		portConfig = c.getPortConfig(msg.From, port)
		portData   interface{}
		exprErrors []exprError // Collect expression errors for later span recording
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
		}).WithErrorCallback(func(expr string, err error) {
			exprErrors = append(exprErrors, exprError{expr: expr, err: err})
			c.log.Error(err, "expression evaluation failed",
				"expression", expr,
				"port", port,
				"from", msg.From,
				"edgeID", msg.EdgeID,
			)
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

		// Detect edge config keys that don't match target struct json tags
		if configMap, ok := configurationMap.(map[string]interface{}); ok {
			if orphaned := findOrphanedKeys(configMap, reflect.TypeOf(nodePort.Configuration)); len(orphaned) > 0 {
				c.log.Error(fmt.Errorf("edge config has keys that don't match target port struct"),
					"orphaned keys will be silently dropped during deserialization",
					"orphanedKeys", orphaned,
					"port", port,
					"edgeID", msg.EdgeID,
					"from", msg.From,
					"node", c.name,
				)
			}
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

	// Skip settings delivery when data hasn't changed between reconciliations.
	// Exception: settings carrying a [[secret:...]] placeholder are re-delivered
	// once secretResolveTTL has elapsed even when the raw bytes are identical —
	// the Secret behind the placeholder can be created or rotated without the
	// stored settings changing, so OnSettings must re-run to re-resolve it.
	// OnSettings is idempotent (it ran on every reconcile before this dedup).
	if port == v1alpha1.SettingsPort {
		hasSecret := secret.ContainsPlaceholder(portData)
		if prevPortData, ok := c.portMsg.Get(port); ok && cmp.Equal(portData, prevPortData) {
			if !hasSecret || !c.secretResolveTTLLapsed(port) {
				c.log.Info("runner msg handler: skipping settings (data unchanged)",
					"port", port,
					"node", c.name,
				)
				return nil, nil
			}
			c.log.Info("runner msg handler: re-delivering settings to re-resolve secrets (TTL lapsed)",
				"port", port,
				"node", c.name,
			)
		}
		c.portMsg.Set(port, portData)
		if hasSecret {
			c.settingsSecretAt.Set(port, time.Now().UnixNano())
		}
	}

	u, err := uuid.NewUUID()
	if err != nil {
		return nil, err
	}

	// Create non-cancellable context that preserves trace hierarchy (including remote span contexts)
	spanCtx, inputSpan := c.tracer.Start(context.WithoutCancel(ctx), u.String(),
		trace.WithAttributes(attribute.String("to", utils.GetPortFullName(c.name, port))),
		trace.WithAttributes(attribute.String("from", msg.From)),
		trace.WithAttributes(attribute.String("flowID", c.flowName)),
		trace.WithAttributes(attribute.String("projectID", c.projectName)),
	)
	// Update ctx with new span for downstream propagation
	ctx = trace.ContextWithSpanContext(ctx, trace.SpanContextFromContext(spanCtx))

	// Record any expression evaluation errors to span
	for _, exprErr := range exprErrors {
		inputSpan.AddEvent("expression_error",
			trace.WithAttributes(
				attribute.String("expression", exprErr.expr),
				attribute.String("error", exprErr.err.Error()),
			),
		)
	}

	defer func() {
		if err != nil {
			c.addSpanError(inputSpan, err)
		}
		inputSpan.End()
	}()

	// Always record span port data for tracing
	inputData, _ := json.Marshal(portData)
	c.addSpanPortData(inputSpan, string(inputData))

	var resp m.Result

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
		// track busy edge metric
		c.setGauge(time.Now().Unix(), msg.EdgeID)
		defer c.clearGauge(msg.EdgeID)

		ticker := time.NewTicker(time.Second * 3)
		defer ticker.Stop()

		go func() {
			for {
				select {
				case <-done:
					return
				case t := <-ticker.C:
					c.setGauge(t.Unix(), msg.EdgeID)
				}
			}
		}()

		var handled bool
		resp, handled = c.dispatchCapability(ctx, port, portData)
		if !handled {
			resp = c.component.Handle(ctx, c.DataHandler(msgHandler), port, portData)
		}
		return resp.Err()
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
			"hasResponse", resp.Value() != nil,
		)
	}

	c.writeStepRecord(ctx, err, handleStart, handleDuration)

	return resp.Value(), err
}

// writeStepRecord persists a durable hop's completion to the step ledger —
// AFTER the handler returned, so every emit it published is already durably
// stored (record exists ⇒ downstream hops exist). Failures are recorded as
// terminal: the durability layer never re-runs business errors; that belongs
// to explicit retry components / edge RetryPolicy. Best-effort — a lost write
// only means a redelivery would re-execute the step, the pre-2b behavior.
func (c *Runner) writeStepRecord(ctx context.Context, handleErr error, startedAt time.Time, dur time.Duration) {
	run, ok := RunFrom(ctx)
	if !ok || c.state == nil {
		return
	}

	rec := StepRecord{
		Node:        c.name,
		Status:      StepStatusDone,
		Emits:       run.Emits(),
		StartedAt:   startedAt,
		CompletedAt: time.Now(),
		DurationMs:  dur.Milliseconds(),
	}
	if handleErr != nil {
		rec.Status = StepStatusFailed
		rec.Error = handleErr.Error()
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}

	// The handler's ctx may already be cancelled (durable hops ack right
	// after this) — detach, but bound the write.
	wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	exec := c.state.Scoped(m.ScopeExecution, run.RunID)
	if err := exec.Set(wctx, StepLedgerKey(run.StepKey), b); err != nil {
		c.log.Error(err, "runner msg handler: step ledger write failed",
			"runID", run.RunID,
			"stepKey", run.StepKey,
			"node", c.name,
		)
	}
}

func (c *Runner) DataHandler(outputHandler Handler) func(outputCtx context.Context, outputPort string, outputData any) m.Result {

	return func(outputCtx context.Context, outputPort string, outputData any) m.Result {
		// ControlPort messages trigger a status update to reflect new control state
		// This ensures UI sees updated control values (e.g., Running status, Stop button)
		if outputPort == v1alpha1.ControlPort {
			// Invalidate port cache so ReadStatus picks up fresh Ports() with new control value
			c.InvalidatePortCache()

			// Capture current node state for debounced execution
			currentNode := c.node

			// Debounce status update to protect K8s API
			// MUST also drain pendingNodeUpdaters since this shares reconcileDebouncer
			// with ReconcilePort — otherwise metadata updates (e.g. clearMetadata) are lost
			c.reconcileDebouncer.Debounce(outputCtx, func() {
				// Collect all pending updaters atomically
				c.pendingNodeUpdatersLock.Lock()
				updaters := c.pendingNodeUpdaters
				c.pendingNodeUpdaters = nil
				c.pendingNodeUpdatersLock.Unlock()

				err := c.manager.PatchNode(c.runCtx, currentNode, func(node *v1alpha1.TinyNode) error {
					// Apply ALL accumulated metadata updates first
					for _, updater := range updaters {
						if err := updater(node); err != nil {
							return err
						}
					}
					return c.ReadStatus(&node.Status)
				})
				if err != nil {
					c.log.Error(err, "data handler: failed to patch node for control port",
						"node", c.name,
					)
				}
			})
			return m.Result{}
		}

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

				err := c.manager.PatchNode(c.runCtx, currentNode, func(node *v1alpha1.TinyNode) error {
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
			return m.Result{}
		}

		u, err := uuid.NewUUID()
		if err != nil {
			return m.Fail(err)
		}

		// Create non-cancellable context that preserves trace hierarchy
		spanCtx, outputSpan := c.tracer.Start(context.WithoutCancel(outputCtx), u.String(), trace.WithAttributes(
			attribute.String("port", utils.GetPortFullName(c.name, outputPort)),
			attribute.String("flowID", c.flowName),
			attribute.String("projectID", c.projectName),
		))

		defer outputSpan.End()

		// Always record span port data for tracing
		outputDataBytes, _ := json.Marshal(outputData)
		c.addSpanPortData(outputSpan, string(outputDataBytes))

		// Pass span context to downstream for trace propagation
		res, err := c.outputHandler(trace.ContextWithSpanContext(outputCtx, trace.SpanContextFromContext(spanCtx)), outputPort, outputData, outputHandler)
		if err != nil {
			c.log.Error(err, "data handler: output handler failed",
				"port", outputPort,
				"node", c.name,
			)
			return m.Fail(err)
		}
		return m.Ok(res)
	}

}

func (c *Runner) Node() v1alpha1.TinyNode {
	c.nodeLock.Lock()

	defer c.nodeLock.Unlock()
	return c.node
}

// isDurable reports whether this node opted into durable execution
// (ExecutionModeLabel), cached by SetNode.
func (c *Runner) isDurable() bool {
	c.nodeLock.Lock()
	defer c.nodeLock.Unlock()
	return c.execDurable
}

// SetNode updates specs and decides do we need to restart which handles by Run method
func (c *Runner) SetNode(node v1alpha1.TinyNode) *Runner {

	c.nodeLock.Lock()
	oldEdges := c.node.Spec.Edges
	c.flowName = node.Labels[v1alpha1.FlowNameLabel]
	c.projectName = node.Labels[v1alpha1.ProjectNameLabel]
	c.execDurable = node.Labels[v1alpha1.ExecutionModeLabel] == v1alpha1.ExecutionModeDurable
	c.name = node.Name
	c.node = node
	c.nodeLock.Unlock()

	// Cancel in-flight sends for removed edges
	c.cancelRemovedEdges(oldEdges, node.Spec.Edges)

	// Invalidate port cache when node spec changes, as component may now
	// expose different ports based on the new configuration
	c.InvalidatePortCache()

	return c
}

// cancelRemovedEdges cancels in-flight sends for edges present in oldEdges but absent in newEdges.
func (c *Runner) cancelRemovedEdges(oldEdges, newEdges []v1alpha1.TinyNodeEdge) {
	newSet := make(map[string]struct{}, len(newEdges))
	for _, e := range newEdges {
		newSet[e.ID] = struct{}{}
	}
	for _, e := range oldEdges {
		if _, ok := newSet[e.ID]; ok {
			continue
		}
		if cancel, ok := c.edgeCancels.Get(e.ID); ok {
			c.log.Info("cancelling in-flight send for removed edge",
				"edgeID", e.ID,
				"port", e.Port,
				"to", e.To,
				"node", c.name,
			)
			cancel()
		}
	}
}

// Stop cancels all ongoing requests and calls component cleanup if implemented.
// Use this when the TinyNode is being deleted and cleanup is required.
func (c *Runner) Stop() {
	// Flush any pending reconcile updates before stopping
	if c.reconcileDebouncer != nil {
		c.reconcileDebouncer.Flush()
	}

	// Cancel lifecycle context after flush so flushed patches complete,
	// but any new async patches are prevented
	c.runCancel()

	// Unregister gauge callback to prevent leaks
	if c.gaugeRegistration != nil {
		_ = c.gaugeRegistration.Unregister()
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

// StopWithoutCleanup cancels all ongoing requests but does NOT call OnDestroy.
// Use this during pod shutdown when the TinyNode still exists and will be
// picked up by another pod. OnDestroy should only be called when the node
// is actually being deleted.
func (c *Runner) StopWithoutCleanup() {
	// Flush any pending reconcile updates before stopping
	if c.reconcileDebouncer != nil {
		c.reconcileDebouncer.Flush()
	}

	// Cancel lifecycle context after flush
	c.runCancel()

	// Unregister gauge callback to prevent leaks
	if c.gaugeRegistration != nil {
		_ = c.gaugeRegistration.Unregister()
	}

	close(c.closeCh)
}

// sendToEdgeWithRetry dispatches an edge per its RetryPolicy. Default
// (policy == nil or MaxAttempts <= 1) = single shot, the historical
// 2026-05-20 contract.
//
// Retries used to be implicit at this layer with infinite backoff —
// removed 2026-05-20 after a Claude-401 storm burned money. The new
// model (2026-06-01) is explicit per-edge: flow authors opt in by
// setting edge.RetryPolicy. NonRetryableErrorCodes short-circuit the
// loop even when MaxAttempts > 1 — components signal these via
// module/pkg/errors.NonRetryable(code, err).
//
// Backoff is exponential (BackoffCoefficient, default 2.0) starting
// from InitialDelayMs (default 1s), capped at MaxDelayMs (default 30s).
//
// Name kept to avoid touching every caller; the function honors a
// configurable policy now but the default is unchanged.
func (c *Runner) sendToEdgeWithRetry(ctx context.Context, edge v1alpha1.TinyNodeEdge, fromPort, edgeTo string, dataBytes []byte, responseConfig interface{}, handler Handler) (any, error) {
	// Track cancel func so SetNode can cancel in-flight sends for removed edges.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if edge.ID != "" {
		c.edgeCancels.Set(edge.ID, cancel)
		defer c.edgeCancels.Remove(edge.ID)
	}

	policy := edge.RetryPolicy
	maxAttempts := 1
	if policy != nil && policy.MaxAttempts > 1 {
		maxAttempts = policy.MaxAttempts
	}
	// Per-attempt timeout — derives a tighter ctx around each
	// handler call when the edge's policy sets one. The transport's
	// internal default still applies as a floor when the policy says
	// 0 or no policy is set.
	if policy != nil && policy.TimeoutMs > 0 {
		var perAttemptCancel context.CancelFunc
		ctx, perAttemptCancel = context.WithTimeout(ctx, time.Duration(policy.TimeoutMs)*time.Millisecond)
		defer perAttemptCancel()
	}

	msg := &Msg{
		To:     edgeTo,
		From:   fromPort,
		EdgeID: edge.ID,
		Data:   dataBytes,
		Resp:   responseConfig,
	}

	// Durable run: stamp the outgoing hop with the run identity and a
	// deterministic idempotency key. Derived ONCE, outside the retry loop —
	// a retry after a lost broker ack re-publishes the same StepKey and the
	// duplicate window collapses it to one stored message.
	run, durable := RunFrom(ctx)
	if durable {
		msg.RunID = run.RunID
		msg.StepKey = run.NextStepKey(edge.ID)
		// Carry the reply address so every hop keeps it; the terminal hop
		// (To == ReplyTarget) diverts to the origin instance in the transport.
		msg.ReplySubject = run.ReplySubject
		msg.ReplyTarget = run.ReplyTarget
		msg.ReplyDeadlineUnixMs = run.ReplyDeadlineUnixMs
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		res, err := handler(ctx, msg)
		if err == nil {
			// Collect the durably-stored hop so this step's ledger record
			// can carry it for reconciler re-drive.
			if durable {
				run.RecordEmit(EmitRecord{
					To:                  msg.To,
					From:                msg.From,
					EdgeID:              msg.EdgeID,
					StepKey:             msg.StepKey,
					Data:                dataBytes,
					ReplySubject:        msg.ReplySubject,
					ReplyTarget:         msg.ReplyTarget,
					ReplyDeadlineUnixMs: msg.ReplyDeadlineUnixMs,
				})
			}
			return res, nil
		}
		lastErr = err

		// Short-circuit on a code that the policy lists as non-retryable.
		if code := perrors.ErrorCode(err); code != "" && policy != nil {
			for _, blocked := range policy.NonRetryableErrorCodes {
				if blocked == code {
					c.log.Info("send to edge: short-circuit on non-retryable code",
						"to", edgeTo, "edgeID", edge.ID, "code", code,
					)
					return nil, err
				}
			}
		}

		if attempt == maxAttempts {
			break
		}
		delay := backoffDelay(policy, attempt)
		c.log.Info("send to edge: retrying after backoff",
			"to", edgeTo, "edgeID", edge.ID,
			"attempt", attempt, "of", maxAttempts,
			"delay", delay,
			"err", err.Error(),
		)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}

	c.log.Error(lastErr, "send to edge: failed after retries exhausted",
		"to", edgeTo, "edgeID", edge.ID, "attempts", maxAttempts,
	)
	return nil, lastErr
}

// backoffDelay computes the wait between attempt N and attempt N+1.
// Defaults pin to: 1s initial, 2.0 multiplier, 30s cap. The policy's
// BackoffCoefficient is a string so the CRD stays YAML-clean; "" or
// unparseable means use the default 2.0.
func backoffDelay(policy *v1alpha1.EdgeRetryPolicy, attempt int) time.Duration {
	initial := time.Second
	maxDelay := 30 * time.Second
	coefficient := 2.0

	if policy != nil {
		if policy.InitialDelayMs > 0 {
			initial = time.Duration(policy.InitialDelayMs) * time.Millisecond
		}
		if policy.MaxDelayMs > 0 {
			maxDelay = time.Duration(policy.MaxDelayMs) * time.Millisecond
		}
		if policy.BackoffCoefficient != "" {
			if v, err := strconv.ParseFloat(policy.BackoffCoefficient, 64); err == nil && v >= 1.0 {
				coefficient = v
			}
		}
	}

	d := float64(initial)
	for i := 1; i < attempt; i++ {
		d *= coefficient
	}
	if time.Duration(d) > maxDelay {
		return maxDelay
	}
	return time.Duration(d)
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

func (c *Runner) ensureEdgeGauge() {
	c.gaugeOnce.Do(func() {
		gauge, err := c.meter.Int64ObservableGauge(string(metrics.MetricEdgeBusy),
			metric.WithUnit("1"),
		)
		if err != nil {
			c.log.Error(err, "failed to create edge busy gauge")
			return
		}

		c.gaugeRegistration, err = c.meter.RegisterCallback(
			func(ctx context.Context, o metric.Observer) error {
				for item := range c.activeEdges.IterBuffered() {
					o.ObserveInt64(gauge, item.Val,
						metric.WithAttributes(
							attribute.String("element", item.Key),
							attribute.String("flowID", c.flowName),
							attribute.String("projectID", c.projectName),
						))
				}
				return nil
			},
			gauge,
		)
		if err != nil {
			c.log.Error(err, "failed to register edge busy gauge callback")
		}
	})
}

func (c *Runner) setGauge(val int64, edgeID string) {
	if edgeID == "" {
		return
	}
	c.ensureEdgeGauge()
	c.activeEdges.Set(edgeID, val)
}

func (c *Runner) clearGauge(edgeID string) {
	c.activeEdges.Remove(edgeID)
}

// retryState holds the live retry counter and latest transient error for one edge.
type retryState struct {
	count   int64
	lastErr string
}

func (c *Runner) ensureRetryGauge() {
	if c.meter == nil {
		return
	}
	c.retryGaugeOnce.Do(func() {
		gauge, err := c.meter.Int64ObservableGauge(string(metrics.MetricEdgeRetryCount),
			metric.WithUnit("1"),
		)
		if err != nil {
			c.log.Error(err, "failed to create edge retry gauge")
			return
		}

		c.retryGaugeRegistration, err = c.meter.RegisterCallback(
			func(ctx context.Context, o metric.Observer) error {
				for item := range c.retryEdges.IterBuffered() {
					state := item.Val
					if state == nil {
						continue
					}
					o.ObserveInt64(gauge, state.count,
						metric.WithAttributes(
							attribute.String("element", item.Key),
							attribute.String("flowID", c.flowName),
							attribute.String("projectID", c.projectName),
							attribute.String("error", state.lastErr),
						))
				}
				return nil
			},
			gauge,
		)
		if err != nil {
			c.log.Error(err, "failed to register edge retry gauge callback")
		}
	})
}

// incRetry increments the retry counter for an edge and records the latest error.
// Safe to call when the runner has no meter / uninitialized retryEdges (test harness).
func (c *Runner) incRetry(edgeID string, err error) {
	if edgeID == "" || c.meter == nil {
		return
	}
	c.ensureRetryGauge()
	errStr := ""
	if err != nil {
		errStr = err.Error()
	}
	state, ok := c.retryEdges.Get(edgeID)
	if !ok || state == nil {
		state = &retryState{}
	}
	state.count++
	state.lastErr = errStr
	c.retryEdges.Set(edgeID, state)
}

// clearRetry drops the retry state for an edge (called on success or terminal failure).
func (c *Runner) clearRetry(edgeID string) {
	if c.meter == nil {
		return
	}
	c.retryEdges.Remove(edgeID)
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

// getJSONTagNames returns a map of json tag name → struct field index for a struct type.
// Skips fields without json tags or with json:"-".
func getJSONTagNames(t reflect.Type) map[string]int {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	result := make(map[string]int, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			continue
		}
		result[name] = i
	}
	return result
}

// findOrphanedKeys compares map keys from evaluated edge config against the target struct's
// json tags. Returns keys present in the map but missing from the struct — these will be
// silently dropped by json.Unmarshal, which is the root cause of bugs like routeName vs route.
// Recurses into nested structs and slice element types. Skips types implementing json.Unmarshaler
// (e.g. RouteName) since they handle their own deserialization.
func findOrphanedKeys(data map[string]interface{}, t reflect.Type) []string {
	if t == nil {
		return nil
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}

	// Skip types that implement json.Unmarshaler — they handle their own keys
	unmarshalerType := reflect.TypeOf((*stdjson.Unmarshaler)(nil)).Elem()
	if t.Implements(unmarshalerType) || reflect.PointerTo(t).Implements(unmarshalerType) {
		return nil
	}

	tags := getJSONTagNames(t)
	var orphaned []string

	for key, val := range data {
		fieldIdx, ok := tags[key]
		if !ok {
			orphaned = append(orphaned, key)
			continue
		}
		// Recurse into nested structs and slice items
		if val == nil {
			continue
		}
		fieldType := t.Field(fieldIdx).Type
		if fieldType.Kind() == reflect.Ptr {
			fieldType = fieldType.Elem()
		}
		switch fieldType.Kind() {
		case reflect.Struct:
			if nested, ok := val.(map[string]interface{}); ok {
				for _, o := range findOrphanedKeys(nested, fieldType) {
					orphaned = append(orphaned, key+"."+o)
				}
			}
		case reflect.Slice:
			elemType := fieldType.Elem()
			if elemType.Kind() == reflect.Ptr {
				elemType = elemType.Elem()
			}
			if elemType.Kind() == reflect.Struct {
				if arr, ok := val.([]interface{}); ok {
					for i, item := range arr {
						if nested, ok := item.(map[string]interface{}); ok {
							for _, o := range findOrphanedKeys(nested, elemType) {
								orphaned = append(orphaned, fmt.Sprintf("%s[%d].%s", key, i, o))
							}
						}
					}
				}
			}
		}
	}
	sort.Strings(orphaned)
	return orphaned
}
