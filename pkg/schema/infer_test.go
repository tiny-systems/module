package schema

import (
	"reflect"
	"testing"
)

func TestInferFromInstance(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  map[string]interface{}
	}{
		{
			name:  "nil → empty schema",
			input: nil,
			want:  map[string]interface{}{},
		},
		{
			name:  "bool",
			input: true,
			want:  map[string]interface{}{"type": "boolean"},
		},
		{
			name:  "int",
			input: 42,
			want:  map[string]interface{}{"type": "integer"},
		},
		{
			name:  "int64",
			input: int64(42),
			want:  map[string]interface{}{"type": "integer"},
		},
		{
			name:  "float64 whole number → integer",
			input: float64(42),
			want:  map[string]interface{}{"type": "integer"},
		},
		{
			name:  "float64 fractional → number",
			input: 3.14,
			want:  map[string]interface{}{"type": "number"},
		},
		{
			name:  "string",
			input: "hello",
			want:  map[string]interface{}{"type": "string"},
		},
		{
			name:  "template string treated as string",
			input: "{{$.context.token}}",
			want:  map[string]interface{}{"type": "string"},
		},
		{
			name:  "empty object",
			input: map[string]interface{}{},
			want: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			name: "flat object with mixed primitives",
			input: map[string]interface{}{
				"token":   "secret",
				"port":    8080,
				"enabled": true,
			},
			want: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"token":   map[string]interface{}{"type": "string"},
					"port":    map[string]interface{}{"type": "integer"},
					"enabled": map[string]interface{}{"type": "boolean"},
				},
			},
		},
		{
			name: "nested object",
			input: map[string]interface{}{
				"outer": map[string]interface{}{
					"inner": "value",
				},
			},
			want: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"outer": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"inner": map[string]interface{}{"type": "string"},
						},
					},
				},
			},
		},
		{
			name:  "empty array → type:array, no items",
			input: []interface{}{},
			want:  map[string]interface{}{"type": "array"},
		},
		{
			name:  "array of strings",
			input: []interface{}{"a", "b", "c"},
			want: map[string]interface{}{
				"type":  "array",
				"items": map[string]interface{}{"type": "string"},
			},
		},
		{
			name: "array of objects",
			input: []interface{}{
				map[string]interface{}{"id": 1},
				map[string]interface{}{"id": 2},
			},
			want: map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]interface{}{"type": "integer"},
					},
				},
			},
		},
		{
			name: "object with template values",
			input: map[string]interface{}{
				"url":    "{{$.context.url}}",
				"method": "GET",
			},
			want: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url":    map[string]interface{}{"type": "string"},
					"method": map[string]interface{}{"type": "string"},
				},
			},
		},
		{
			name:  "unknown type → empty schema, not a panic",
			input: struct{ X int }{X: 1},
			want:  map[string]interface{}{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := InferFromInstance(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("InferFromInstance(%#v) =\n  %#v\nwant\n  %#v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsTemplate(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"plain string", false},
		{"{{$.field}}", true},
		{"prefix {{$.field}} suffix", true},
		{"{{}}", true},
		{"{ not a template }", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.s, func(t *testing.T) {
			if got := IsTemplate(tt.s); got != tt.want {
				t.Errorf("IsTemplate(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}
