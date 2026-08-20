package utils

import (
	"testing"

	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
)

// Reading only the string variant dropped every other kind of attribute, and
// the loss was invisible: the value arrived as an empty string rather than as
// an error, so a reader saw a present-but-blank field.
//
// The case that made it matter: the runner marks a caught error with
// handled=true so a consumer can tell a recovery from a crash. Every consumer
// received "".
func TestAttributeValue_RendersEveryKind(t *testing.T) {
	cases := map[string]struct {
		value *commonv1.AnyValue
		want  string
	}{
		"string": {&commonv1.AnyValue{Value: &commonv1.AnyValue_StringValue{StringValue: "pod-1"}}, "pod-1"},
		"bool":   {&commonv1.AnyValue{Value: &commonv1.AnyValue_BoolValue{BoolValue: true}}, "true"},
		"int":    {&commonv1.AnyValue{Value: &commonv1.AnyValue_IntValue{IntValue: 42}}, "42"},
		"double": {&commonv1.AnyValue{Value: &commonv1.AnyValue_DoubleValue{DoubleValue: 1.5}}, "1.5"},
		"bytes":  {&commonv1.AnyValue{Value: &commonv1.AnyValue_BytesValue{BytesValue: []byte("raw")}}, "raw"},
	}
	for name, c := range cases {
		if got := attributeValue(c.value); got != c.want {
			t.Errorf("%s: got %q, want %q", name, got, c.want)
		}
	}
}

// The specific attribute this was found through.
func TestAttributeValue_HandledFlagIsReadable(t *testing.T) {
	got := attributeValue(&commonv1.AnyValue{Value: &commonv1.AnyValue_BoolValue{BoolValue: true}})
	if got != "true" {
		t.Fatalf("handled=true renders as %q — a consumer cannot tell a caught error from a crash", got)
	}
}

func TestAttributeValue_SurvivesNil(t *testing.T) {
	if got := attributeValue(nil); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
	if got := attributeValue(&commonv1.AnyValue{}); got != "" {
		t.Fatalf("got %q, want empty for a value with no variant set", got)
	}
}

// Arrays and key-value lists have no obvious flat rendering. Empty is honest;
// a guessed format would put something misleading in front of a reader.
func TestAttributeValue_LeavesCompositesEmpty(t *testing.T) {
	composite := &commonv1.AnyValue{Value: &commonv1.AnyValue_ArrayValue{
		ArrayValue: &commonv1.ArrayValue{Values: []*commonv1.AnyValue{
			{Value: &commonv1.AnyValue_StringValue{StringValue: "a"}},
		}},
	}}
	if got := attributeValue(composite); got != "" {
		t.Fatalf("got %q, want empty rather than a guessed rendering", got)
	}
}
