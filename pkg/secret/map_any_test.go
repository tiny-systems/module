package secret

import (
	"context"
	"testing"

	"github.com/tiny-systems/module/module"
)

func TestResolveMapStringAny(t *testing.T) {
	type Settings struct {
		Context any
	}
	s := &Settings{Context: map[string]any{
		"apiKey":    "{{secret:demo-keys/anthropic}}",
		"plainText": "hello",
		"nested": map[string]any{
			"deep": "{{secret:demo-keys/anthropic}}",
		},
	}}
	var c module.K8sClient = newFake(t, mkSecret("demo-keys", map[string]string{"anthropic": "sk-ant-real"}))
	if err := Resolve(context.Background(), s, c); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	m := s.Context.(map[string]any)
	if m["apiKey"] != "sk-ant-real" {
		t.Errorf("apiKey = %v, want sk-ant-real", m["apiKey"])
	}
	if m["plainText"] != "hello" {
		t.Errorf("plainText changed: %v", m["plainText"])
	}
	nested := m["nested"].(map[string]any)
	if nested["deep"] != "sk-ant-real" {
		t.Errorf("nested.deep = %v, want sk-ant-real", nested["deep"])
	}
}
