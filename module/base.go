package module

import (
	"context"

	"github.com/tiny-systems/module/api/v1alpha1"
)

// Base is an optional helper components embed to remove lifecycle
// boilerplate. It satisfies Stateful, IdentityAware, and ClientAware,
// stashing the injected dependencies for later use through accessor
// methods.
//
// Authors who embed Base no longer need to switch on the system ports
// inside Handle; they just call b.State(), b.Identity(), b.Client() when
// needed.
//
// Components that override any of OnState, OnIdentity, OnClient must call
// the embedded Base method too if they want the accessors to keep working:
//
//	func (c *MyComponent) OnIdentity(id v1alpha1.NodeIdentity) {
//	    c.Base.OnIdentity(id)
//	    // ... custom work
//	}
//
// Base does not implement ReconcileHandler or SettingsHandler — those are
// always component-specific and authors implement them directly.
type Base struct {
	state    State
	identity v1alpha1.NodeIdentity
	client   K8sClient
	emit     Handler
}

// State returns the injected state backend. Returns nil if the framework
// has not yet injected one (i.e., before runner setup completes) or if
// the component is not declared as Stateful.
func (b *Base) State() State { return b.state }

// Identity returns the node's identity. Zero value before OnIdentity fires.
func (b *Base) Identity() v1alpha1.NodeIdentity { return b.identity }

// Client returns the K8s client. Returns nil before OnClient fires or if
// the platform did not deliver a client to this component.
func (b *Base) Client() K8sClient { return b.client }

// Emit forwards to the injected long-lived Handler. Use it from goroutines
// (tickers, cron loops, watchers) to publish to output or system ports.
// No-op if OnEmitter has not yet fired.
func (b *Base) Emit(ctx context.Context, port string, data any) any {
	if b.emit == nil {
		return nil
	}
	return b.emit(ctx, port, data)
}

// Emitter returns the raw injected Handler for callers that prefer to
// stash it once and call it directly. Returns nil before OnEmitter fires.
func (b *Base) Emitter() Handler { return b.emit }

// OnState satisfies Stateful.
func (b *Base) OnState(s State) { b.state = s }

// OnIdentity satisfies IdentityAware.
func (b *Base) OnIdentity(id v1alpha1.NodeIdentity) { b.identity = id }

// OnClient satisfies ClientAware.
func (b *Base) OnClient(c K8sClient) { b.client = c }

// OnEmitter satisfies EmitterAware.
func (b *Base) OnEmitter(e Handler) { b.emit = e }

// Static interface assertions — fails to compile if drift occurs.
var (
	_ Stateful      = (*Base)(nil)
	_ IdentityAware = (*Base)(nil)
	_ ClientAware   = (*Base)(nil)
	_ EmitterAware  = (*Base)(nil)
)
