package runner

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/tiny-systems/module/api/v1alpha1"
	m "github.com/tiny-systems/module/module"
)

// settingsCapableMock implements both Component and SettingsHandler. It
// records whether OnSettings was called and how many times Handle was
// called, so tests can verify the capability path wins.
type settingsCapableMock struct {
	mockComponent
	onSettingsCalls atomic.Int32
	handleCalls     atomic.Int32
	settingsErr     error
	gotSettings     atomic.Value // any
}

func (c *settingsCapableMock) Handle(_ context.Context, _ m.Handler, _ string, _ any) any {
	c.handleCalls.Add(1)
	return nil
}

func (c *settingsCapableMock) OnSettings(_ context.Context, settings any) error {
	c.onSettingsCalls.Add(1)
	c.gotSettings.Store(settings)
	return c.settingsErr
}

func (c *settingsCapableMock) Instance() m.Component { return c }

// controlCapableMock implements ControlHandler.
type controlCapableMock struct {
	mockComponent
	onControlCalls atomic.Int32
	handleCalls    atomic.Int32
}

func (c *controlCapableMock) Handle(_ context.Context, _ m.Handler, _ string, _ any) any {
	c.handleCalls.Add(1)
	return nil
}

func (c *controlCapableMock) OnControl(_ context.Context, _ any) error {
	c.onControlCalls.Add(1)
	return nil
}

func (c *controlCapableMock) Instance() m.Component { return c }

// legacyMock is a plain Component without any capability interfaces.
// Verifies the dispatch falls through to Handle on every system port.
type legacyMock struct {
	mockComponent
	handleCalls atomic.Int32
}

func (c *legacyMock) Handle(_ context.Context, _ m.Handler, _ string, _ any) any {
	c.handleCalls.Add(1)
	return nil
}

func (c *legacyMock) Instance() m.Component { return c }

// TestDispatchCapability_SettingsRoutesToOnSettings verifies that a component
// implementing SettingsHandler gets its OnSettings called instead of Handle,
// and Handle is not called as a fallback.
func TestDispatchCapability_SettingsRoutesToOnSettings(t *testing.T) {
	cmp := &settingsCapableMock{}
	r := createTestRunnerWithComponent(cmp)

	type fakeSettings struct{ Delay int }
	want := fakeSettings{Delay: 42}

	resp, handled := r.dispatchCapability(context.Background(), v1alpha1.SettingsPort, want)

	if !handled {
		t.Fatalf("dispatchCapability returned handled=false for SettingsHandler component")
	}
	if resp != nil {
		t.Errorf("dispatchCapability returned resp=%v on success; want nil", resp)
	}
	if cmp.onSettingsCalls.Load() != 1 {
		t.Errorf("OnSettings called %d times; want 1", cmp.onSettingsCalls.Load())
	}
	got := cmp.gotSettings.Load()
	if got != want {
		t.Errorf("OnSettings received %+v; want %+v", got, want)
	}
}

// TestDispatchCapability_SettingsErrorPropagated verifies that an error
// from OnSettings flows back through dispatchCapability so the runner
// can record it as the response error.
func TestDispatchCapability_SettingsErrorPropagated(t *testing.T) {
	wantErr := errors.New("bad settings")
	cmp := &settingsCapableMock{settingsErr: wantErr}
	r := createTestRunnerWithComponent(cmp)

	resp, handled := r.dispatchCapability(context.Background(), v1alpha1.SettingsPort, struct{}{})

	if !handled {
		t.Fatalf("dispatchCapability returned handled=false; expected true even on error")
	}
	gotErr, ok := resp.(error)
	if !ok {
		t.Fatalf("resp is not an error; got %T (%v)", resp, resp)
	}
	if !errors.Is(gotErr, wantErr) {
		t.Errorf("resp error = %v; want %v", gotErr, wantErr)
	}
}

// TestDispatchCapability_ControlRoutesToOnControl verifies the same
// dispatch contract for ControlHandler.
func TestDispatchCapability_ControlRoutesToOnControl(t *testing.T) {
	cmp := &controlCapableMock{}
	r := createTestRunnerWithComponent(cmp)

	_, handled := r.dispatchCapability(context.Background(), v1alpha1.ControlPort, struct{}{})

	if !handled {
		t.Fatalf("dispatchCapability returned handled=false for ControlHandler component")
	}
	if cmp.onControlCalls.Load() != 1 {
		t.Errorf("OnControl called %d times; want 1", cmp.onControlCalls.Load())
	}
}

