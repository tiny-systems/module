package schema

import (
	"fmt"
	"github.com/google/go-cmp/cmp"
	"github.com/spyzhov/ajson"
	"github.com/swaggest/jsonschema-go"
	"testing"
)

type ModifyContext any
type Modify2Context any

type ModifyInMessage struct {
	Context  ModifyContext  `json:"context" configurable:"true" required:"true" title:"Context" description:"Arbitrary message to be modified"`
	Context2 Modify2Context `json:"context2"`
}

// TestSchemaConsistency test that we generate schema same way
func TestSchemaConsistency(t *testing.T) {
	ports := []interface{}{
		ModifyInMessage{},
		ModifyInMessage{},
		ModifyInMessage{},
		ModifyInMessage{},
		ModifyInMessage{},
		ModifyInMessage{},
		ModifyInMessage{},
		ModifyInMessage{},
		ModifyInMessage{},
		ModifyInMessage{},
		ModifyInMessage{},
		ModifyInMessage{},
		ModifyInMessage{},
		ModifyInMessage{},
		ModifyInMessage{},
	}

	var prevSchemaData []byte

	for i, port := range ports {
		schema, err := CreateSchema(port)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		schemaData, err := schema.MarshalJSON()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		i++
		if i <= 1 {
			// do not compare on a first loop
			prevSchemaData = schemaData
			continue
		}

		diff := cmp.Diff(string(schemaData), string(prevSchemaData))
		if diff != "" {
			t.Fatalf("%d schema not equal prev schema: %s", i, diff)
		}
		prevSchemaData = schemaData
	}
}

