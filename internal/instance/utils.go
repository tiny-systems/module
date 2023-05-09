package instance

import (
	"encoding/json"
	"fmt"
	"github.com/pkg/errors"
	"github.com/spyzhov/ajson"
	modulepb "github.com/tiny-systems/module/pkg/api/module-go"
	m "github.com/tiny-systems/module/pkg/module"
	schema2 "github.com/tiny-systems/module/pkg/schema"
	"google.golang.org/protobuf/types/known/structpb"
)

func NewApiNode(instance m.Component, conf *Configuration) (*modulepb.Node, error) {

	ports := instance.Ports()
	node := &modulepb.Node{
		Ports: make([]*modulepb.NodePort, len(ports)),
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
		nodePort := &modulepb.NodePort{
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
		schema, err := schema2.CreateSchema(port.Message)
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
