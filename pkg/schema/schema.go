package schema

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/tiny-systems/ajson"
)

// UpdateWithDefinitions parses schema and update all definitions in it by using map of ajson.nodes
// definitions replacing preserves path and configurable properties
// If the schema has no $defs, returns the original schema unchanged
func UpdateWithDefinitions(realSchema []byte, configurableDefinitionNodes map[string]*ajson.Node) ([]byte, error) {
	if len(realSchema) == 0 {
		return realSchema, nil
	}

	// status
	realSchemaNode, err := ajson.Unmarshal(realSchema)
	if err != nil {
		return nil, errors.Wrap(err, "error reading original schema")
	}

	realSchemaNodeDefs, err := realSchemaNode.GetKey("$defs")
	if err != nil {
		// Schema has no $defs, return original schema unchanged
		return realSchema, nil
	}

	// build a path-to-definition lookup for fallback matching
	// when definition key names differ (e.g. "Context" vs "Startcontext")
	// but both refer to the same field path (e.g. "$.context")
	pathToConf := make(map[string]*ajson.Node)
	for _, conf := range configurableDefinitionNodes {
		if p, ok := GetStr("path", conf); ok && p != "" {
			// prefer richer definitions (with explicit properties) over bare ones
			if existing, exists := pathToConf[p]; !exists || !hasProperties(existing) {
				pathToConf[p] = conf
			}
		}
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

		// try exact key name match first
		conf, ok := configurableDefinitionNodes[defKey]

		// If exact match found a bare definition (no explicit properties, only additionalProperties),
		// check if there's a richer definition available via path matching.
		// This handles the case where target node has bare "Startcontext" and source node
		// has rich "Context" — both with path "$.context".
		if ok && !hasProperties(conf) {
			if p, hasPath := GetStr("path", realSchemaDef); hasPath && p != "" {
				if pathConf, pathOk := pathToConf[p]; pathOk && hasProperties(pathConf) {
					conf = pathConf
				}
			}
		}

		if !ok {
			// fallback: match by path when key names differ
			if p, hasPath := GetStr("path", realSchemaDef); hasPath && p != "" {
				conf, ok = pathToConf[p]
			}
		}

		if ok {
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
			if shared, hasShared := GetBool("shared", realSchemaDef); hasShared {
				_ = SetBool("shared", shared, confCopy)
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

// hasProperties returns true if the node has a "properties" key with at least one child.
// Bare definitions (only additionalProperties, no explicit properties) return false.
func hasProperties(n *ajson.Node) bool {
	props, err := n.GetKey("properties")
	if err != nil || props == nil {
		return false
	}
	return props.IsObject() && len(props.Keys()) > 0
}

// cleanNode removes all attributes we don't need
func cleanNode(n *ajson.Node) error {

	return nil
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

// FilterSchemaAttributes removes specified custom attributes from a JSON schema
// It accepts the schema as []byte and returns the filtered schema as []byte
// The function recursively processes nested objects and arrays
func FilterSchemaAttributes(schemaBytes []byte, attributesToRemove []string) ([]byte, error) {
	// Parse the JSON schema
	root, err := ajson.Unmarshal(schemaBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Remove the specified attributes recursively
	removeAttributes(root, attributesToRemove)

	// Marshal back to JSON
	result, err := ajson.Marshal(root)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}

	return result, nil
}

// removeAttributes recursively removes specified attributes from the JSON tree
func removeAttributes(node *ajson.Node, attributesToRemove []string) {
	if node == nil {
		return
	}

	// If it's an object, process its properties
	if node.IsObject() {
		keys := node.Keys()

		// Collect nodes to delete
		var nodesToDelete []*ajson.Node

		// Iterate through all keys
		for _, key := range keys {
			child, err := node.GetKey(key)
			if err != nil {
				continue
			}

			// Check if this key should be removed
			shouldRemove := false
			for _, attr := range attributesToRemove {
				if key == attr {
					shouldRemove = true
					break
				}
			}

			if shouldRemove {
				nodesToDelete = append(nodesToDelete, child)
			} else {
				// Recursively process the child node
				removeAttributes(child, attributesToRemove)
			}
		}

		// Delete collected nodes
		for _, nodeToDelete := range nodesToDelete {
			nodeToDelete.Delete()
		}
	}

	// If it's an array, process each element
	if node.IsArray() {
		// Iterate through array elements by index
		for i := 0; i < node.Size(); i++ {
			child, err := node.GetIndex(i)
			if err == nil {
				removeAttributes(child, attributesToRemove)
			}
		}
	}
}
