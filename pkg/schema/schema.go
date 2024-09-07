package schema

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/spyzhov/ajson"
)

func UpdateWithConfigurableDefinitions(realSchema []byte, configurableDefinitionNodes map[string]*ajson.Node) ([]byte, error) {
	// status

	realSchemaNode, err := ajson.Unmarshal(realSchema)
	if err != nil {
		return nil, errors.Wrap(err, "error reading original schema")
	}

	realSchemaNodeDefs, err := realSchemaNode.GetKey("$defs")
	if err != nil {
		return nil, err
	}

	realSchemaNodeDefsKeys := realSchemaNodeDefs.Keys()

	// go through status definitions
	for _, defKey := range realSchemaNodeDefsKeys {
		realSchemaDef, err := realSchemaNodeDefs.GetKey(defKey)
		if err != nil {
			return nil, errors.Wrapf(err, "unable to get original key: %s", defKey)
		}
		if realSchemaDef == nil {
			continue
		}
		if conf, ok := configurableDefinitionNodes[defKey]; ok {
			// replace this real status def with configurable one
			// copy important props before replace

			if path, ok := getStr("path", realSchemaDef); ok {
				_ = setStr("path", path, conf)
			}

			configurable, _ := getBool("configurable", realSchemaDef)

			if err = setBool("configurable", configurable, conf); err != nil {
				return nil, fmt.Errorf("set bool error: %w", err)
			}
			//
			//if propertyOrder, ok := getInt("propertyOrder", realSchemaDef); ok {
			//	_ = setInt("propertyOrder", propertyOrder, conf)
			//}

			// update real schema from configurable definitions but copy path,configurable,propertyOrder props from status real schema
			if err = realSchemaDef.SetNode(conf); err != nil {
				return nil, err
			}
			continue
		}
	}
	return ajson.Marshal(realSchemaNode)
}

func setStr(param string, val string, v *ajson.Node) error {
	if c, err := v.GetKey(param); err == nil {
		return c.SetString(val)
	}
	return v.AppendObject(param, ajson.StringNode("", val))
}

func setBool(param string, val bool, v *ajson.Node) error {

	if c, _ := v.GetKey(param); c != nil {
		return c.SetBool(val)
	}
	return v.AppendObject(param, ajson.BoolNode("", val))
}

func setInt(param string, val int, v *ajson.Node) error {
	if c, err := v.GetKey(param); err == nil {
		return c.SetNumeric(float64(val))
	}
	return v.AppendObject(param, ajson.NumericNode("", float64(val)))
}

func getStr(param string, v *ajson.Node) (string, bool) {
	c, _ := v.GetKey(param)
	if c != nil {
		// if node has override
		b, _ := c.GetString()
		return b, true
	}
	return "", false
}

func getInt(param string, v *ajson.Node) (int, bool) {
	c, _ := v.GetKey(param)
	if c != nil {
		// if node has override
		b, _ := c.GetNumeric()
		return int(b), true
	}
	return 0, false
}

func getBool(param string, v *ajson.Node) (bool, bool) {
	c, _ := v.GetKey(param)
	if c != nil {
		// if node has override
		b, _ := c.GetBool()
		return b, true
	}
	return false, false
}
