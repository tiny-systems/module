package schema

import (
	"fmt"
	"github.com/goccy/go-json"
	"github.com/pkg/errors"
	"github.com/spyzhov/ajson"
	"github.com/swaggest/jsonschema-go"
	"golang.org/x/exp/slices"
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
	"requiredWhen", "propertyOrder", "optionalWhen", "colSpan", "tab", "align", "configurable", "$ref", "type", "readonly", "format",
}

var arrayCustomProps = []string{
	"enumTitles",
}

func getDefinitionName(t reflect.Type) string {
	return cases.Title(language.English).String(t.Name())
}

func ParseSchema(s []byte, confDefs map[string]*ajson.Node) error {
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
		if !configurable {
			continue
		}
		confDefs[defName] = def

	}
	return nil
}

func CreateSchema(m interface{}) (jsonschema.Schema, error) {

	r := jsonschema.Reflector{
		DefaultOptions: make([]func(ctx *jsonschema.ReflectContext), 0),
	}

	var (
		defs = make(map[string]jsonschema.Schema)
	)

	sh, _ := r.Reflect(m,
		jsonschema.RootRef,
		jsonschema.InterceptDefName(func(t reflect.Type, _ string) string {
			return getDefinitionName(t)
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

			if !configurable && !params.PropertySchema.HasType(jsonschema.Object) {
				return nil
			}

			defName := getDefinitionName(params.Field.Type)

			clone, _ := params.PropertySchema.JSONSchema() //
			clone.Ref = nil
			defs[defName] = clone

			// register definitions without $ref

			// place $ref instead
			ref := fmt.Sprintf("#/$defs/%s", defName)
			refOnly := jsonschema.Schema{}
			refOnly.Ref = &ref

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

	// keep the sorting order
	defNames := make([]string, 0)
	for k := range defs {
		defNames = append(defNames, k)
	}
	slices.Sort(defNames)
	//

	for _, defName := range defNames {
		schema := defs[defName]

		propsNames := make([]string, 0)
		for k := range schema.Properties {
			propsNames = append(propsNames, k)
		}

		slices.Sort(propsNames)

		for _, k := range propsNames {
			v := schema.Properties[k]

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

	for _, defName := range defNames {
		schema := defs[defName]

		// update all definitions with path
		path := strings.Join(reverse(append(getPath(defName, definitionPaths, []string{}), "$")), ".")
		//	add json path to each definition
		updated := schema.WithExtraPropertiesItem("path", path)
		defs[defName] = *updated
	}

	sh.WithExtraPropertiesItem("$defs", defs)
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
