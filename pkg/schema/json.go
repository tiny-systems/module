package schema

import (
	"fmt"
	"github.com/goccy/go-json"
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
	"requiredWhen", "propertyOrder", "optionalWhen", "colSpan", "align", "configurable", "$ref", "type", "readonly", "format",
}

var arrayCustomProps = []string{
	"enumTitles",
}

func getDefinitionName(t reflect.Type) string {
	return cases.Title(language.English).String(t.Name())
}

func CreateSchema(m interface{}, confDefs map[string]jsonschema.Schema) (jsonschema.Schema, error) {
	r := jsonschema.Reflector{
		DefaultOptions: make([]func(ctx *jsonschema.ReflectContext), 0),
	}

	if confDefs == nil {
		confDefs = make(map[string]jsonschema.Schema)
	}

	var (
		defs = make(map[string]jsonschema.Schema)
	)

	sh, _ := r.Reflect(m,
		jsonschema.RootRef,
		jsonschema.InterceptDefName(func(t reflect.Type, defaultDefName string) string {
			return getDefinitionName(t)
		}),
		jsonschema.DefinitionsPrefix("#/$defs/"),
		jsonschema.CollectDefinitions(func(name string, schema jsonschema.Schema) {
			defs[name] = schema
		}),
		jsonschema.InterceptProp(func(params jsonschema.InterceptPropParams) error {
			if !params.Processed {
				return nil
			}

			// looking for values  for certain properties
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

			for _, cp := range arrayCustomProps {
				if prop, ok := params.Field.Tag.Lookup(cp); ok {
					params.PropertySchema.WithExtraPropertiesItem(cp, strings.Split(prop, ","))
				}
			}

			defName := getDefinitionName(params.Field.Type)

			//
			if b, ok := params.PropertySchema.ExtraProperties["configurable"]; !ok || b != true {
				// not configurable; replace with configurable if any

				if conf, ok := confDefs[defName]; ok {
					// replacing with configurable schema
					defs[defName] = conf
					refOnly := jsonschema.Schema{}
					ref := fmt.Sprintf("#/$defs/%s", defName)
					refOnly.Ref = &ref
					refOnly.WithExtraPropertiesItem("propertyOrder", params.PropertySchema.ExtraProperties["propertyOrder"])
					refOnly.WithExtraPropertiesItem("configurable", false)
					*params.PropertySchema = refOnly
				}
				// type is not configurable in here, but maybe be it is within the shared
				return nil
			}

			// we need to update definition with title and description, only info we know from tags
			// problem here I solve is that tags affect on inline schema and not the one in $ref
			// copy from inline to ref definition
			//if propertySchema.Ref == nil {
			//	return nil
			//}
			// make sure types with configurable tags always has own def
			// update defs with everything except ref

			//ref := *propertySchema.Ref
			ref := fmt.Sprintf("#/$defs/%s", defName)
			//defID := strings.TrimPrefix(ref, "#/$defs/")

			params.PropertySchema.Ref = nil
			confDefs[defName] = *params.PropertySchema

			refOnly := jsonschema.Schema{}
			refOnly.Ref = &ref

			refOnly.WithExtraPropertiesItem("propertyOrder", params.PropertySchema.ExtraProperties["propertyOrder"])
			*params.PropertySchema = refOnly
			return nil

		}),

		jsonschema.InterceptNullability(func(params jsonschema.InterceptNullabilityParams) {
			// fires when something is null
			if params.Type.Kind() == reflect.Array || params.Type.Kind() == reflect.Slice {
				a := jsonschema.Array.Type()
				params.Schema.Type = &a
			}
		}),
	)

	// build json path for each definition how it's related to node's root
	definitionPaths := make(map[string]tagDefinition)

	defNames := make([]string, 0)
	for k := range defs {
		defNames = append(defNames, k)
	}
	slices.Sort(defNames)

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

		if c, ok := confDefs[defName]; ok {
			// definition is configurable
			schema.Title = c.Title
			schema.Description = c.Description
			if schema.Type == nil {
				schema.Type = c.Type
			}
			schema.WithExtraPropertiesItem("configurable", true)
		}

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

	sh, err := CreateSchema(m, nil)
	if err != nil {
		return nil, nil, err
	}
	confSchema, err := sh.MarshalJSON()
	if err != nil {
		return nil, nil, err
	}

	return confSchema, confData, nil
}
