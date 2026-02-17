package schema

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/goccy/go-json"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/tiny-systems/ajson"
)

func TestUpdateWithDefinitions(t *testing.T) {
	type args struct {
		realSchema                  []byte
		configurableDefinitionNodes map[string]*ajson.Node
	}
	tests := []struct {
		name    string
		args    args
		want    []byte
		wantErr bool
	}{
		{
			name: "empty schema returns empty schema unchanged",
			args: args{
				realSchema:                  []byte(``),
				configurableDefinitionNodes: map[string]*ajson.Node{},
			},
			wantErr: false,
			want:    []byte(``),
		},
		{
			name: "schema with no $defs returns original schema unchanged",
			args: args{
				realSchema:                  []byte(`{}`),
				configurableDefinitionNodes: map[string]*ajson.Node{},
			},
			wantErr: false,
			want:    []byte(`{}`),
		},
		{
			name: "empty defs returns as it is, no error",
			args: args{
				realSchema:                  []byte(`{"$defs":{}}`),
				configurableDefinitionNodes: map[string]*ajson.Node{},
			},
			wantErr: false,
			want:    []byte(`{"$defs":{}}`),
		},
		{
			name: "updates SomeDefinition from map, keep non configurable false because original schema has not it defined, no error",
			args: args{
				// real schema has it as simple string type with no format
				realSchema: []byte(`{"$defs":{"SomeDefinition": {"type": "string"}}}`),
				configurableDefinitionNodes: map[string]*ajson.Node{
					"SomeDefinition": ajson.Must(ajson.Unmarshal([]byte(`{"type":"string", "format":"textarea"}`))),
				},
			},
			wantErr: false,
			// now we have explicitly configurable == false
			// we see format from defintions
			want: []byte(`{"$defs":{"SomeDefinition":{"configurable":false,"format":"textarea","type":"string"}}}`),
		},
		{
			name: "updates SomeDefinition from map, keep non configurable true because original schema has it that way, no error",
			args: args{
				// real schema has it as simple string type with no format
				realSchema: []byte(`{"$defs":{"SomeDefinition": {"type": "string","configurable":false}}}`),
				configurableDefinitionNodes: map[string]*ajson.Node{
					// ignore configurable flag from def
					"SomeDefinition": ajson.Must(ajson.Unmarshal([]byte(`{"type":"string", "format":"textarea", "configurable":true}`))),
				},
			},
			wantErr: false,
			// now we have explicitly configurable == false
			// we see format from definitions
			want: []byte(`{"$defs":{"SomeDefinition":{"configurable":false,"format":"textarea","type":"string"}}}`),
		},
		{
			name: "updates SomeDefinition from map, preserves configurable and path, no error",
			args: args{
				// real schema has it as simple string type with no format
				realSchema: []byte(`{"$defs":{"SomeDefinition": {"type": "string","configurable":true, "path": "random"}}}`),
				configurableDefinitionNodes: map[string]*ajson.Node{
					// ignore configurable flag from def
					"SomeDefinition": ajson.Must(ajson.Unmarshal([]byte(`{"type": "string", "format": "textarea", "configurable": false}`))),
				},
			},
			wantErr: false,
			// now we have explicitly configurable == false
			// we see format from definitions
			want: []byte(`{"$defs":{"SomeDefinition":{"configurable":true,"format":"textarea","path":"random","type":"string"}}}`),
		},
		{
			name: "updates SomeDefinition from map, preserves configurable and path, drops some param, no error",
			args: args{
				// real schema has it as simple string type with no format
				realSchema: []byte(`{"$defs":{"SomeDefinition": {"type": "string","configurable":true, "path": "random", "some": "param"}}}`),
				configurableDefinitionNodes: map[string]*ajson.Node{
					// ignore configurable flag from def
					"SomeDefinition": ajson.Must(ajson.Unmarshal([]byte(`{"type": "string", "format": "textarea", "configurable": false}`))),
				},
			},
			wantErr: false,
			// now we have explicitly configurable == false
			// we see format from definitions
			want: []byte(`{"$defs":{"SomeDefinition":{"configurable":true,"format":"textarea","path":"random","type":"string"}}}`),
		},
		{
			name: "updates nothing, no definition match, no error",
			args: args{
				// real schema has it as simple string type with no format
				realSchema: []byte(`{"$defs":{"SunDefinition": {"type": "string"}}}`),
				configurableDefinitionNodes: map[string]*ajson.Node{
					"SomeDefinition": ajson.Must(ajson.Unmarshal([]byte(`{"type": "string", "format": "textarea"}`))),
				},
			},
			wantErr: false,
			// now we have explicitly configurable == false
			// we see format from definitions
			want: []byte(`{"$defs":{"SunDefinition": {"type": "string"}}}`),
		},
		{
			name: "path-based fallback: Startcontext matched by Context via path $.context",
			args: args{
				// HTTP server edge schema has Startcontext with additionalProperties (bare configurable)
				realSchema: []byte(`{"$defs":{"Start":{"path":"$","properties":{"context":{"$ref":"#/$defs/Startcontext"}},"type":"object"},"Startcontext":{"additionalProperties":{"type":"string"},"configurable":true,"path":"$.context","title":"Context","type":"object"}},"$ref":"#/$defs/Start"}`),
				configurableDefinitionNodes: map[string]*ajson.Node{
					// Source node (config/ticker) has Context with explicit properties
					"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"auto_hostname":{"type":"boolean"},"host_name":{"type":"string"}},"title":"Context","type":"object"}`))),
				},
			},
			wantErr: false,
			// Startcontext should now have the properties from Context, matched by path
			want: []byte(`{"$defs":{"Start":{"path":"$","properties":{"context":{"$ref":"#/$defs/Startcontext"}},"type":"object"},"Startcontext":{"configurable":true,"path":"$.context","properties":{"auto_hostname":{"type":"boolean"},"host_name":{"type":"string"}},"title":"Context","type":"object"}},"$ref":"#/$defs/Start"}`),
		},
		{
			name: "path-based fallback: no path on real schema def, no fallback match",
			args: args{
				realSchema: []byte(`{"$defs":{"Foo":{"type":"string"}}}`),
				configurableDefinitionNodes: map[string]*ajson.Node{
					"Bar": ajson.Must(ajson.Unmarshal([]byte(`{"type":"string","path":"$.bar","format":"textarea"}`))),
				},
			},
			wantErr: false,
			// Foo has no path, Bar has different name — no match
			want: []byte(`{"$defs":{"Foo":{"type":"string"}}}`),
		},
		{
			name: "bare exact match replaced by richer path match: both Startcontext and Context in defs",
			args: args{
				// Edge schema has Startcontext (bare, from target node)
				realSchema: []byte(`{"$defs":{"Start":{"path":"$","properties":{"context":{"$ref":"#/$defs/Startcontext"}},"type":"object"},"Startcontext":{"additionalProperties":{"type":"string"},"configurable":true,"path":"$.context","title":"Context","type":"object"}},"$ref":"#/$defs/Start"}`),
				configurableDefinitionNodes: map[string]*ajson.Node{
					// From target node: bare Startcontext (exact key match but no properties)
					"Startcontext": ajson.Must(ajson.Unmarshal([]byte(`{"additionalProperties":{"type":"string"},"configurable":true,"path":"$.context","title":"Context","type":"object"}`))),
					// From source node: rich Context with explicit properties
					"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"auto_hostname":{"type":"boolean"},"host_name":{"type":"string"}},"title":"Context","type":"object"}`))),
				},
			},
			wantErr: false,
			// Startcontext exact match is bare, so path-based fallback should pick richer Context
			want: []byte(`{"$defs":{"Start":{"path":"$","properties":{"context":{"$ref":"#/$defs/Startcontext"}},"type":"object"},"Startcontext":{"configurable":true,"path":"$.context","properties":{"auto_hostname":{"type":"boolean"},"host_name":{"type":"string"}},"title":"Context","type":"object"}},"$ref":"#/$defs/Start"}`),
		},
		{
			name: "exact key match takes priority over path match",
			args: args{
				realSchema: []byte(`{"$defs":{"Context":{"configurable":true,"path":"$.context","type":"object","additionalProperties":{"type":"string"}}}}`),
				configurableDefinitionNodes: map[string]*ajson.Node{
					// exact name match
					"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"name":{"type":"string"}},"type":"object"}`))),
					// same path but different name — should NOT be used
					"Other": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"wrong":{"type":"integer"}},"type":"object"}`))),
				},
			},
			wantErr: false,
			// exact key match wins: Context properties from "Context" def, not "Other"
			want: []byte(`{"$defs":{"Context":{"configurable":true,"path":"$.context","properties":{"name":{"type":"string"}},"type":"object"}}}`),
		},
		{
			name: "required stripped from configurable overlay",
			args: args{
				// Target port has bare Context (configurable, no properties, no type)
				realSchema: []byte(`{"$defs":{"Request":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"}},"$ref":"#/$defs/Request"}`),
				configurableDefinitionNodes: map[string]*ajson.Node{
					// Source node's Context has required: ["endpoints"] and type: "object"
					"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"endpoints":{"type":"array","items":{"type":"object"}}},"required":["endpoints"],"title":"Context","type":"object"}`))),
				},
			},
			wantErr: false,
			// required stripped; type:"object" preserved because overlay has properties
			want: []byte(`{"$defs":{"Context":{"configurable":true,"path":"$.context","properties":{"endpoints":{"type":"array","items":{"type":"object"}}},"title":"Context","type":"object"},"Request":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"}},"$ref":"#/$defs/Request"}`),
		},
		{
			name: "type preserved when overlay has properties — UI renders form fields",
			args: args{
				// Target port (e.g. Debug's IN) has bare Context — no type
				realSchema: []byte(`{"$defs":{"In":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"data":{"$ref":"#/$defs/Inputdata"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"},"Inputdata":{"configurable":true,"path":"$.data","title":"Data"}},"$ref":"#/$defs/In"}`),
				configurableDefinitionNodes: map[string]*ajson.Node{
					// Source node's Context is type:"object" with properties
					"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"collection":{"type":"string"},"namespace":{"type":"string"}},"title":"Context","type":"object"}`))),
				},
			},
			wantErr: false,
			// type:"object" preserved because overlay has explicit properties
			want: []byte(`{"$defs":{"Context":{"configurable":true,"path":"$.context","properties":{"collection":{"type":"string"},"namespace":{"type":"string"}},"title":"Context","type":"object"},"In":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"data":{"$ref":"#/$defs/Inputdata"}},"type":"object"},"Inputdata":{"configurable":true,"path":"$.data","title":"Data"}},"$ref":"#/$defs/In"}`),
		},
		{
			name: "type preserved when original definition has type",
			args: args{
				// Target port has Context WITH explicit type:"object"
				realSchema: []byte(`{"$defs":{"Request":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context","type":"object"}},"$ref":"#/$defs/Request"}`),
				configurableDefinitionNodes: map[string]*ajson.Node{
					"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"name":{"type":"string"}},"title":"Context","type":"object"}`))),
				},
			},
			wantErr: false,
			// type:"object" preserved because original already had it
			want: []byte(`{"$defs":{"Context":{"configurable":true,"path":"$.context","properties":{"name":{"type":"string"}},"title":"Context","type":"object"},"Request":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"}},"$ref":"#/$defs/Request"}`),
		},
		{
			name: "required stripped even with path-based match",
			args: args{
				realSchema: []byte(`{"$defs":{"Start":{"path":"$","properties":{"context":{"$ref":"#/$defs/Startcontext"}},"type":"object"},"Startcontext":{"additionalProperties":{"type":"string"},"configurable":true,"path":"$.context","title":"Context","type":"object"}},"$ref":"#/$defs/Start"}`),
				configurableDefinitionNodes: map[string]*ajson.Node{
					"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"domain":{"type":"string"},"automatic_domain":{"type":"boolean"}},"required":["domain","automatic_domain"],"title":"Context","type":"object"}`))),
				},
			},
			wantErr: false,
			// Startcontext gets properties from Context via path match, but required is stripped
			want: []byte(`{"$defs":{"Start":{"path":"$","properties":{"context":{"$ref":"#/$defs/Startcontext"}},"type":"object"},"Startcontext":{"configurable":true,"path":"$.context","properties":{"domain":{"type":"string"},"automatic_domain":{"type":"boolean"}},"title":"Context","type":"object"}},"$ref":"#/$defs/Start"}`),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UpdateWithDefinitions(tt.args.realSchema, tt.args.configurableDefinitionNodes)
			if (err != nil) != tt.wantErr {
				t.Errorf("UpdateWithConfigurableDefinitions() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("UpdateWithConfigurableDefinitions() got = %s, want %s", got, tt.want)
			}
		})
	}
}

// TestUpdateWithDefinitions_ValidationIntegration tests that schemas produced by
// UpdateWithDefinitions actually validate data correctly — the full pipeline that
// the platform uses. This catches bugs where the overlay introduces constraints
// (type, required, etc.) that cause false validation errors.
func TestUpdateWithDefinitions_ValidationIntegration(t *testing.T) {
	tests := []struct {
		name string
		// Schema and overlay inputs
		targetPortSchema            string // native target port schema from Status
		configurableDefinitionNodes map[string]*ajson.Node
		// Data to validate against the overlaid schema
		dataToValidate string
		wantValid      bool
		errContains    string // substring expected in error message (if wantValid=false)
	}{
		// ============================================================
		// Bare Context target (type Context any) — accepts any type
		// ============================================================
		// Overlay WITH properties → type:"object" preserved for UI form rendering
		{
			name: "rich overlay: object value passes",
			targetPortSchema: `{"$defs":{"In":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"}},"$ref":"#/$defs/In"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"name":{"type":"string"}},"title":"Context","type":"object"}`))),
			},
			dataToValidate: `{"context":{"name":"Alice"}}`,
			wantValid:      true,
		},
		{
			name: "rich overlay: string value rejected — type:object preserved",
			targetPortSchema: `{"$defs":{"In":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"data":{"$ref":"#/$defs/Inputdata"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"},"Inputdata":{"configurable":true,"path":"$.data","title":"Data"}},"$ref":"#/$defs/In"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"collection":{"type":"string"},"namespace":{"type":"string"}},"title":"Context","type":"object"}`))),
			},
			dataToValidate: `{"context":"some error string","data":null}`,
			wantValid:      false,
			errContains:    "expected object",
		},
		{
			name: "rich overlay: number value rejected",
			targetPortSchema: `{"$defs":{"In":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"}},"$ref":"#/$defs/In"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"count":{"type":"integer"}},"title":"Context","type":"object"}`))),
			},
			dataToValidate: `{"context":42}`,
			wantValid:      false,
			errContains:    "expected object",
		},
		// Bare overlay (no properties but type:"object") — type preserved
		{
			name: "bare overlay with type: string value rejected",
			targetPortSchema: `{"$defs":{"In":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"}},"$ref":"#/$defs/In"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","title":"Context","type":"object"}`))),
			},
			dataToValidate: `{"context":"any string"}`,
			wantValid:      false,
			errContains:    "expected object",
		},
		{
			name: "bare overlay with type: object value passes",
			targetPortSchema: `{"$defs":{"In":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"}},"$ref":"#/$defs/In"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","title":"Context","type":"object"}`))),
			},
			dataToValidate: `{"context":{}}`,
			wantValid:      true,
		},
		// ============================================================
		// Required stripping — source's required doesn't apply to target
		// ============================================================
		{
			name: "required stripped: object without source-required fields passes (status page)",
			targetPortSchema: `{"$defs":{"Request":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"url":{"type":"string"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"}},"$ref":"#/$defs/Request"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{
				// Ticker's Context with required: ["endpoints"]
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"endpoints":{"type":"array","items":{"type":"object"}}},"required":["endpoints"],"title":"Context","type":"object"}`))),
			},
			// Edge maps a single endpoint item (no "endpoints" array), should pass
			dataToValidate: `{"context":{"url":"https://example.com","method":"GET"},"url":"https://example.com"}`,
			wantValid:      true,
		},
		{
			name: "required stripped: empty context object passes",
			targetPortSchema: `{"$defs":{"Request":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"}},"$ref":"#/$defs/Request"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"a":{"type":"string"},"b":{"type":"string"}},"required":["a","b"],"title":"Context","type":"object"}`))),
			},
			dataToValidate: `{"context":{}}`,
			wantValid:      true,
		},
		// ============================================================
		// Typed target — type preserved, validation still works
		// ============================================================
		{
			name: "typed context: object value passes when original has type",
			targetPortSchema: `{"$defs":{"Request":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context","type":"object"}},"$ref":"#/$defs/Request"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"key":{"type":"string"}},"title":"Context","type":"object"}`))),
			},
			dataToValidate: `{"context":{"key":"value"}}`,
			wantValid:      true,
		},
		{
			name: "typed context: string value fails when original has type object",
			targetPortSchema: `{"$defs":{"Request":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context","type":"object"}},"$ref":"#/$defs/Request"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"key":{"type":"string"}},"title":"Context","type":"object"}`))),
			},
			dataToValidate: `{"context":"not an object"}`,
			wantValid:      false,
			errContains:    "expected object",
		},
		// ============================================================
		// Path-based matching — Startcontext matched by Context
		// ============================================================
		{
			name: "path-matched overlay: string into bare Startcontext passes",
			targetPortSchema: `{"$defs":{"Start":{"path":"$","properties":{"context":{"$ref":"#/$defs/Startcontext"}},"type":"object"},"Startcontext":{"additionalProperties":{"type":"string"},"configurable":true,"path":"$.context","title":"Context"}},"$ref":"#/$defs/Start"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"host":{"type":"string"}},"title":"Context","type":"object"}`))),
			},
			// Startcontext has type:"object" in the original — overlay preserves it
			// But wait: the original Startcontext already has "type":"object" explicitly
			// However, path match means we pick the richer Context overlay
			// The original has type:"object", so the overlay type is preserved
			dataToValidate: `{"context":{"host":"example.com"}}`,
			wantValid:      true,
		},
		// ============================================================
		// No overlay — schema unchanged, validation still works
		// ============================================================
		{
			name: "no matching overlay: schema unchanged, valid data passes",
			targetPortSchema: `{"$defs":{"Request":{"path":"$","properties":{"name":{"type":"string"}},"type":"object"}},"$ref":"#/$defs/Request"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{
				"Unrelated": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.other","type":"string"}`))),
			},
			dataToValidate: `{"name":"test"}`,
			wantValid:      true,
		},
		{
			name: "no matching overlay: schema unchanged, invalid data fails",
			targetPortSchema: `{"$defs":{"Request":{"path":"$","properties":{"count":{"type":"integer"}},"required":["count"],"type":"object"}},"$ref":"#/$defs/Request"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{
				"Unrelated": ajson.Must(ajson.Unmarshal([]byte(`{"type":"string"}`))),
			},
			dataToValidate: `{}`,
			wantValid:      false,
			errContains:    "missing properties",
		},
		// ============================================================
		// Multiple configurable defs — mixed bare and typed
		// ============================================================
		{
			name: "multiple defs: rich overlay Context rejects string, typed Data keeps type",
			targetPortSchema: `{"$defs":{"In":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"data":{"$ref":"#/$defs/Data"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"},"Data":{"configurable":true,"path":"$.data","title":"Data","type":"object"}},"$ref":"#/$defs/In"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"x":{"type":"string"}},"title":"Context","type":"object"}`))),
				"Data":    ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.data","properties":{"y":{"type":"integer"}},"title":"Data","type":"object"}`))),
			},
			// Both overlays have properties → type:"object" preserved on both
			dataToValidate: `{"context":"just a string","data":{"y":42}}`,
			wantValid:      false,
			errContains:    "expected object",
		},
		{
			name: "multiple defs: both objects pass",
			targetPortSchema: `{"$defs":{"In":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"data":{"$ref":"#/$defs/Data"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"},"Data":{"configurable":true,"path":"$.data","title":"Data","type":"object"}},"$ref":"#/$defs/In"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"x":{"type":"string"}},"title":"Context","type":"object"}`))),
				"Data":    ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.data","properties":{"y":{"type":"integer"}},"title":"Data","type":"object"}`))),
			},
			dataToValidate: `{"context":{"x":"hello"},"data":{"y":42}}`,
			wantValid:      true,
		},
		{
			name: "multiple defs: typed Data rejects string value",
			targetPortSchema: `{"$defs":{"In":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"data":{"$ref":"#/$defs/Data"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"},"Data":{"configurable":true,"path":"$.data","title":"Data","type":"object"}},"$ref":"#/$defs/In"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"x":{"type":"string"}},"title":"Context","type":"object"}`))),
				"Data":    ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.data","properties":{"y":{"type":"integer"}},"title":"Data","type":"object"}`))),
			},
			dataToValidate: `{"context":"ok string","data":"not an object"}`,
			wantValid:      false,
			errContains:    "expected object",
		},
		// ============================================================
		// Edge cases: overlay with enum, minProperties, additionalProperties:false
		// ============================================================
		{
			name: "rich overlay: additionalProperties false rejects string — type preserved",
			targetPortSchema: `{"$defs":{"In":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"}},"$ref":"#/$defs/In"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"x":{"type":"string"}},"additionalProperties":false,"title":"Context","type":"object"}`))),
			},
			// overlay has properties → type:"object" preserved → string rejected
			dataToValidate: `{"context":"a string value"}`,
			wantValid:      false,
			errContains:    "expected object",
		},
		{
			name: "rich overlay: enum on property rejects non-object — type preserved",
			targetPortSchema: `{"$defs":{"In":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"}},"$ref":"#/$defs/In"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"status":{"type":"string","enum":["active","inactive"]}},"title":"Context","type":"object"}`))),
			},
			dataToValidate: `{"context":12345}`,
			wantValid:      false,
			errContains:    "expected object",
		},
		// ============================================================
		// Real-world scenario: ticker → split → http_request chain
		// ============================================================
		{
			name: "status page: single endpoint item passes http_request Context (required stripped, type preserved)",
			targetPortSchema: `{"$defs":{"Request":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"},"url":{"type":"string"},"method":{"type":"string"},"contentType":{"type":"string"},"timeout":{"type":"integer"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"}},"$ref":"#/$defs/Request"}`,
			configurableDefinitionNodes: map[string]*ajson.Node{
				// From ticker's settings: Context has endpoints array with required
				"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"endpoints":{"type":"array","items":{"type":"object","properties":{"url":{"type":"string"},"method":{"type":"string"},"contentType":{"type":"string"}}}}},"required":["endpoints"],"title":"Context","type":"object"}`))),
			},
			// After split, $.item is a single endpoint (not the full context with endpoints array)
			dataToValidate: `{"context":{"url":"https://api.example.com/health","method":"GET","contentType":"application/json"},"url":"https://api.example.com/health","method":"GET","contentType":"application/json","timeout":10}`,
			wantValid:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Step 1: Apply overlay
			overlaidSchema, err := UpdateWithDefinitions([]byte(tt.targetPortSchema), tt.configurableDefinitionNodes)
			if err != nil {
				t.Fatalf("UpdateWithDefinitions() error: %v", err)
			}

			// Step 2: Compile and validate
			compiler := jsonschema.NewCompiler()
			compiler.Draft = jsonschema.Draft7
			if err := compiler.AddResource("schema.json", bytes.NewReader(overlaidSchema)); err != nil {
				t.Fatalf("AddResource() error: %v (schema: %s)", err, overlaidSchema)
			}
			sch, err := compiler.Compile("schema.json")
			if err != nil {
				t.Fatalf("Compile() error: %v (schema: %s)", err, overlaidSchema)
			}

			// Parse data to validate
			var data interface{}
			if err := json.Unmarshal([]byte(tt.dataToValidate), &data); err != nil {
				t.Fatalf("data unmarshal error: %v", err)
			}

			// Validate
			validationErr := sch.Validate(data)

			if tt.wantValid && validationErr != nil {
				t.Errorf("expected valid but got error: %v\n  schema: %s\n  data: %s", validationErr, overlaidSchema, tt.dataToValidate)
			}
			if !tt.wantValid && validationErr == nil {
				t.Errorf("expected validation error but got nil\n  schema: %s\n  data: %s", overlaidSchema, tt.dataToValidate)
			}
			if !tt.wantValid && validationErr != nil && tt.errContains != "" {
				if !strings.Contains(validationErr.Error(), tt.errContains) {
					t.Errorf("error %q does not contain %q", validationErr.Error(), tt.errContains)
				}
			}
		})
	}
}
