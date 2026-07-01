package module_test

import (
	"context"
	"testing"

	"github.com/tiny-systems/module/api/v1alpha1"
	"github.com/tiny-systems/module/module"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// fakeState is an inert State backend used to verify that Base wires through
// OnState correctly. It records calls but does not persist anything.
type fakeState struct{}

func (f *fakeState) Get(_ context.Context, _ string) ([]byte, bool, error) {
	return nil, false, nil
}
func (f *fakeState) Set(_ context.Context, _ string, _ []byte) error           { return nil }
func (f *fakeState) Delete(_ context.Context, _ string) error                  { return nil }
func (f *fakeState) List(_ context.Context, _ string) ([]string, error)        { return nil, nil }
func (f *fakeState) Scoped(_, _ string) module.State                           { return f }

// fakeK8sClient lets us call OnClient with a non-nil value.
type fakeK8sClient struct{}

func (fakeK8sClient) GetK8sClient() client.WithWatch { return nil }
func (fakeK8sClient) GetNamespace() string           { return "default" }

func TestBase_AccessorsZeroBeforeInjection(t *testing.T) {
	var b module.Base
	if b.State() != nil {
		t.Errorf("State() before OnState should be nil; got %v", b.State())
	}
	if b.Client() != nil {
		t.Errorf("Client() before OnClient should be nil; got %v", b.Client())
	}
	if (b.Identity() != v1alpha1.NodeIdentity{}) {
		t.Errorf("Identity() before OnIdentity should be zero; got %+v", b.Identity())
	}
}

func TestBase_OnStateStoresBackend(t *testing.T) {
	var b module.Base
	want := &fakeState{}
	b.OnState(want)
	if got := b.State(); got != want {
		t.Errorf("State() = %v, want %v", got, want)
	}
}

func TestBase_OnIdentityStoresIdentity(t *testing.T) {
	var b module.Base
	id := v1alpha1.NodeIdentity{
		NodeName:    "node-a",
		Namespace:   "ns-1",
		FlowName:    "flow-x",
		ProjectName: "proj-y",
	}
	b.OnIdentity(id)
	if got := b.Identity(); got != id {
		t.Errorf("Identity() = %+v, want %+v", got, id)
	}
}

func TestBase_OnClientStoresClient(t *testing.T) {
	var b module.Base
	want := fakeK8sClient{}
	b.OnClient(want)
	if got := b.Client(); got != want {
		t.Errorf("Client() = %v, want %v", got, want)
	}
}

// TestBase_SatisfiesCapabilityInterfaces is a compile-time check that Base
// satisfies the capability interfaces it advertises. The runtime asserts in
// Base's source already cover this; this test exists so a regression shows
// up as a test failure, not just a build error in a downstream package.
func TestBase_SatisfiesCapabilityInterfaces(t *testing.T) {
	var b module.Base
	var (
		_ module.Stateful      = &b
		_ module.IdentityAware = &b
		_ module.ClientAware   = &b
	)
}
