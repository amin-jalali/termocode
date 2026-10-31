package mcp

import (
	"encoding/json"
	"fmt"
)

// checkResultType refuses a result that is not complete.
func checkResultType(raw json.RawMessage) error {
	var r struct {
		ResultType string `json:"resultType"`
	}
	if json.Unmarshal(raw, &r) != nil {
		return nil // the caller's own decode reports it
	}
	switch r.ResultType {
	case "", "complete":
		return nil
	}
	return fmt.Errorf("the server answered with resultType %q, which termocode does not support yet", r.ResultType)
}

// decodeCallResult decodes a tools/call result.
func decodeCallResult(raw json.RawMessage) (CallResult, error) {
	if err := checkResultType(raw); err != nil {
		return CallResult{}, fmt.Errorf("tools/call: %w", err)
	}
	var res CallResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return CallResult{}, fmt.Errorf("tools/call: bad answer: %w", err)
	}
	return res, nil
}
