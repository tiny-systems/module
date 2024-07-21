package schema

import (
	"github.com/google/go-cmp/cmp"
	"github.com/swaggest/jsonschema-go"
	"github.com/tiny-systems/module/module"
	"testing"
)

type ModifyContext any

type ModifyInMessage struct {
	Context ModifyContext `json:"context" configurable:"true" required:"true" title:"Context" description:"Arbitrary message to be modified"`
}

type ModifyOutMessage struct {
	Context ModifyContext `json:"context"`
}

func TestSchemaConsistency(t *testing.T) {

	sharedConfigurableSchemaDefinitions := make(map[string]jsonschema.Schema)

	ports := []module.Port{
		{
			Configuration: ModifyInMessage{},
		},
		{
			Configuration: ModifyOutMessage{},
		},
	}

	for _, p := range ports {
		_, err := CreateSchema(p.Configuration)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	t.Logf("shared configurable defs size: %d", len(sharedConfigurableSchemaDefinitions))

	schema, err := CreateSchema(ports[0].Configuration)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	schemaData, err := schema.MarshalJSON()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	schema2, err := CreateSchema(ports[0].Configuration)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	schema2Data, err := schema2.MarshalJSON()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	t.Log(string(schemaData))

	diff := cmp.Diff(schemaData, schema2Data)
	if diff != "" {
		t.Errorf("not equal: %s", diff)
	}
}
