package runner

import (
	"github.com/pkg/errors"
	"github.com/spyzhov/ajson"
	"golang.org/x/exp/slices"
)

func UpdateWithConfigurableDefinitions(original []byte, updateWith []byte, configurableDefinitions map[string]*ajson.Node) ([]byte, error) {
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

	keys := originalNodeDefs.Keys()
	slices.Sort(keys)

	for _, defKey := range keys {
		v, err := originalNodeDefs.GetKey(defKey)

		if err != nil {
			return nil, errors.Wrapf(err, "unable to get original key: %s", defKey)
		}
		if v == nil {
			continue
		}
		if conf, ok := configurableDefinitions[defKey]; ok {
			// replace this def with configurable one
			// store important props before replace

			_ = setStr("path", getStr("path", v), conf)
			_ = setBool("configurable", getBool("configurable", v), conf)
			_ = setInt("propertyOrder", getInt("propertyOrder", v), conf)

			if err = v.SetNode(conf); err != nil {
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
