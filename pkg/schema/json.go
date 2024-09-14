package schema

import (
	"fmt"
	"github.com/goccy/go-json"
	"github.com/pkg/errors"
	"github.com/spyzhov/ajson"
	"github.com/swaggest/jsonschema-go"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"reflect"
	"strconv"
	"strings"
)

type tagDefinition struct {
	Path       string
	Definition string
}

var scalarCustomProps = []string{
	"requiredWhen", "propertyOrder", "optionalWhen", "colSpan", "tab", "align", "configurable", "shared", "$ref", "type", "readonly", "format",
}

var arrayCustomProps = []string{
	"enumTitles",
}

func GetDefinitionName(t reflect.Type) string {
	return cases.Title(language.English).String(t.Name())
}

//CollectDefinitions finds all shared and configurable definitions

func CollectDefinitions(s []byte, confDefs map[string]*ajson.Node) error {
	realSchemaNode, err := ajson.Unmarshal(s)
	if err != nil {
		return errors.Wrap(err, "error reading original schema")
	}
	realSchemaNodeDefs, err := realSchemaNode.GetKey("$defs")
	if err != nil {
		return err
	}
	for _, defName := range realSchemaNodeDefs.Keys() {
		def, err := realSchemaNodeDefs.GetKey(defName)
		if err != nil || def == nil {
			continue
		}
		configurable, _ := getBool("configurable", def)
		shared, _ := getBool("shared", def)

		if !configurable && !shared {
			continue
		}
		confDefs[defName] = def
	}
	return nil
}

func CreateSchema(m interface{}) (jsonschema.Schema, error) {

	var (
		r = jsonschema.Reflector{
			DefaultOptions: make([]func(ctx *jsonschema.ReflectContext), 0),
		}
		defs    = make(map[string]jsonschema.Schema)
		propIdx = 0
	)

	sh, _ := r.Reflect(m,
		jsonschema.RootRef,
		jsonschema.InterceptDefName(func(t reflect.Type, _ string) string {
			return GetDefinitionName(t)
		}),
		jsonschema.DefinitionsPrefix("#/$defs/"),
		jsonschema.CollectDefinitions(func(name string, schema jsonschema.Schema) {
			if _, ok := defs[name]; ok {
				return
			}
			defs[name] = schema
		}),
		jsonschema.InterceptProp(func(params jsonschema.InterceptPropParams) error {
			if !params.Processed {
				return nil
			}
			propIdx++

			// make sure we do not ignore our custom props listed in scalarCustomProps
			for _, cp := range scalarCustomProps {
				if prop, ok := params.Field.Tag.Lookup(cp); ok {
					var propVal interface{}
					if prop == "true" || prop == "false" {
						propVal, _ = strconv.ParseBool(prop)
					} else if f, err := strconv.ParseFloat(prop, 64); err == nil {
						// looks like its float
						propVal = f
					} else if i, err := strconv.Atoi(prop); err == nil {
						// looks like its int
						propVal = i
					} else {
						// as is
						propVal = prop
					}
					params.PropertySchema.WithExtraPropertiesItem(cp, propVal)
				}
			}

			// make sure we do not ignore our custom props listed in
			for _, cp := range arrayCustomProps {
				if prop, ok := params.Field.Tag.Lookup(cp); ok {
					params.PropertySchema.WithExtraPropertiesItem(cp, strings.Split(prop, ","))
				}
			}

			// ensure each schema has it's definition
			configurable := interfaceBool(params.PropertySchema.ExtraProperties["configurable"])
			shared := interfaceBool(params.PropertySchema.ExtraProperties["shared"])

			// autoinc
			params.PropertySchema.WithExtraPropertiesItem("propertyOrder", propIdx)

			if !configurable && !shared && !params.PropertySchema.HasType(jsonschema.Object) {
				return nil
			}

			//
			defName := GetDefinitionName(params.Field.Type)

			clone, _ := params.PropertySchema.JSONSchema() //
			clone.Ref = nil
			defs[defName] = clone

			// register definitions without $ref

			// place $ref instead
			ref := fmt.Sprintf("#/$defs/%s", defName)
			refOnly := jsonschema.Schema{}
			refOnly.Ref = &ref
			refOnly.WithExtraPropertiesItem("propertyOrder", propIdx)

			*params.PropertySchema = refOnly
			return nil

		}),

		jsonschema.InterceptNullability(func(params jsonschema.InterceptNullabilityParams) {
			// fires when something is null
			params.Schema.RemoveType(jsonschema.Null)
		}),
	)

	// calculate path
	// build json path for each definition how it's related to node's root
	definitionPaths := make(map[string]tagDefinition)

	//

	for defName, schema := range defs {

		for k, v := range schema.Properties {

			var typ jsonschema.SimpleType
			if v.TypeObject != nil && v.TypeObject.Type != nil && v.TypeObject.Type.SimpleTypes != nil {
				typ = *v.TypeObject.Type.SimpleTypes
			}
			path := k
			ref := v.TypeObject.Ref
			if typ == jsonschema.Array {
				path = fmt.Sprintf("%s[0]", path)
				ref = v.TypeObject.ItemsEns().SchemaOrBoolEns().TypeObjectEns().Ref
			}
			if ref == nil {
				continue
			}
			from := strings.TrimPrefix(*ref, "#/$defs/")
			if defName == from {
				// avoid dead loop
				continue
			}
			t := tagDefinition{
				Path:       path,
				Definition: defName,
			}
			definitionPaths[from] = t
		}
	}

	for defName, schema := range defs {

		// update all definitions with path
		path := strings.Join(reverse(append(getPath(defName, definitionPaths, []string{}), "$")), ".")
		//	add json path to each definition
		updated := schema.WithExtraPropertiesItem("path", path)
		defs[defName] = *updated
	}

	sh.WithExtraPropertiesItem("$defs", defs)

	// schema post-processing hook
	if processor, ok := m.(Processor); ok {
		processor.Process(&sh)
	}

	return sh, nil
}

func getPath(defName string, all map[string]tagDefinition, path []string) []string {
	if p, ok := all[defName]; ok {
		// check parent
		return getPath(p.Definition, all, append(path, p.Path))
	}
	return path
}

func reverse(s []string) []string {
	n := reflect.ValueOf(s).Len()
	swap := reflect.Swapper(s)
	for i, j := 0, n-1; i < j; i, j = i+1, j-1 {
		swap(i, j)
	}
	return s
}

func CreateSchemaAndData(m interface{}) ([]byte, []byte, error) {
	confData, err := json.Marshal(m)
	if err != nil {
		return nil, nil, err
	}

	sh, err := CreateSchema(m)
	if err != nil {
		return nil, nil, err
	}
	confSchema, err := sh.MarshalJSON()
	if err != nil {
		return nil, nil, err
	}

	return confSchema, confData, nil
}

func interfaceBool(v interface{}) bool {
	if value, ok := v.(bool); ok {
		return value
	}
	return false
}

type Processor interface {
	Process(s *jsonschema.Schema)
}
