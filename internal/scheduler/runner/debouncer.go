package runner

import (
	"context"
	"sync"
	"time"
)

// ReconcileDebouncer coalesces rapid reconcile requests to protect the K8s API server.
// Multiple reconcile requests within the debounce window are merged, and only the
// latest update is executed after the window expires.
type ReconcileDebouncer struct {
	interval time.Duration

	mu            sync.Mutex
	timer         *time.Timer
	pendingUpdate func()
	pending       bool
}

// NewReconcileDebouncer creates a new debouncer with the specified interval.
// Recommended interval: 100-500ms depending on expected reconcile frequency.
func NewReconcileDebouncer(interval time.Duration) *ReconcileDebouncer {
	return &ReconcileDebouncer{
		interval: interval,
	}
}

// Debounce schedules an update function to be executed after the debounce interval.
// If called multiple times within the interval, only the latest update is executed.
// Returns true if this call was debounced (merged with pending), false if it will execute.
func (d *ReconcileDebouncer) Debounce(ctx context.Context, update func()) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	wasDebounced := d.pending
	d.pendingUpdate = update
	d.pending = true

	if d.timer != nil {
		// Timer already running - just update the pending function
		return wasDebounced
	}

	// Start new timer
	d.timer = time.AfterFunc(d.interval, func() {
		d.mu.Lock()
		fn := d.pendingUpdate
		d.pendingUpdate = nil
		d.pending = false
		d.timer = nil
		d.mu.Unlock()

		if fn != nil {
			fn()
		}
	})

	return false
}

// Flush immediately executes any pending update and stops the timer.
// Useful for cleanup or when immediate execution is required.
func (d *ReconcileDebouncer) Flush() {
	d.mu.Lock()
	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	fn := d.pendingUpdate
	d.pendingUpdate = nil
	d.pending = false
	d.mu.Unlock()

	if fn != nil {
		fn()
	}
}

// Stop cancels any pending update without executing it.
func (d *ReconcileDebouncer) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.timer != nil {
		d.timer.Stop()
		d.timer = nil
	}
	d.pendingUpdate = nil
	d.pending = false
}

// IsPending returns true if there's a pending update waiting to execute.
func (d *ReconcileDebouncer) IsPending() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.pending
}
