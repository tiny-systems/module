package evaluator

import (
	"fmt"
	"github.com/spyzhov/ajson"
)

type Callback func(expression string) (interface{}, error)

type Evaluator struct {
	callback Callback
}

var DefaultCallback Callback = func(expression string) (interface{}, error) {
	return nil, fmt.Errorf("not implemented")
}

func NewEvaluator(callback Callback) *Evaluator {
	if callback == nil {
		callback = DefaultCallback
	}
	return &Evaluator{callback: callback}
}

func (c *Evaluator) calculateResult(valNode *ajson.Node) (interface{}, error) {

	if valNode.IsObject() {
		o := valNode.MustObject()
		if len(o) == 2 {
			// our special node
			// value key or expression or both should exist
			if expression, ok := o["expression"]; ok {
				if expr, _ := expression.GetString(); expr != "" {
					// if expression exists - calculate it
					return c.callback(expr)
				}
				var ok bool
				if valNode, ok = o["value"]; ok {
					return c.calculateResult(valNode)
				}
			}
		}
	}

	if valNode == nil {
		// something went wrong, current object has no value sub node, return root as it is
		return nil, nil
	}
	// recreate structure
	if valNode.IsObject() {
		m := map[string]interface{}{}
		for _, propName := range valNode.Keys() {
			propNode, err := valNode.GetKey(propName)
			if err != nil {
				return nil, err
			}
			res, err := c.calculateResult(propNode)
			if err != nil {
				return nil, err
			}
			m[propName] = res
		}
		return m, nil

	} else if valNode.IsArray() {
		inheritors := valNode.Inheritors()
		m := make([]interface{}, len(inheritors))
		for idx, node := range inheritors {
			val, err := c.calculateResult(node)
			if err != nil {
				return nil, err
			}
			m[idx] = val
		}
		return m, nil
	}
	return valNode.Unpack()
}

func (c *Evaluator) Eval(data []byte) (interface{}, error) {
	root, err := ajson.Unmarshal(data)
	if err != nil {
		return nil, err
	}
	if !root.IsObject() {
		return nil, fmt.Errorf("node is not an object it is %v %s", root.Type(), string(data))
	}
	result, err := c.calculateResult(root)
	if err != nil {
		return nil, err
	}
	// if we fail maybe its not a "valued" object
	// fallback to json unmarshal as it is
	return result, nil
}
