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
