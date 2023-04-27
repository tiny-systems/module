package instance

import (
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"github.com/spyzhov/ajson"
	"github.com/swaggest/jsonschema-go"
	"github.com/tiny-systems/module/pkg/api/module-go"
	m "github.com/tiny-systems/module/pkg/module"
	"google.golang.org/protobuf/types/known/structpb"
	"reflect"
	"strconv"
	"strings"
)

var scalarCustomProps = []string{
	"requiredWhen", "propertyOrder", "optionalWhen", "colSpan", "configurable", "$ref", "type", "readonly", "format",
}

var arrayCustomProps = []string{
	"enumTitles",
}

type TagDefinition struct {
	Path       string
	Definition string
}

func createSchema(m interface{}) (jsonschema.Schema, error) {
	r := jsonschema.Reflector{DefaultOptions: make([]func(ctx *jsonschema.ReflectContext), 0)}

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
	definitionPaths := make(map[string]TagDefinition)
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
			t := TagDefinition{
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

		pathParts := append(getPath(k, definitionPaths, []string{}), "$")
		path := strings.Join(reverse(pathParts), ".")
		//	add json path to each definition
		updated := d.WithExtraPropertiesItem("path", path)
		defs[k] = *updated
	}

	sh.WithExtraPropertiesItem("$defs", defs)
	return sh, nil
}

func NewApiNode(instance m.Component, conf *Configuration) (*module.Node, error) {

	ports := instance.Ports()
	node := &module.Node{
		Ports: make([]*module.NodePort, len(ports)),
	}

	var data map[string]interface{}
	if conf != nil {
		data = conf.Data
		node.ComponentID = conf.ComponentID
	}
	if data == nil {
		data = map[string]interface{}{}
	}
	data["component_name"] = instance.GetInfo().Name
	data["component_info"] = instance.GetInfo().Info
	data["component_description"] = instance.GetInfo().Description

	dataStruct, err := structpb.NewStruct(data)
	if err != nil {
		return nil, fmt.Errorf("data struct error: %v", err)
	}
	node.Data = dataStruct

	if _, ok := instance.(m.Runnable); ok {
		node.Runnable = true
	}

	// settings then source ports then destination ports
	for k, port := range ports {
		nodePort := &module.NodePort{
			PortName:   port.Name,
			Label:      port.Label,
			Position:   int32(port.Position),
			IsSettings: port.Settings,
			Status:     port.Status,
			Source:     port.Source,
		}
		if port.Message == nil {
			return nil, fmt.Errorf("port DTO message is nil")
		}

		// define default schema and config using reflection
		schema, err := createSchema(port.Message)
		if err != nil {
			return nil, err
		}

		schemaData, _ := schema.MarshalJSON()
		nodePort.SchemaDefault = schemaData

		confData, _ := json.Marshal(port.Message)
		nodePort.ConfigurationDefault = confData
		node.Ports[k] = nodePort
	}
	return node, nil
}

func UpdateWithConfigurableDefinitions(portName string, original []byte, updateWith []byte, configurableDefinitions map[string]*ajson.Node) ([]byte, error) {
	originalNode, err := ajson.Unmarshal(original)
	if err != nil {
		return nil, errors.Wrap(err, "error reading original schema")
	}

	updateWithNode, err := ajson.Unmarshal(updateWith)
	if err != nil {
		return nil, errors.Wrap(err, "error reading updatedWith schema")
	}
	originalNodeDefs, err := originalNode.GetKey("$defs")
	if err != nil {
		return nil, err
	}
	updateWithNodeDefs, err := updateWithNode.GetKey("$defs")
	if err != nil {
		return nil, err
	}

	for _, defKey := range originalNodeDefs.Keys() {
		v, err := originalNodeDefs.GetKey(defKey)

		if err != nil {
			return nil, errors.Wrapf(err, "unable to get original key: %s", defKey)
		}
		if v == nil {
			continue
		}
		if r, ok := configurableDefinitions[defKey]; ok {
			// replace this def with configurable one
			// store important props before replace
			configurable := getBool("configurable", v)
			path := getStr("path", v)
			propertyOrder := getInt("propertyOrder", v)

			//
			_ = setStr("path", path, r)
			_ = setBool("configurable", configurable, r)
			_ = setInt("propertyOrder", propertyOrder, r)

			if err = v.SetNode(r); err != nil {
				return nil, err
			}
			continue
		}

		if !getBool("configurable", v) {
			continue
		}
		updated, err := updateWithNodeDefs.GetKey(defKey)
		if updated == nil {
			// in user's customised schema definition is not presented, skipping
			continue
		}
		if err = v.SetNode(updated); err != nil {
			return nil, err
		}
		configurableDefinitions[defKey] = updated
	}

	return ajson.Marshal(originalNode)
}

func setStr(param string, val string, v *ajson.Node) error {
	if c, err := v.GetKey(param); err == nil {
		return c.SetNode(ajson.StringNode("", val))
	}
	return v.AppendObject(param, ajson.StringNode("", val))
}

func setBool(param string, val bool, v *ajson.Node) error {
	if c, err := v.GetKey(param); err == nil {
		return c.SetNode(ajson.BoolNode("", val))
	}
	return v.AppendObject(param, ajson.BoolNode("", val))
}

func setInt(param string, val int, v *ajson.Node) error {
	if c, err := v.GetKey(param); err == nil {
		return c.SetNode(ajson.NumericNode("", float64(val)))
	}
	return v.AppendObject(param, ajson.NumericNode("", float64(val)))
}

func getStr(param string, v *ajson.Node) string {
	c, _ := v.GetKey(param)
	if c != nil {
		// if node has override
		b, _ := c.GetString()
		return b
	}
	return ""
}

func getInt(param string, v *ajson.Node) int {
	c, _ := v.GetKey(param)
	if c != nil {
		// if node has override
		b, _ := c.GetNumeric()
		return int(b)
	}
	return 0
}

func getBool(param string, v *ajson.Node) bool {
	c, _ := v.GetKey(param)
	if c != nil {
		// if node has override
		b, _ := c.GetBool()
		return b
	}
	return false
}

func getPath(defName string, all map[string]TagDefinition, path []string) []string {
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
