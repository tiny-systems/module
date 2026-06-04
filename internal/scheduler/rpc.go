package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"

	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/module"
)

// handleRPC dispatches an MCP tool call directly to a component
// without touching the flow graph. msg.To carries the bare component
// name (no flow prefix, no port suffix); msg.Data is the JSON payload
// the agent supplied. The dispatcher:
//
//  1. Looks up the component in the local registry,
//  2. Confirms it implements AgentTool,
//  3. Creates a fresh Instance() — no settings, no metadata, no
//     persisted state. The agent's payload is everything,
//  4. Unmarshals msg.Data into the input port's struct type,
//  5. Calls Handle() with a capture handler that records the first
//     emit on the configured output port,
//  6. Returns the captured payload to the JetStream reply path.
//
// Stateful components don't opt into AgentTool and so never reach
// this path. Anything that does opt in must work from a cold instance
// with only the call payload.
func (s *Schedule) handleRPC(ctx context.Context, msg *runner.Msg) (any, error) {
	componentName := msg.To
	if componentName == "" {
		return nil, fmt.Errorf("rpc: msg.To is empty (need component name)")
	}

	cmp, ok := s.componentsMap.Get(componentName)
	if !ok {
		return nil, fmt.Errorf("rpc: component %q not registered on this module", componentName)
	}

	agentTool, ok := cmp.(module.AgentTool)
	if !ok {
		return nil, fmt.Errorf("rpc: component %q does not opt into AgentTool", componentName)
	}
	info := agentTool.AgentTool()
	inputPort := info.InputPort
	if inputPort == "" {
		inputPort = "in"
	}
	outputPort := info.OutputPort
	if outputPort == "" {
		outputPort = "out"
	}

	instance := cmp.Instance()
	if instance == nil {
		return nil, fmt.Errorf("rpc: %s.Instance() returned nil", componentName)
	}

	inputType, err := portConfigurationType(instance, inputPort)
	if err != nil {
		return nil, fmt.Errorf("rpc: %s: %w", componentName, err)
	}
	input := reflect.New(inputType).Elem()
	if len(msg.Data) > 0 {
		if err := json.Unmarshal(msg.Data, input.Addr().Interface()); err != nil {
			return nil, fmt.Errorf("rpc: unmarshal %s input: %w", componentName, err)
		}
	}

	capture := &rpcCapture{port: outputPort}
	result := instance.Handle(ctx, capture.handler, inputPort, input.Interface())
	if err := result.Err(); err != nil {
		return nil, err
	}

	// Prefer the emit on the configured output port; fall back to
	// the Handle Result's value when the component returned its
	// response directly (some authors do this for one-shot tools).
	if capture.captured.Load() {
		return capture.payload, nil
	}
	return result.Value(), nil
}

// portConfigurationType returns the reflect.Type of the named port's
// Configuration struct on the component instance, so the dispatcher
// can build a typed value to JSON-unmarshal the agent's payload into.
func portConfigurationType(instance module.Component, portName string) (reflect.Type, error) {
	for _, p := range instance.Ports() {
		if p.Name != portName {
			continue
		}
		if p.Configuration == nil {
			return nil, fmt.Errorf("port %q has no Configuration shape", portName)
		}
		return reflect.TypeOf(p.Configuration), nil
	}
	return nil, fmt.Errorf("port %q not found on component", portName)
}

// rpcCapture wraps a module.Handler that records the first emit on
// the configured output port. Subsequent emits return Ok(nil) and are
// otherwise discarded — RPC mode is single-shot by design.
type rpcCapture struct {
	port string

	mu       sync.Mutex
	captured atomicBool
	payload  any
}

func (c *rpcCapture) handler(ctx context.Context, port string, data any) module.Result {
	if port != c.port {
		return module.Ok(nil)
	}
	c.mu.Lock()
	if !c.captured.Load() {
		c.payload = data
		c.captured.Store(true)
	}
	c.mu.Unlock()
	return module.Ok(nil)
}

// atomicBool keeps the tiny capture-state machine self-contained and
// import-free at the file level.
type atomicBool struct{ v bool }

func (a *atomicBool) Load() bool   { return a.v }
func (a *atomicBool) Store(b bool) { a.v = b }
