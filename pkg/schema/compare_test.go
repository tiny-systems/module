package schema

import (
	"testing"
)

func TestSchemasEqual(t *testing.T) {
	tests := []struct {
		name    string
		schema1 []byte
		schema2 []byte
		want    bool
	}{
		{
			name:    "both nil",
			schema1: nil,
			schema2: nil,
			want:    true,
		},
		{
			name:    "both empty",
			schema1: []byte{},
			schema2: []byte{},
			want:    true,
		},
		{
			name:    "one nil one empty",
			schema1: nil,
			schema2: []byte{},
			want:    true,
		},
		{
			name:    "one nil one with content",
			schema1: nil,
			schema2: []byte(`{"type":"object"}`),
			want:    false,
		},
		{
			name:    "identical schemas",
			schema1: []byte(`{"type":"object","properties":{"name":{"type":"string"}}}`),
			schema2: []byte(`{"type":"object","properties":{"name":{"type":"string"}}}`),
			want:    true,
		},
		{
			name:    "same content different whitespace",
			schema1: []byte(`{"type":"object"}`),
			schema2: []byte(`{ "type" : "object" }`),
			want:    true,
		},
		{
			name:    "different schemas",
			schema1: []byte(`{"type":"object"}`),
			schema2: []byte(`{"type":"string"}`),
			want:    false,
		},
		{
			name:    "same keys different order",
			schema1: []byte(`{"type":"object","title":"Test"}`),
			schema2: []byte(`{"title":"Test","type":"object"}`),
			want:    true,
		},
		{
			name:    "invalid JSON schema1",
			schema1: []byte(`{invalid`),
			schema2: []byte(`{"type":"object"}`),
			want:    false,
		},
		{
			name:    "invalid JSON schema2",
			schema1: []byte(`{"type":"object"}`),
			schema2: []byte(`{invalid`),
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SchemasEqual(tt.schema1, tt.schema2)
			if got != tt.want {
				t.Errorf("SchemasEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasCustomDefinitions(t *testing.T) {
	tests := []struct {
		name   string
		schema []byte
		want   bool
	}{
		{
			name:   "nil schema",
			schema: nil,
			want:   false,
		},
		{
			name:   "empty schema",
			schema: []byte{},
			want:   false,
		},
		{
			name:   "schema without defs",
			schema: []byte(`{"type":"object"}`),
			want:   false,
		},
		{
			name:   "schema with empty defs",
			schema: []byte(`{"type":"object","$defs":{}}`),
			want:   false,
		},
		{
			name:   "schema with non-configurable def",
			schema: []byte(`{"type":"object","$defs":{"TestDef":{"type":"string"}}}`),
			want:   false,
		},
		{
			name:   "schema with configurable:false def",
			schema: []byte(`{"type":"object","$defs":{"TestDef":{"type":"string","configurable":false}}}`),
			want:   false,
		},
		{
			name:   "schema with configurable:true def",
			schema: []byte(`{"type":"object","$defs":{"TestDef":{"type":"string","configurable":true}}}`),
			want:   true,
		},
		{
			name:   "schema with multiple defs one configurable",
			schema: []byte(`{"type":"object","$defs":{"Def1":{"type":"string"},"Def2":{"type":"number","configurable":true}}}`),
			want:   true,
		},
		{
			name:   "invalid JSON",
			schema: []byte(`{invalid`),
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasCustomDefinitions(tt.schema)
			if got != tt.want {
				t.Errorf("HasCustomDefinitions() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSchemasEqualIgnoringDefinitions(t *testing.T) {
	tests := []struct {
		name    string
		schema1 []byte
		schema2 []byte
		want    bool
	}{
		{
			name:    "both nil",
			schema1: nil,
			schema2: nil,
			want:    true,
		},
		{
			name:    "same schema no defs",
			schema1: []byte(`{"type":"object"}`),
			schema2: []byte(`{"type":"object"}`),
			want:    true,
		},
		{
			name:    "same schema different defs",
			schema1: []byte(`{"type":"object","$defs":{"A":{"type":"string"}}}`),
			schema2: []byte(`{"type":"object","$defs":{"B":{"type":"number"}}}`),
			want:    true,
		},
		{
			name:    "one with defs one without",
			schema1: []byte(`{"type":"object","$defs":{"A":{"type":"string"}}}`),
			schema2: []byte(`{"type":"object"}`),
			want:    true,
		},
		{
			name:    "different base schema",
			schema1: []byte(`{"type":"object","$defs":{"A":{"type":"string"}}}`),
			schema2: []byte(`{"type":"string","$defs":{"A":{"type":"string"}}}`),
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SchemasEqualIgnoringDefinitions(tt.schema1, tt.schema2)
			if got != tt.want {
				t.Errorf("SchemasEqualIgnoringDefinitions() = %v, want %v", got, tt.want)
			}
		})
	}
}
