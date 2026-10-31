package mcp

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (MCPX-20): a result with resultType other than "complete" is a
// clear error, not an empty answer; _meta of a result and a tool is kept.
func TestResultTypeAndMeta(t *testing.T) {
	if _, err := decodeCallResult(json.RawMessage(`{"resultType":"incomplete","inputRequests":{}}`)); err == nil ||
		!strings.Contains(err.Error(), `resultType "incomplete"`) {
		t.Fatalf("err %v", err)
	}
	r, err := decodeCallResult(json.RawMessage(`{"resultType":"complete","content":[{"type":"text","text":"ok"}],"_meta":{"k":"v"}}`))
	if err != nil || r.Text() != "ok" || r.Meta["k"] != "v" {
		t.Fatalf("%+v %v", r, err)
	}
	var tl Tool
	if err := json.Unmarshal([]byte(`{"name":"x","_meta":{"ui":"y"}}`), &tl); err != nil || tl.Meta["ui"] != "y" {
		t.Fatalf("%+v %v", tl, err)
	}
}
