package schema

import (
	"fmt"
	"github.com/goccy/go-json"
	"github.com/swaggest/jsonschema-go"
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

func CreateSchema(m interface{}) (jsonschema.Schema, error) {
	r := jsonschema.Reflector{
		DefaultOptions: make([]func(ctx *jsonschema.ReflectContext), 0),
	}

	var (
		defs     = make(map[string]jsonschema.Schema)
		confDefs = make(map[string]jsonschema.Schema)
	)

	sh, _ := r.Reflect(m,
		jsonschema.RootRef,
		jsonschema.DefinitionsPrefix("#/$defs/"),
		jsonschema.CollectDefinitions(func(name string, schema jsonschema.Schema) {
			defs[name] = schema
		}),
		jsonschema.InterceptProperty(func(name string, field reflect.StructField, propertySchema *jsonschema.Schema) error {
			for _, cp := range scalarCustomProps {
				if prop, ok := field.Tag.Lookup(cp); ok {
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
					propertySchema.WithExtraPropertiesItem(cp, propVal)
				}
			}

			for _, cp := range arrayCustomProps {
				if prop, ok := field.Tag.Lookup(cp); ok {
					propertySchema.WithExtraPropertiesItem(cp, strings.Split(prop, ","))
				}
			}
			//
			if b, ok := propertySchema.ExtraProperties["configurable"]; !ok || b != true {
				return nil
			}

			//tag with here configurable, we need to update definition with title and description, only info we know from tags
			// problem here I solve is that tags affect on inline schema and not the one in $ref
			// copy from inline to ref definition
			if propertySchema.Ref == nil {
				return nil
			}
			// make sure types with configurable tags always has own def
			// update defs with everything except ref

			ref := *propertySchema.Ref
			defID := strings.TrimPrefix(ref, "#/$defs/")

			propertySchema.Ref = nil
			propertyOrder := propertySchema.ExtraProperties["propertyOrder"]
			confDefs[defID] = *propertySchema

			refOnly := jsonschema.Schema{}
			refOnly.Ref = &ref
			refOnly.WithExtraPropertiesItem("propertyOrder", propertyOrder)
			*propertySchema = refOnly
			return nil
		}),
		jsonschema.InterceptNullability(func(params jsonschema.InterceptNullabilityParams) {
			if params.Type.Kind() == reflect.Array || params.Type.Kind() == reflect.Slice {
				a := jsonschema.Array.Type()
				params.Schema.Type = &a
			}
		}),
	)

	// build json path for each definition how it's related to node's root
	definitionPaths := make(map[string]tagDefinition)
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
	for k, d := range defs {
		if c, ok := confDefs[k]; ok {
			// definition is configurable
			d.Title = c.Title
			d.Description = c.Description
			if d.Type == nil {
				d.Type = c.Type
			}
			d.WithExtraPropertiesItem("configurable", true)
		}

		path := strings.Join(reverse(append(getPath(k, definitionPaths, []string{}), "$")), ".")
		//	add json path to each definition
		updated := d.WithExtraPropertiesItem("path", path)
		defs[k] = *updated
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
