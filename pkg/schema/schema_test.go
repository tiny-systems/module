package schema

import (
	"github.com/tiny-systems/ajson"
	"reflect"
	"testing"
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
				// Target port has bare Context (configurable, no properties)
				realSchema: []byte(`{"$defs":{"Request":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"},"Context":{"configurable":true,"path":"$.context","title":"Context"}},"$ref":"#/$defs/Request"}`),
				configurableDefinitionNodes: map[string]*ajson.Node{
					// Source node's Context has required: ["endpoints"] from ticker settings
					"Context": ajson.Must(ajson.Unmarshal([]byte(`{"configurable":true,"path":"$.context","properties":{"endpoints":{"type":"array","items":{"type":"object"}}},"required":["endpoints"],"title":"Context","type":"object"}`))),
				},
			},
			wantErr: false,
			// Context should have properties from overlay BUT required must be stripped
			want: []byte(`{"$defs":{"Context":{"configurable":true,"path":"$.context","properties":{"endpoints":{"type":"array","items":{"type":"object"}}},"title":"Context","type":"object"},"Request":{"path":"$","properties":{"context":{"$ref":"#/$defs/Context"}},"type":"object"}},"$ref":"#/$defs/Request"}`),
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
