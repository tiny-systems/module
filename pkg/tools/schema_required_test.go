package tools

import (
	"reflect"
	"sort"
	"testing"
)

func TestConfigurableFieldsIn(t *testing.T) {
	cases := []struct {
		name        string
		schemaJSON  string
		wantFields  []string
	}{
		{
			name:        "no $defs",
			schemaJSON:  `{"type":"object"}`,
			wantFields:  []string{},
		},
		{
			name: "single configurable def, capitalised",
			schemaJSON: `{"$defs":{"Context":{"configurable":true,"type":"object","properties":{"token":{"type":"string"}}}},"$ref":"#/$defs/Settings"}`,
			wantFields: []string{"context"},
		},
		{
			name: "multiple configurable defs, alpha sorted",
			schemaJSON: `{"$defs":{
				"Context":{"configurable":true,"type":"object"},
				"OutputData":{"configurable":true,"type":"object"},
				"InputData":{"configurable":true,"type":"object"},
				"Plain":{"type":"object"}
			}}`,
			wantFields: []string{"context", "inputData", "outputData"},
		},
		{
			name: "non-configurable def ignored",
			schemaJSON: `{"$defs":{"Settings":{"type":"object","properties":{"delay":{"type":"integer"}}}}}`,
			wantFields: []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := configurableFieldsIn([]byte(tc.schemaJSON))
			sort.Strings(got)
			sort.Strings(tc.wantFields)
			if !reflect.DeepEqual(got, tc.wantFields) {
				t.Fatalf("got %v, want %v", got, tc.wantFields)
			}
		})
	}
}

func TestRequireSchemaForData(t *testing.T) {
	cases := []struct {
		name        string
		data        map[string]interface{}
		userSchema  map[string]interface{}
		configurable []string
		wantMissing []string
	}{
		{
			name:         "data fills configurable field, no schema",
			data:         map[string]interface{}{"context": map[string]interface{}{"x": 1}},
			userSchema:   nil,
			configurable: []string{"context"},
			wantMissing:  []string{"context"},
		},
		{
			name:         "schema present for configurable field",
			data:         map[string]interface{}{"context": map[string]interface{}{"x": 1}},
			userSchema:   map[string]interface{}{"context": map[string]interface{}{"type": "object"}},
			configurable: []string{"context"},
			wantMissing:  nil,
		},
		{
			name:         "data only fills non-configurable field",
			data:         map[string]interface{}{"delay": 1000},
			userSchema:   nil,
			configurable: []string{"context"},
			wantMissing:  nil,
		},
		{
			name:         "multiple configurable, only one missing",
			data:         map[string]interface{}{"context": map[string]interface{}{}, "outputData": map[string]interface{}{}},
			userSchema:   map[string]interface{}{"context": map[string]interface{}{}},
			configurable: []string{"context", "outputData"},
			wantMissing:  []string{"outputData"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := requireSchemaForData(tc.data, tc.userSchema, tc.configurable)
			if !reflect.DeepEqual(got, tc.wantMissing) {
				t.Fatalf("got %v, want %v", got, tc.wantMissing)
			}
		})
	}
}
