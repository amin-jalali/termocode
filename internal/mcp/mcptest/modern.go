package mcptest

import (
	"encoding/json"
	"os"

	"github.com/amin-jalali/termocode/internal/extproc"
)

// The stateless 2026-07-28 revision. FAKE_MODERN=1 makes the stdio fake
// dual-era: it answers server/discover, serves a request that carries
// the revision in its _meta statelessly, and still answers initialize.
// FAKE_MODERN=only refuses initialize (a modern-only server).

// ModernVersion is the stateless revision.
const ModernVersion = "2026-07-28"

// _meta keys of a stateless request.
const (
	MetaVersion = "io.modelcontextprotocol/protocolVersion"
	MetaClient  = "io.modelcontextprotocol/clientInfo"
	MetaCaps    = "io.modelcontextprotocol/clientCapabilities"
)

// codeUnsupportedVersion is UnsupportedProtocolVersionError.
const codeUnsupportedVersion = -32022

func modernMode() string { return os.Getenv("FAKE_MODERN") }

// RequestMeta reads the revision and client name of a request's _meta.
func RequestMeta(params json.RawMessage) (version, client string) {
	var p struct {
		Meta struct {
			Version string `json:"io.modelcontextprotocol/protocolVersion"`
			Client  struct {
				Name string `json:"name"`
			} `json:"io.modelcontextprotocol/clientInfo"`
		} `json:"_meta"`
	}
	_ = json.Unmarshal(params, &p)
	return p.Meta.Version, p.Meta.Client.Name
}

// DiscoverResult is the fake's server/discover answer.
func DiscoverResult() map[string]any {
	versions := []string{ModernVersion}
	if modernMode() != "only" {
		versions = append(versions, supported...)
	}
	return map[string]any{"resultType": "complete", "supportedVersions": versions,
		"capabilities": map[string]any{"tools": map[string]any{"listChanged": true},
			"resources": map[string]any{"listChanged": true}, "prompts": map[string]any{"listChanged": true}, "completions": map[string]any{}},
		"_meta": map[string]any{"io.modelcontextprotocol/serverInfo": map[string]any{"name": "fake-mcp-modern", "version": "2.0.0"}},
		"ttlMs": 60000, "cacheScope": "public"}
}

// unsupportedVersion is the error for a request without the revision.
func unsupportedVersion(requested string) *extproc.Error {
	data, _ := json.Marshal(map[string]any{"supported": []string{ModernVersion}, "requested": requested})
	return &extproc.Error{Code: codeUnsupportedVersion, Message: "Unsupported protocol version", Data: data}
}
