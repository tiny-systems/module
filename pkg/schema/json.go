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

type FilterFunction func(node *ajson.Node) bool

var FilterConfigurable FilterFunction = func(node *ajson.Node) bool {
	configurable, _ := GetBool("configurable", node)
	return configurable
}

var FilterShared FilterFunction = func(node *ajson.Node) bool {
	configurable, _ := GetBool("shared", node)
	return configurable
}

func getDefinitionName(t reflect.Type) string {
	var n = t.Name()
	if t.Kind() == reflect.Ptr {
		n = t.Elem().Name()
	}
	return cases.Title(language.English).String(n)
}

func getDefinitionNameArray(t reflect.Type) string {
	var n = t.Name()
	if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		n = t.Elem().Name()
	}
	return cases.Title(language.English).String(n)
}

//CollectDefinitions finds all shared and configurable definitions

func CollectDefinitions(s []byte, confDefs map[string]*ajson.Node, filter FilterFunction) error {
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

		if filter != nil && !filter(def) {
			continue
		}
		confDefs[defName] = def
	}
	return nil
}

func CreateSchema(val interface{}) (jsonschema.Schema, error) {
	if val == nil {
		// empty object empty schema
		return jsonschema.Schema{}, nil
	}

	var (
		r = jsonschema.Reflector{
			DefaultOptions: make([]func(ctx *jsonschema.ReflectContext), 0),
		}
		defs = make(map[string]jsonschema.Schema)
	)

	propIdxMap := make(map[string]int)

	var replaceRoot = func(defName string, s *jsonschema.Schema) jsonschema.Schema {
		if defName == "" {
			return *s
		}

		clone, _ := s.JSONSchema() //
		clone.Ref = nil
		defs[defName] = clone

		ref := fmt.Sprintf("#/$defs/%s", defName)
		refOnly := jsonschema.Schema{}
		refOnly.Ref = &ref
		return refOnly
	}

	sh, err := r.Reflect(val,
		jsonschema.InlineRefs,
		jsonschema.InterceptProp(func(params jsonschema.InterceptPropParams) error {
			if !params.Processed {
				return nil
			}
			propPath := strings.Join(params.Path, "")
			propIdxMap[propPath]++
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

			// when we have configurable == true
			// or shared == true
			// it is definitely object or type is unknown
			// create a definition for that

			// schema says we have items
			if params.PropertySchema.Items != nil {

				// get defName from item
				itemDefName := getDefinitionNameArray(params.Field.Type)
				// we probably failed cause kind of array holder is an interface
				if params.Field.Type.Kind() == reflect.Interface {
					// in this case we don't want item definition be named as property
					// add suffix
					itemDefName = fmt.Sprintf("%s%s", getDefinitionName(params.Field.Type), "Item")
				}

				// register items schema under definition name
				refOnly := replaceRoot(itemDefName, params.PropertySchema.Items.SchemaOrBool.TypeObject)
				ts := refOnly.ToSchemaOrBool()
				// replace property items with ref
				params.PropertySchema.Items.SchemaOrBool = &ts
			}

			if configurable || shared || params.PropertySchema.HasType(jsonschema.Object) || params.PropertySchema.HasType(jsonschema.Array) || params.PropertySchema.Type == nil {
				// ensure we have definitions for configurables, shared, objects, arrays and empty schemes
				refOnly := replaceRoot(getDefinitionName(params.Field.Type), params.PropertySchema)
				*params.PropertySchema = refOnly
			}

			// add custom sorting mechanism
			params.PropertySchema.WithExtraPropertiesItem("propertyOrder", propIdxMap[propPath])
			// place $ref instead
			return nil
		}),

		// all pointerss are nullable
		jsonschema.InterceptNullability(func(params jsonschema.InterceptNullabilityParams) {
			if params.Type.Kind() == reflect.Ptr {
				return
			}
			params.Schema.RemoveType(jsonschema.Null)
		}),
	)

	if err != nil {
		return jsonschema.Schema{}, err
	}

	// root schema is ref too
	sh = replaceRoot(getDefinitionName(reflect.TypeOf(val)), &sh)

	// calculate paths
	// build json path for each definition how it's related to node's root
	definitionPaths := make(map[string]tagDefinition)
	//

	for defName, schema := range defs {
		for k, v := range schema.Properties {
			path := k
			//
			ref := v.TypeObject.Ref
			//

			var refDefP *jsonschema.Schema
			if ref != nil {
				// something has ref, let's get definition for that red
				if refDef, ok := defs[strings.TrimPrefix(*ref, "#/$defs/")]; ok {
					refDefP = &refDef
				}
			}

			// our prop is array or def which it references to is an array
			if v.TypeObject.HasType(jsonschema.Array) || (refDefP != nil && refDefP.HasType(jsonschema.Array)) {
				path = fmt.Sprintf("%s[0]", path)

				if refDefP != nil && refDefP.Items != nil {
					ref = refDefP.Items.SchemaOrBoolEns().TypeObjectEns().Ref
				} else if v.TypeObject.Items != nil {
					ref = v.TypeObject.Items.SchemaOrBoolEns().TypeObjectEns().Ref
				}
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
	if processor, ok := val.(Processor); ok {
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
