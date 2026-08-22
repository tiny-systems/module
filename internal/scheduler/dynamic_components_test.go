package scheduler

import (
	"context"
	"testing"

	"github.com/tiny-systems/module/internal/scheduler/runner"
	"github.com/tiny-systems/module/module"
)

// A component whose definition is data can appear and disappear while the
// module runs — that is the whole difference from a compiled one, which exists
// for the life of the binary.
type scriptComponent struct{ name string }

func (s *scriptComponent) GetInfo() module.ComponentInfo {
	return module.ComponentInfo{Name: s.name, Description: "script-defined"}
}
func (s *scriptComponent) Handle(context.Context, module.Handler, string, any) module.Result {
	return module.Result{}
}
func (s *scriptComponent) Ports() []module.Port       { return nil }
func (s *scriptComponent) Instance() module.Component { return &scriptComponent{name: s.name} }

func TestAComponentCanBeInstalledWhileRunning(t *testing.T) {
	s := New(func(context.Context, *runner.Msg) (any, error) { return nil, nil })

	if _, ok := s.InstalledComponent("summarise_pods"); ok {
		t.Fatal("component existed before it was defined")
	}
	if err := s.Install(&scriptComponent{name: "summarise_pods"}); err != nil {
		t.Fatalf("install: %v", err)
	}
	got, ok := s.InstalledComponent("summarise_pods")
	if !ok {
		t.Fatal("component was installed but cannot be looked up")
	}
	if got.GetInfo().Name != "summarise_pods" {
		t.Errorf("looked up %q", got.GetInfo().Name)
	}
}

// Deleting a definition must actually withdraw it. Leaving it installed is the
// failure mode this whole area keeps producing: something that no longer exists
// still reporting as available.
func TestUninstallWithdrawsTheComponent(t *testing.T) {
	s := New(func(context.Context, *runner.Msg) (any, error) { return nil, nil })
	_ = s.Install(&scriptComponent{name: "gone"})

	if !s.Uninstall("gone") {
		t.Fatal("Uninstall reported nothing to remove")
	}
	if _, ok := s.InstalledComponent("gone"); ok {
		t.Error("component survived Uninstall")
	}
	// A second removal is a no-op, and says so — a caller distinguishing a real
	// withdrawal from a repeat needs that.
	if s.Uninstall("gone") {
		t.Error("removing an absent component reported success")
	}
	if s.Uninstall("") {
		t.Error("empty name reported success")
	}
}

// A compiled component must never be shadowed by a definition claiming its
// name: the definition is refused instead, so something that already works
// cannot be replaced by something a user typed.
func TestInstalledComponentLetsAModuleRefuseAShadow(t *testing.T) {
	s := New(func(context.Context, *runner.Msg) (any, error) { return nil, nil })
	_ = s.Install(&scriptComponent{name: "router"}) // stands in for a compiled one

	if _, taken := s.InstalledComponent("router"); !taken {
		t.Fatal("a name in use did not report as taken")
	}
	if _, taken := s.InstalledComponent("router_v2"); taken {
		t.Error("a free name reported as taken")
	}
}
