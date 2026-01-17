package jsonschemagenerator

import (
	"fmt"
	"strings"

	"github.com/brianvoe/gofakeit/v6"
	"github.com/spyzhov/ajson"
)

/*
Generates fake JSON data based on a JSON schema given, supports callback for custom data
*/

type Callback func(*ajson.Node) (result interface{}, replace bool, err error)

var DefaultCallback Callback = func(node *ajson.Node) (interface{}, bool, error) {
	return nil, false, nil
}

type JSONSchemaBasedDataGenerator struct {
	root    *ajson.Node
	visited map[string]struct{}
}

func NewSchemaBasedDataGenerator() *JSONSchemaBasedDataGenerator {
	return &JSONSchemaBasedDataGenerator{visited: make(map[string]struct{})}
}

// Generate generates fake data based on a JSON schema
// @todo recursion check too strict, definition nodes are mocked once instead of all places they are being referenced
func (se *JSONSchemaBasedDataGenerator) Generate(node *ajson.Node, clb Callback) (interface{}, error) {
	if node == nil {
		return nil, fmt.Errorf("nil node")
	}
	if se.root == nil {
		se.root = node
	}

	var err error
	ref, _ := GetStrKey("$ref", node)

	if ref != "" {
		// dereference
		node, err = se.getDefinition(ref)
	}
	if err != nil {
		return nil, fmt.Errorf("get definition error: %v", err)
	}

	oneOfNode, _ := node.GetKey("oneOf")
	if oneOfNode != nil && oneOfNode.IsArray() {
		return se.getOf(oneOfNode, clb)
	}

	anyOfNode, _ := node.GetKey("anyOf")
	if anyOfNode != nil && anyOfNode.IsArray() {
		return se.getOf(anyOfNode, clb)
	}

	if clb != nil {
		res, replace, err := clb(node)
		if replace {
			if err != nil {
				return nil, err
			}
			return res, nil
		}
	}
	if node.String() == "true" {
		// true schema
		// fake with null
		return nil, nil
	}
	typ, _ := GetStrKey("type", node)

	switch typ {
	case "":
		// empty schema - nil
		return nil, nil
	case "string", "number", "integer":
		return getDefaultValue(typ, node)
	case "array":
		itemsNode, err := node.GetKey("items")
		if err != nil {
			return nil, fmt.Errorf("items node error: %v", err)
		}
		def, err := getDefaultValue(typ, node)
		if err == nil {
			return def, nil
		}
		generateItem, err := se.Generate(itemsNode, clb)
		if err != nil {
			return nil, fmt.Errorf("can not generate example value for items of the array: %v", err)
		}
		if generateItem == nil {
			return []interface{}{}, nil
		}
		return []interface{}{generateItem}, nil
	case "boolean":
		return getDefaultValue(typ, node)
	case "object":
		m := make(map[string]interface{})

		propsNode, _ := node.GetKey("properties")
		if propsNode == nil || !propsNode.IsObject() {
			return getDefaultValue(typ, node)
		}
		if len(propsNode.Keys()) == 0 {
			return getDefaultValue(typ, node)
		}
		for _, propName := range propsNode.Keys() {
			propValue, err := propsNode.GetKey(propName)
			if err != nil {
				return nil, err
			}

			val, err := se.Generate(propValue, clb)
			if err != nil {
				return nil, err
			}
			m[propName] = val
		}
		return m, nil
	default:
		return nil, nil
	}
}

func (se *JSONSchemaBasedDataGenerator) getOf(n *ajson.Node, clb Callback) (interface{}, error) {
	oneOfKeys := n.Keys()
	if len(oneOfKeys) > 0 {
		firstOneOf, _ := n.GetIndex(gofakeit.IntRange(0, len(oneOfKeys)-1))
		if firstOneOf != nil {
			return se.Generate(firstOneOf, clb)
		}
	}
	return nil, fmt.Errorf("set is empty")
}

func (se *JSONSchemaBasedDataGenerator) getDefinition(ref string) (*ajson.Node, error) {
	if se.root == nil {
		return nil, fmt.Errorf("no root node")
	}
	defsNode, err := se.root.GetKey("$defs")
	if err != nil {
		return nil, fmt.Errorf("root search $defs error: %v", err)
	}
	nodeID := strings.Replace(ref, "#/$defs/", "", 1)
	defNode, err := defsNode.GetKey(nodeID)
	if err != nil {
		return nil, fmt.Errorf("definition node error: %v node: %s schema: %s", err, nodeID, se.root)
	}
	return defNode, nil
}

// GetNumericKey gets a numeric value from a JSON schema node by key
func GetNumericKey(key string, node *ajson.Node) (float64, error) {
	numNode, _ := node.GetKey(key)

	if numNode == nil {
		return 0, nil
	}
	if !numNode.IsNumeric() {
		return 0, fmt.Errorf("%s is not a numeric", key)
	}
	return numNode.GetNumeric()
}

// GetStrKey gets a string value from a JSON schema node by key
func GetStrKey(key string, node *ajson.Node) (string, error) {
	strNode, _ := node.GetKey(key)

	if strNode == nil {
		return "", nil
	}
	if !strNode.IsString() {
		return "", fmt.Errorf("%s is not a string", key)
	}
	return strNode.GetString()
}

func getDefaultValue(typ string, n *ajson.Node) (interface{}, error) {
	d, _ := n.GetKey("default")
	if d != nil {
		return d.Unpack()
	}

	e, _ := n.GetKey("enum")
	if e != nil {
		enum, err := e.Unpack()
		if err != nil {
			return nil, fmt.Errorf("unable to unpack enum")
		}
		if enumArray, ok := enum.([]interface{}); ok && len(enumArray) > 0 {
			k := gofakeit.IntRange(0, len(enumArray)-1)
			return enumArray[k], nil
		}
	}
	switch typ {
	case "object":
		return map[string]interface{}{}, nil
	case "number":
		minimum, _ := GetNumericKey("minimum", n)
		maximum, _ := GetNumericKey("maximum", n)
		if maximum != 0 || minimum != 0 {
			return gofakeit.Float64Range(minimum, maximum), nil
		}
		return gofakeit.Float64Range(0, 9999), nil
	case "integer":
		minimum, _ := GetNumericKey("minimum", n)
		maximum, _ := GetNumericKey("maximum", n)
		if maximum != 0 || minimum != 0 {
			return gofakeit.IntRange(int(minimum), int(maximum)), nil
		}
		return gofakeit.IntRange(0, 9999), nil
	case "boolean":
		return gofakeit.Bool(), nil
	case "string":
		// for strings, use description as default example value
		format, _ := GetStrKey("format", n)
		switch format {
		case "date-time":
			return "1984-09-06T10:00:00+00:00", nil
		case "time":
			return "10:00:00+00:00", nil
		case "date":
			return "1984-09-06", nil
		case "duration":
			return "2h", nil
		case "email":
			return "contact@tinysystems.io", nil
		case "hostname":
			return "example.com", nil
		case "ipv4":
			return "8.8.8.8", nil
		case "uuid":
			return "3e4666bf-d5e5-4aa7-b8ce-cefe41c7568a", nil
		case "uri":
			return "https://john.doe@example.com:123/forum/questions/?tag=networking&order=newest#top", nil
		}
		description, _ := GetStrKey("description", n)
		if description != "" {
			return description, nil
		}
		return fmt.Sprintf("fakedata:%s", gofakeit.NounAbstract()), nil
	}
	return nil, fmt.Errorf("unknown type")
}