// TestDispatchCapability_SystemPortsAlwaysHandled verifies the no-fallback
// contract: for every system port, dispatchCapability returns handled=true
// even when the component does not implement the corresponding capability
// interface. The call is a silent no-op; Handle is never invoked for a
// system port.
func TestDispatchCapability_SystemPortsAlwaysHandled(t *testing.T) {
	cmp := &legacyMock{}
	r := createTestRunnerWithComponent(cmp)

	systemPorts := []string{
		v1alpha1.SettingsPort,
		v1alpha1.ControlPort,
		v1alpha1.ReconcilePort,
		v1alpha1.ClientPort,
		v1alpha1.IdentityPort,
	}

	for _, port := range systemPorts {
		resp, handled := r.dispatchCapability(context.Background(), port, struct{}{})
		if !handled {
			t.Errorf("dispatchCapability(%q) returned handled=false; system ports must always be handled", port)
		}
		if resp != nil {
			t.Errorf("dispatchCapability(%q) on legacy component returned resp=%v; want nil (no-op)", port, resp)
		}
	}

	if cmp.handleCalls.Load() != 0 {
		t.Errorf("legacy Handle was invoked %d times for system ports; want 0", cmp.handleCalls.Load())
	}
}

// TestDispatchCapability_NonSystemPortFallsThrough verifies that ports
// outside the system-port set always return handled=false, even if the
// component implements a capability interface. Non-system ports always
// route through Handle today.
func TestDispatchCapability_NonSystemPortFallsThrough(t *testing.T) {
	cmp := &settingsCapableMock{}
	r := createTestRunnerWithComponent(cmp)

	_, handled := r.dispatchCapability(context.Background(), "user-port", struct{}{})
	if handled {
		t.Errorf("dispatchCapability for non-system port returned handled=true")
	}
	if cmp.onSettingsCalls.Load() != 0 {
		t.Errorf("OnSettings was called for non-system port; should not have been")
	}
}

// TestSetState_StoresAndExposesBackend verifies the runner's state field
// can be set and retrieved via the public accessors.
func TestSetState_StoresAndExposesBackend(t *testing.T) {
	r := createTestRunnerWithComponent(&legacyMock{})
	want := &fakeRunnerState{}

	r.SetState(want)

	if got := r.GetState(); got != want {
		t.Errorf("GetState() = %v; want %v", got, want)
	}
}

// TestNotifyReconcile_NoopWithoutState verifies that calling NotifyReconcile
// before SetState (or with a state backend that doesn't implement
// snapshotRefresher) is safe.
func TestNotifyReconcile_NoopWithoutState(t *testing.T) {
	r := createTestRunnerWithComponent(&legacyMock{})
	// no SetState — state is nil
	r.NotifyReconcile(map[string]string{"a": "b"}) // must not panic
}

// TestNotifyReconcile_RefreshesSnapshotWhenSupported verifies that a state
// backend implementing snapshotRefresher receives the metadata snapshot
// on each reconcile.
func TestNotifyReconcile_RefreshesSnapshotWhenSupported(t *testing.T) {
	r := createTestRunnerWithComponent(&legacyMock{})
	state := &fakeRefresherState{}
	r.SetState(state)

	want := map[string]string{"k": "v"}
	r.NotifyReconcile(want)

	got := state.lastSnapshot.Load()
	if got == nil {
		t.Fatalf("UpdateSnapshot was not called")
	}
	gotMap, ok := got.(map[string]string)
	if !ok {
		t.Fatalf("snapshot stored type %T; want map[string]string", got)
	}
	if gotMap["k"] != "v" {
		t.Errorf("snapshot = %+v; want %+v", gotMap, want)
	}
}

// fakeRunnerState is a minimal m.State that records nothing — used to test
// SetState/GetState plumbing without exercising real backends.
type fakeRunnerState struct{}

func (*fakeRunnerState) Get(_ context.Context, _ string) ([]byte, bool, error) {
	return nil, false, nil
}
func (*fakeRunnerState) Set(_ context.Context, _ string, _ []byte) error { return nil }
func (*fakeRunnerState) Delete(_ context.Context, _ string) error        { return nil }

// fakeRefresherState additionally implements snapshotRefresher so tests can
// verify NotifyReconcile reaches the right method.
type fakeRefresherState struct {
	fakeRunnerState
	lastSnapshot atomic.Value
}

func (s *fakeRefresherState) UpdateSnapshot(metadata map[string]string) {
	// store a copy so callers can't mutate the recorded snapshot
	cp := make(map[string]string, len(metadata))
	for k, v := range metadata {
		cp[k] = v
	}
	s.lastSnapshot.Store(cp)
}
