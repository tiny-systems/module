package schema

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/spyzhov/ajson"
)

// UpdateWithDefinitions parses schema and update all definitions in it by using map of ajson.nodes
// definitions replacing preserves path and configurable properties
func UpdateWithDefinitions(realSchema []byte, configurableDefinitionNodes map[string]*ajson.Node) ([]byte, error) {
	// status
	realSchemaNode, err := ajson.Unmarshal(realSchema)
	if err != nil {
		return nil, errors.Wrap(err, "error reading original schema")
	}

	realSchemaNodeDefs, err := realSchemaNode.GetKey("$defs")
	if err != nil {
		return nil, err
	}

	// go through status definitions
	for _, defKey := range realSchemaNodeDefs.Keys() {
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
			confCopy := conf.Clone()

			if path, ok := GetStr("path", realSchemaDef); ok {
				_ = SetStr("path", path, confCopy)
			}

			configurable, _ := GetBool("configurable", realSchemaDef)

			if err = SetBool("configurable", configurable, confCopy); err != nil {
				return nil, fmt.Errorf("set bool error: %w", err)
			}

			if readonly, _ := GetBool("readonly", realSchemaDef); readonly {
				_ = SetBool("readonly", readonly, confCopy)
			}

			// update real schema from configurable definitions but copy path,configurable,propertyOrder props from status real schema
			if err = realSchemaDef.SetNode(confCopy); err != nil {
				return nil, err
			}
		}
	}
	return ajson.Marshal(realSchemaNode)
}

func SetStr(param string, val string, v *ajson.Node) error {
	if c, err := v.GetKey(param); err == nil {
		return c.SetString(val)
	}
	return v.AppendObject(param, ajson.StringNode("", val))
}

func SetBool(param string, val bool, v *ajson.Node) error {

	if c, _ := v.GetKey(param); c != nil {
		if err := c.SetBool(val); err != nil {
			return err
		}
	}

	return v.AppendObject(param, ajson.BoolNode("", val))
}

func SetInt(param string, val int, v *ajson.Node) error {
	if c, err := v.GetKey(param); err == nil {
		return c.SetNumeric(float64(val))
	}
	return v.AppendObject(param, ajson.NumericNode("", float64(val)))
}

func GetStr(param string, v *ajson.Node) (string, bool) {
	c, _ := v.GetKey(param)
	if c != nil {
		// if node has override
		b, _ := c.GetString()
		return b, true
	}
	return "", false
}

func GetInt(param string, v *ajson.Node) (int, bool) {
	c, _ := v.GetKey(param)
	if c != nil {
		// if node has override
		b, _ := c.GetNumeric()
		return int(b), true
	}
	return 0, false
}

func GetBool(param string, v *ajson.Node) (bool, bool) {
	c, _ := v.GetKey(param)
	if c != nil {
		// if node has override
		b, _ := c.GetBool()
		return b, true
	}
	return false, false
}