func TestCollectDefinitions(t *testing.T) {
	type args struct {
		s        []byte
		confDefs map[string]*ajson.Node
		filter   FilterFunction
	}
	tests := []struct {
		name     string
		args     args
		wantErr  bool
		checkMap func(map[string]*ajson.Node) error
	}{
		{
			name: "invalid empty json, should error",
			args: args{
				s:        []byte(``),
				confDefs: map[string]*ajson.Node{},
				filter:   FilterConfigurable,
			},
			wantErr: true,
		},
		{
			name: "invalid empty json, should error",
			args: args{
				s:        []byte(``),
				confDefs: map[string]*ajson.Node{},
				filter:   FilterConfigurable,
			},
			wantErr: true,
		},
		{
			name: "no $defs, should error",
			args: args{
				s:        []byte(`{}`),
				confDefs: map[string]*ajson.Node{},
				filter:   FilterConfigurable,
			},
			wantErr: true,
		},
		{
			name: "no configurable of shared defs, no error",
			args: args{
				s: []byte(`{
        "$defs": {
          "Test":{"type":"string"},
          "Test":{"type":"number"}
        }
        }`),
				confDefs: map[string]*ajson.Node{},
				filter:   FilterConfigurable,
			},
			wantErr: false,
			checkMap: func(m map[string]*ajson.Node) error {
				if len(m) > 0 {
					return fmt.Errorf("expected no configurable or shared defs")
				}
				return nil
			},
		},
		{
			name: "1 configurable and 1 shared def, no error",
			args: args{
				s: []byte(`{
        "$defs": {
          "Test":{"type":"string", "configurable": true},
          "Test2":{"type":"number", "shared": true}
        }
        }`),
				confDefs: map[string]*ajson.Node{},
				filter: func(node *ajson.Node) bool {
					if FilterConfigurable(node) || FilterShared(node) {
						return true
					}
					return false
				},
			},
			wantErr: false,
			checkMap: func(m map[string]*ajson.Node) error {
				if len(m) != 2 {
					return fmt.Errorf("expected 2 configurable/shared defs: found %d", len(m))
				}
				return nil
			},
		},

		{
			name: "check configurable definition type, no error",
			args: args{
				s: []byte(`{
        "$defs": {
          "Test":{"type":"specialtype", "configurable": true}
          }
        }`),
				confDefs: map[string]*ajson.Node{},
				filter:   FilterConfigurable,
			},
			wantErr: false,
			checkMap: func(m map[string]*ajson.Node) error {
				def, ok := m["Test"]
				if !ok {
					return fmt.Errorf("expected Test to exist")
				}
				if st, _ := GetStr("type", def); st != "specialtype" {
					return fmt.Errorf("expected type")
				}
				return nil
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CollectDefinitions(tt.args.s, tt.args.confDefs, tt.args.filter); (err != nil) != tt.wantErr {
				t.Errorf("CollectDefinitions() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.checkMap == nil {
				return
			}
			if err := tt.checkMap(tt.args.confDefs); err != nil {
				t.Errorf("checkMap error = %v", err)
			}
		})
	}
}

type routeName struct {
	Value   string   `json:"value"`
	Options []string `json:"options"`
}

func (r routeName) JSONSchema() (jsonschema.Schema, error) {
	name := jsonschema.Schema{}
	name.AddType(jsonschema.String)
	name.WithTitle("Route")
	name.WithDefault(r.Value)
	name.WithExtraPropertiesItem("shared", true)
	enums := make([]interface{}, len(r.Options))
	for k, v := range r.Options {
		enums[k] = v
	}
	name.WithEnum(enums...)
	return name, nil
}

func TestCreateSchema(t *testing.T) {
	// using different objects to generate their JSON schemas

	tests := []struct {
		name    string
		getObj  func() interface{}
		want    *jsonschema.Schema
		wantErr bool
	}{
		//
		{
			name: "nil object",
			getObj: func() interface{} {
				return nil
			},
			want: (&jsonschema.Schema{}),
		},
		//
		{
			name: "empty struct object",
			getObj: func() interface{} {
				return struct {
				}{}
			},
			want: (&jsonschema.Schema{}).WithType(jsonschema.Object.Type()).WithExtraPropertiesItem("$defs", map[string]jsonschema.Schema{}),
		},
		//
		{
			name: "check scalar tags presented as extra properties, any type props are definitions",
			getObj: func() interface{} {

				type ModifyContext any // no type
				type ModifyInMessage struct {
					Context ModifyContext `json:"context" configurable:"true" required:"true" title:"Context" description:"Arbitrary message to be modified"`
				}
				return ModifyInMessage{}
			},
			//
			want: (&jsonschema.Schema{}).
				WithRef("#/$defs/Modifyinmessage"). // on a root level we only have reference to the definition
				WithExtraPropertiesItem("$defs", map[string]jsonschema.Schema{
					///
					"Modifycontext": *((&jsonschema.Schema{}).
						WithTitle("Context").
						WithDescription("Arbitrary message to be modified")).
						WithExtraProperties(map[string]interface{}{
							"configurable": true,
							"path":         "$.context", // JSON path of this definition related to the root object
						}),
					///
					"Modifyinmessage": *((&jsonschema.Schema{}).WithType(jsonschema.Object.Type()).WithRequired("context").
						WithProperties(map[string]jsonschema.SchemaOrBool{
							"context": (&jsonschema.Schema{}).WithRef("#/$defs/Modifycontext").WithExtraProperties(map[string]interface{}{
								"propertyOrder": 1, // first property of it's level
							}).ToSchemaOrBool(),
						}).WithExtraPropertiesItem("path", "$")), // root level
					///
				}),
		},
		//
		{
			name: "configurable preserves structure",
			getObj: func() interface{} {

				type ModifyContext struct {
					Name string `json:"name"`
				} // no type

				type ModifyInMessage struct {
					Context ModifyContext `json:"context" configurable:"true" required:"true" title:"Context" description:"Arbitrary message to be modified"`
				}
				return ModifyInMessage{}
			},
			//
			want: (&jsonschema.Schema{}).
				WithRef("#/$defs/Modifyinmessage"). // on a root level we only have reference to the definition
				WithExtraPropertiesItem("$defs", map[string]jsonschema.Schema{
					///
					"Modifycontext": *(&jsonschema.Schema{}).
						WithType(jsonschema.Object.Type()).WithTitle("Context").WithDescription("Arbitrary message to be modified").
						WithProperties(map[string]jsonschema.SchemaOrBool{
							"name": (&jsonschema.Schema{}).WithType(jsonschema.String.Type()).WithExtraProperties(map[string]interface{}{
								"propertyOrder": 1, // first property of it's level
							}).ToSchemaOrBool(),
						}).WithExtraProperties(map[string]interface{}{
						"path":         "$.context", // JSON path of this definition related to the root object
						"configurable": true,
						// first property of it's level
					}),
					///
					"Modifyinmessage": *((&jsonschema.Schema{}).WithType(jsonschema.Object.Type()).WithRequired("context").
						WithProperties(map[string]jsonschema.SchemaOrBool{
							"context": (&jsonschema.Schema{}).WithRef("#/$defs/Modifycontext").WithExtraProperties(map[string]interface{}{
								"propertyOrder": 1, // first property of it's level
							}).ToSchemaOrBool(),
						}).WithExtraPropertiesItem("path", "$")), // root level
					///
				}),
		},
		//
		{
			name: "array tag enumTitles",
			getObj: func() interface{} {

				type Request struct {
					Method string `json:"method" title:"Method" enum:"get,post,patch,put,delete" enumTitles:"GET,POST,PATCH,PUT,DELETE"`
				}
				return Request{}
			},
			//
			want: (&jsonschema.Schema{}).
				WithRef("#/$defs/Request"). // on a root level we only have reference to the definition
				WithExtraPropertiesItem("$defs", map[string]jsonschema.Schema{
					///
					"Request": *((&jsonschema.Schema{}).WithType(jsonschema.Object.Type()).WithProperties(map[string]jsonschema.SchemaOrBool{
						"method": (&jsonschema.Schema{}).WithType(jsonschema.String.Type()).WithTitle("Method").WithEnum("get", "post", "patch", "put", "delete").WithExtraProperties(map[string]interface{}{
							"enumTitles":    []string{"GET", "POST", "PATCH", "PUT", "DELETE"},
							"propertyOrder": 1,
						}).ToSchemaOrBool(),
					}).
						WithExtraProperties(map[string]interface{}{
							"path": "$", // Request is root
						})),
				}),
		},

		{
			name: "root context any (used in signal)",
			getObj: func() interface{} {
				type Context any
				return new(Context)
			},
			//
			want: (&jsonschema.Schema{}).
				WithRef("#/$defs/Context"). // on a root level we only have reference to the definition
				WithExtraPropertiesItem("$defs", map[string]jsonschema.Schema{
					///
					"Context": *((&jsonschema.Schema{}).
						WithExtraProperties(map[string]interface{}{
							"path": "$", // Request is root
						})),
				}),
		},
		{
			name: "test schema exposer",
			getObj: func() interface{} {
				return routeName{
					Value:   "test",
					Options: []string{"test", "none", ""},
				}
			},
			//
			want: (&jsonschema.Schema{}).
				WithRef("#/$defs/Routename"). // on a root level we only have reference to the definition
				WithExtraPropertiesItem("$defs", map[string]jsonschema.Schema{
					///
					"Routename": *((&jsonschema.Schema{}).WithType(jsonschema.String.Type()). // string
															WithTitle("Route").
															WithDefault("test").
															WithEnum("test", "none", "").
															WithExtraProperties(map[string]interface{}{
							"propertyOrder": 1,
						}).
						WithExtraProperties(map[string]interface{}{
							"path":   "$", // Request is root
							"shared": true,
						})),
				}),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CreateSchema(tt.getObj())
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateSchema() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			g, _ := got.MarshalJSON()
			w, _ := tt.want.MarshalJSON()

			if d := cmp.Diff(string(g), string(w)); d != "" {
				t.Errorf("expected schema mismatch: %s; \nexpected:\n%s;\ngot:\n%s", d, string(w), string(g))
			}
		})
	}
}
