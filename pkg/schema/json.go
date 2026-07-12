package schema

import (
	"fmt"
	"github.com/goccy/go-json"
	"github.com/pkg/errors"
	"github.com/tiny-systems/ajson"
	"github.com/swaggest/jsonschema-go"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

type tagDefinition struct {
	Path       string
	Definition string
}

var scalarCustomProps = []string{
	"requiredWhen", "propertyOrder", "optionalWhen", "colSpan", "tab", "align", "configurable", "shared", "$ref", "type", "readonly", "format", "language",
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

				// Propagate shared/configurable from array field to items definition.
				// When `shared:"true"` is on []ItemContext, the items type should also
				// be marked shared so GetFlowMaps can add port annotations.
				itemSchema := params.PropertySchema.Items.SchemaOrBool.TypeObject
				if shared {
					itemSchema.WithExtraPropertiesItem("shared", true)
				}
				if configurable {
					itemSchema.WithExtraPropertiesItem("configurable", true)
				}

				// register items schema under definition name
				refOnly := replaceRoot(itemDefName, itemSchema)
				ts := refOnly.ToSchemaOrBool()
				// replace property items with ref
				params.PropertySchema.Items.SchemaOrBool = &ts
			}

			if configurable || shared || params.PropertySchema.HasType(jsonschema.Object) || params.PropertySchema.HasType(jsonschema.Array) || params.PropertySchema.Type == nil {
				// ensure we have definitions for configurable, shared, objects, arrays and empty schemes
				refOnly := replaceRoot(getDefinitionName(params.Field.Type), params.PropertySchema)
				*params.PropertySchema = refOnly
			}

			// add custom sorting mechanism
			params.PropertySchema.WithExtraPropertiesItem("propertyOrder", propIdxMap[propPath])
			// place $ref instead
			return nil
		}),

		// all pointers are nullable
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

	// Materialize the observed data shape into the schema. Reflection over an
	// `any` field holding a map can only say "bare object"; the LIVE VALUE
	// knows the actual keys and types (js_eval's outputData
	// {userMessage: "..."} → properties.userMessage: string, default set to
	// the observed value so the simulator mocks REAL example data). This is
	// the ONE inference point: the published Status schema feeds edge
	// validation, the MCP tools, and the UI — none of them infer on their own.
	// Operates on the typed defs in place — untouched schemas serialize
	// byte-identically to before.
	enrichDefsFromValue(defs, getDefinitionName(reflect.TypeOf(val)), val)

	sh.WithExtraPropertiesItem("$defs", defs)

	// schema post-processing hook
	if processor, ok := val.(Processor); ok {
		processor.Process(&sh)
	}

	return sh, nil
}

// enrichDefsFromValue walks the reflected definitions alongside the live value
// and fills object defs that have NO explicit properties (bare map reflection)
// with properties inferred from the value's actual keys. Existing explicit
// properties are never modified — recursion only descends through them to
// reach nested any-typed fields.
func enrichDefsFromValue(defs map[string]jsonschema.Schema, rootDefName string, val interface{}) {
	valBytes, err := json.Marshal(val)
	if err != nil {
		return // best-effort; never fail schema creation
	}
	var value interface{}
	if err := json.Unmarshal(valBytes, &value); err != nil {
		return
	}

	visited := map[string]struct{}{}

	var walkSchema func(s *jsonschema.Schema, v interface{})
	var walkDef func(defName string, v interface{})

	walkDef = func(defName string, v interface{}) {
		if _, seen := visited[defName]; seen {
			return
		}
		visited[defName] = struct{}{}
		def, ok := defs[defName]
		if !ok {
			return
		}
		walkSchema(&def, v)
		defs[defName] = def
	}

	walkSchema = func(s *jsonschema.Schema, v interface{}) {
		if s == nil || v == nil {
			return
		}
		if s.Ref != nil {
			walkDef(strings.TrimPrefix(*s.Ref, "#/$defs/"), v)
			return
		}

		// descend arrays into items with the first element
		if arr, ok := v.([]interface{}); ok {
			if s.Items != nil && s.Items.SchemaOrBool != nil && len(arr) > 0 {
				walkSchema(s.Items.SchemaOrBool.TypeObject, arr[0])
			}
			return
		}

		vm, ok := v.(map[string]interface{})
		if !ok || len(vm) == 0 {
			return
		}

		if len(s.Properties) > 0 {
			// explicit properties — never modified, only descended
			for k, pv := range vm {
				if p, ok := s.Properties[k]; ok {
					walkSchema(p.TypeObject, pv)
				}
			}
			return
		}

		// bare object (map/any reflection) — materialize observed keys
		if s.Type != nil && !s.HasType(jsonschema.Object) {
			return
		}
		keys := make([]string, 0, len(vm))
		for k := range vm {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			node := inferValueSchema(vm[k])
			node["propertyOrder"] = i + 1
			nodeBytes, err := json.Marshal(node)
			if err != nil {
				continue
			}
			var ps jsonschema.Schema
			if err := ps.UnmarshalJSON(nodeBytes); err != nil {
				continue
			}
			s.WithPropertiesItem(k, ps.ToSchemaOrBool())
		}
	}

	walkDef(rootDefName, value)
}

// inferValueSchema builds a property schema from an observed value, carrying
// the value itself as `default` so the schema-based data generator mocks the
// REAL example instead of placeholder filler.
func inferValueSchema(v interface{}) map[string]interface{} {
	switch t := v.(type) {
	case string:
		return map[string]interface{}{"type": "string", "default": t}
	case float64:
		return map[string]interface{}{"type": "number", "default": t}
	case bool:
		return map[string]interface{}{"type": "boolean", "default": t}
	case []interface{}:
		node := map[string]interface{}{"type": "array", "default": t}
		if len(t) > 0 {
			node["items"] = inferValueSchema(t[0])
		}
		return node
	case map[string]interface{}:
		props := map[string]interface{}{}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			p := inferValueSchema(t[k])
			p["propertyOrder"] = i + 1
			props[k] = p
		}
		return map[string]interface{}{"type": "object", "properties": props}
	default:
		return map[string]interface{}{}
	}
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
