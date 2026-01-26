package evaluator

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spyzhov/ajson"
)

type Callback func(expression string) (interface{}, error)
type ErrorCallback func(expression string, err error)

type Evaluator struct {
	callback Callback
	onError  ErrorCallback
}

var DefaultCallback Callback = func(expression string) (interface{}, error) {
	return nil, fmt.Errorf("not implemented")
}

// exprPattern matches {{expression}} patterns
var exprPattern = regexp.MustCompile(`\{\{(.+?)\}\}`)

// pureExprPattern matches strings that are ONLY {{expression}} with nothing else
var pureExprPattern = regexp.MustCompile(`^\{\{(.+)\}\}$`)

func NewEvaluator(callback Callback) *Evaluator {
	if callback == nil {
		callback = DefaultCallback
	}
	return &Evaluator{callback: callback}
}

// WithErrorCallback sets an error callback for reporting expression evaluation errors
func (c *Evaluator) WithErrorCallback(onError ErrorCallback) *Evaluator {
	c.onError = onError
	return c
}

func (c *Evaluator) calculateResult(valNode *ajson.Node) (interface{}, error) {
	if valNode == nil {
		return nil, nil
	}

	// Handle strings with {{expression}} pattern
	if valNode.IsString() {
		str, _ := valNode.GetString()
		return c.evaluateString(str)
	}

	// Recursively process objects
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
	}

	// Recursively process arrays
	if valNode.IsArray() {
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

	// Return primitives (numbers, booleans, null) as-is
	return valNode.Unpack()
}

// evaluateString processes a string that may contain {{expression}} patterns
func (c *Evaluator) evaluateString(str string) (interface{}, error) {
	// Check if the entire string is a single {{expression}}
	// In this case, return the actual type (number, bool, object, etc.)
	if matches := pureExprPattern.FindStringSubmatch(str); len(matches) == 2 {
		expr := strings.TrimSpace(matches[1])
		result, err := c.callback(expr)
		if err != nil {
			// Report error via callback if set
			if c.onError != nil {
				c.onError(expr, err)
			}
			// Return nil to allow downstream code to continue with missing data
			return nil, nil
		}
		return result, nil
	}

	// Check if string contains any {{expression}} patterns for interpolation
	if !exprPattern.MatchString(str) {
		// No expressions, return literal string
		return str, nil
	}

	// Interpolate: replace each {{expr}} with its evaluated string value
	result := exprPattern.ReplaceAllStringFunc(str, func(match string) string {
		// Extract expression from {{expr}}
		expr := strings.TrimSpace(match[2 : len(match)-2])
		val, err := c.callback(expr)
		if err != nil {
			// Report error via callback if set
			if c.onError != nil {
				c.onError(expr, err)
			}
			// Leave expression unevaluated so user can see what failed
			return match
		}
		if val == nil {
			return ""
		}
		return fmt.Sprintf("%v", val)
	})

	return result, nil
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
