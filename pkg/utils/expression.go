package utils

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"github.com/tiny-systems/ajson"
	"github.com/tiny-systems/module/pkg/evaluator"
)

// RunExpressionRequest contains the input for expression evaluation
type RunExpressionRequest struct {
	// Expression is the JSONPath expression to evaluate (e.g., "$.decoded", "$.response.body")
	Expression string
	// Data is the JSON data to evaluate the expression against
	Data string
	// Schema is the JSON schema to validate the result against
	Schema string
}

// RunExpressionResponse contains the result of expression evaluation
type RunExpressionResponse struct {
	// Result is the JSON-encoded result of the expression evaluation
	Result string
	// ValidSchema indicates whether the result validates against the schema
	ValidSchema bool
	// ValidationError contains the validation error message if validation failed
	ValidationError string
}

// RunExpression evaluates a JSONPath expression against JSON data and validates the result against a schema.
// It uses the ajson library for JSONPath evaluation and jsonschema for validation.
func RunExpression(req *RunExpressionRequest) (*RunExpressionResponse, error) {
	if req.Expression == "" {
		return nil, errors.New("expression is required")
	}
	if req.Data == "" {
		return nil, errors.New("data is required")
	}

	// Parse the JSON data
	node, err := ajson.Unmarshal([]byte(req.Data))
	if err != nil {
		return nil, fmt.Errorf("failed to parse data: %w", err)
	}

	// Evaluate the JSONPath expression
	jsonPathResult, err := ajson.Eval(node, req.Expression)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate expression: %w", err)
	}

	// Unpack the result for schema validation
	v, err := jsonPathResult.Unpack()
	if err != nil {
		return nil, fmt.Errorf("failed to unpack result: %w", err)
	}

	// Marshal the result back to JSON
	result, err := ajson.Marshal(jsonPathResult)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	resp := &RunExpressionResponse{
		ValidSchema: true,
		Result:      string(result),
	}

	// If schema is provided, validate the result against it
	if req.Schema != "" && req.Schema != "{}" {
		compiler := jsonschema.NewCompiler()
		compiler.Draft = jsonschema.Draft7

		err = compiler.AddResource("schema.json", bytes.NewReader([]byte(req.Schema)))
		if err != nil {
			return nil, fmt.Errorf("failed to add schema resource: %w", err)
		}

		sch, err := compiler.Compile("schema.json")
		if err != nil {
			return nil, fmt.Errorf("failed to compile schema: %w", err)
		}

		if err = sch.Validate(v); err != nil {
			resp.ValidSchema = false
			var validationErr *jsonschema.ValidationError
			if errors.As(err, &validationErr) {
				// Get the deepest validation error for a more specific message
				leaf := validationErr
				for len(leaf.Causes) > 0 {
					leaf = leaf.Causes[0]
				}
				resp.ValidationError = fmt.Sprintf("%s %s", leaf.KeywordLocation, leaf.Message)
			} else {
				resp.ValidationError = err.Error()
			}
		}
	}

	return resp, nil
}

// PreviewEdgeMappingRequest contains the input for edge mapping preview
type PreviewEdgeMappingRequest struct {
	// Configuration is the edge configuration JSON (may contain {{expression}} patterns)
	Configuration string
	// SourceData is the source port data to evaluate expressions against
	SourceData string
}

// PreviewEdgeMappingResponse contains the result of edge mapping preview
type PreviewEdgeMappingResponse struct {
	// Result is the JSON-encoded result after evaluating all expressions
	Result string
	// Errors contains any expression evaluation errors that occurred
	Errors []string
}

// PreviewEdgeMapping evaluates an edge configuration against source data.
// It processes all {{expression}} patterns in the configuration and returns the transformed result.
func PreviewEdgeMapping(req *PreviewEdgeMappingRequest) (*PreviewEdgeMappingResponse, error) {
	if req.Configuration == "" {
		return &PreviewEdgeMappingResponse{Result: "{}"}, nil
	}

	// Parse source data for JSONPath evaluation
	var sourceNode *ajson.Node
	var parseErr error
	if req.SourceData != "" {
		sourceNode, parseErr = ajson.Unmarshal([]byte(req.SourceData))
		if parseErr != nil {
			return nil, fmt.Errorf("failed to parse source data: %w", parseErr)
		}
	}

	var evalErrors []string

	// Create evaluator with callback that evaluates JSONPath expressions
	eval := evaluator.NewEvaluator(func(expression string) (interface{}, error) {
		if sourceNode == nil {
			return nil, fmt.Errorf("no source data available")
		}

		// Evaluate JSONPath expression
		result, err := ajson.Eval(sourceNode, expression)
		if err != nil {
			return nil, err
		}

		return result.Unpack()
	}).WithErrorCallback(func(expression string, err error) {
		evalErrors = append(evalErrors, fmt.Sprintf("%s: %s", expression, err.Error()))
	})

	// Evaluate the configuration
	result, err := eval.Eval([]byte(req.Configuration))
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate configuration: %w", err)
	}

	// Marshal result to JSON
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal result: %w", err)
	}

	return &PreviewEdgeMappingResponse{
		Result: string(resultJSON),
		Errors: evalErrors,
	}, nil
}
