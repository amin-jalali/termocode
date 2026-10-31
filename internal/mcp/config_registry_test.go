package mcp

import "testing"

// A catalog server's "registry" key (catalog.go) is not a config error:
// the entry parses and keeps its pinned version.
func TestParseFileAcceptsRegistryKey(t *testing.T) {
	b := []byte(`{"mcpServers":{"fs":{"command":"npx","args":["-y","@x/fs@1.2.0"],"registry":{"name":"io.github.x/fs","version":"1.2.0"}},
		"web":{"type":"http","url":"https://example.com/mcp","registry":{"name":"com.example/web","version":"2.0.0"}}}}`)
	list, warns, err := ParseFile(b, SourceUser, "/tmp/mcp.json")
	if err != nil || len(warns) > 0 {
		t.Fatalf("err=%v warns=%v", err, warns)
	}
	if len(list) != 2 {
		t.Fatalf("got %d servers", len(list))
	}
	for _, s := range list {
		if s.Problem != "" {
			t.Errorf("%s: %s", s.Name, s.Problem)
		}
		if r, ok := RegistryOf(s); !ok || r.Version == "" {
			t.Errorf("%s: registry not read: %+v", s.Name, r)
		}
	}
}
