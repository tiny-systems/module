package module

import (
	"context"

	"github.com/tiny-systems/module/api/v1alpha1"
)

// SettingsHandler is the typed alternative to handling v1alpha1.SettingsPort
// in Component.Handle. The runner dispatches to OnSettings before falling
// through to Handle, so components implementing this interface no longer
// need a port switch for settings.
//
// The settings argument is the concrete type declared as the SettingsPort
// Configuration value (already deserialized from the edge config).
type SettingsHandler interface {
	OnSettings(ctx context.Context, settings any) error
}

// ReconcileHandler is the typed alternative to handling v1alpha1.ReconcilePort.
// Called by the scheduler each time the TinyNode reconciles. Components use
// this to restore state from node metadata or react to spec changes.
//
// The framework guarantees this fires before OnSettings on a fresh runner,
// which removes the need for the settingsFromPort guard pattern: settings
// always wins over reconcile-restored state because settings runs second.
type ReconcileHandler interface {
	OnReconcile(ctx context.Context, node v1alpha1.TinyNode) error
}

// IdentityAware is the typed alternative to handling v1alpha1.IdentityPort.
// The framework calls OnIdentity once during Update, before OnReconcile.
// Components that need to namespace local resources (PVC paths, prefixes)
// implement this and stash the identity for later use.
type IdentityAware interface {
	OnIdentity(id v1alpha1.NodeIdentity)
}

// ClientAware is the typed alternative to handling v1alpha1.ClientPort.
// The framework calls OnClient once during Update, before OnReconcile.
// Components needing direct K8s access stash the client for later use.
type ClientAware interface {
	OnClient(client K8sClient)
}

// ControlHandler is the typed alternative to handling v1alpha1.ControlPort
// in Component.Handle. Control messages drive dashboard widgets and
// runtime control affordances (Start/Stop buttons etc.).
type ControlHandler interface {
	OnControl(ctx context.Context, control any) error
}

// EmitterAware components receive a long-lived Handler they can call from
// any goroutine to emit messages on their output ports — control updates,
// reconcile-port metadata patches, downstream signals from background
// loops (tickers, cron, watchers).
//
// The capability methods (OnSettings, OnReconcile, OnControl) intentionally
// do not pass a handler argument; components that need to emit outside
// the lifecycle calls implement EmitterAware (or embed Base, which does
// it for them).
//
// The injected Handler stays valid for the runner's lifetime. Calling it
// after the component is destroyed is a no-op routed through cancelled
// contexts.
type EmitterAware interface {
	OnEmitter(emit Handler)
}

// Lifecycle dispatch order enforced by the framework on a fresh runner:
//
//   1. OnIdentity   — node knows who it is
//   2. OnClient     — K8s client wired up
//   3. OnState      — state backend wired up (Stateful interface)
//   4. OnReconcile  — restore from metadata, react to spec
//   5. OnSettings   — apply user-provided settings (wins over reconcile)
//
// On subsequent reconciles, OnReconcile fires again (settings only re-fires
// when the configured value changes — existing dedup is preserved).
