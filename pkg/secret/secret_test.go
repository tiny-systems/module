package secret

import (
	"context"
	"testing"

	"github.com/tiny-systems/module/module"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// fakeClient implements module.K8sClient backed by a controller-
// runtime fake client. The fake client supports Get/List/Watch on
// arbitrary resources without a real apiserver — enough to exercise
// the Secret fetch path.
type fakeClient struct {
	c  client.WithWatch
	ns string
}

func (f *fakeClient) GetK8sClient() client.WithWatch { return f.c }
func (f *fakeClient) GetNamespace() string           { return f.ns }

func newFake(t *testing.T, secrets ...*corev1.Secret) *fakeClient {
	t.Helper()
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	objs := make([]client.Object, 0, len(secrets))
	for _, s := range secrets {
		objs = append(objs, s)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	return &fakeClient{c: c, ns: "default"}
}

func mkSecret(name string, data map[string]string) *corev1.Secret {
	d := make(map[string][]byte, len(data))
	for k, v := range data {
		d[k] = []byte(v)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
		Data:       d,
	}
}

func TestResolveStringField(t *testing.T) {
	type Settings struct {
		APIKey string
		Other  string
	}
	s := &Settings{APIKey: "{{secret:anthropic/key}}", Other: "untouched"}
	c := newFake(t, mkSecret("anthropic", map[string]string{"key": "sk-ant-xyz"}))
	if err := Resolve(context.Background(), s, c); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if s.APIKey != "sk-ant-xyz" {
		t.Errorf("APIKey = %q, want sk-ant-xyz", s.APIKey)
	}
	if s.Other != "untouched" {
		t.Errorf("Other = %q, want untouched", s.Other)
	}
}

func TestResolveNested(t *testing.T) {
	type Context struct {
		APIKey string
	}
	type Settings struct {
		Context Context
		Label   string
	}
	s := &Settings{Context: Context{APIKey: "{{secret:openai/api_key}}"}, Label: "prod"}
	c := newFake(t, mkSecret("openai", map[string]string{"api_key": "sk-xxx"}))
	if err := Resolve(context.Background(), s, c); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if s.Context.APIKey != "sk-xxx" {
		t.Errorf("nested APIKey = %q, want sk-xxx", s.Context.APIKey)
	}
}

func TestResolveMissingSecret(t *testing.T) {
	type Settings struct{ APIKey string }
	s := &Settings{APIKey: "{{secret:missing/key}}"}
	c := newFake(t)
	if err := Resolve(context.Background(), s, c); err == nil {
		t.Fatal("expected error for missing Secret")
	}
}

func TestResolveMissingKey(t *testing.T) {
	type Settings struct{ APIKey string }
	s := &Settings{APIKey: "{{secret:anthropic/wrongkey}}"}
	c := newFake(t, mkSecret("anthropic", map[string]string{"key": "sk-ant-xyz"}))
	err := Resolve(context.Background(), s, c)
	if err == nil {
		t.Fatal("expected error for missing key")
	}
}

func TestResolveMalformedPlaceholderIgnored(t *testing.T) {
	type Settings struct{ APIKey string }
	s := &Settings{APIKey: "{{secret:bad}}"}
	c := newFake(t)
	if err := Resolve(context.Background(), s, c); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if s.APIKey != "{{secret:bad}}" {
		t.Errorf("APIKey = %q, want unchanged", s.APIKey)
	}
}

func TestResolveRequiresPointer(t *testing.T) {
	type Settings struct{ APIKey string }
	s := Settings{APIKey: "{{secret:anthropic/key}}"}
	c := newFake(t)
	if err := Resolve(context.Background(), s, c); err == nil {
		t.Fatal("expected error for non-pointer settings")
	}
}

func TestResolveNilClient(t *testing.T) {
	type Settings struct{ APIKey string }
	s := &Settings{APIKey: "{{secret:anthropic/key}}"}
	var c module.K8sClient
	if err := Resolve(context.Background(), s, c); err == nil {
		t.Fatal("expected error for nil client")
	}
}

func TestResolveMap(t *testing.T) {
	type Settings struct {
		Env map[string]string
	}
	s := &Settings{Env: map[string]string{
		"TOKEN":   "{{secret:slack/token}}",
		"OTHER":   "plain",
		"INVALID": "{{secret:foo}}",
	}}
	c := newFake(t, mkSecret("slack", map[string]string{"token": "xoxb-secret"}))
	if err := Resolve(context.Background(), s, c); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if s.Env["TOKEN"] != "xoxb-secret" {
		t.Errorf("Env[TOKEN] = %q, want xoxb-secret", s.Env["TOKEN"])
	}
	if s.Env["OTHER"] != "plain" {
		t.Errorf("Env[OTHER] = %q, want plain", s.Env["OTHER"])
	}
	if s.Env["INVALID"] != "{{secret:foo}}" {
		t.Errorf("Env[INVALID] = %q, want unchanged", s.Env["INVALID"])
	}
}
