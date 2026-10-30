package app

import (
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
)

// The words the MCP server cards and rows use.
func TestCovSvcMCPTexts(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0: "0", 90 * time.Second: "90s", 2 * time.Hour: "2h", 10 * time.Minute: "10m",
		150 * time.Second: "150s", 1500 * time.Millisecond: "1.5s",
	} {
		if got := mcpDurationText(d); got != want {
			t.Errorf("mcpDurationText(%v) = %q, want %q", d, got, want)
		}
	}
	for key, want := range map[string]string{
		"execute_action:format-document":  "the action format-document",
		"http_send:api.example.com/write": "http_send with any method to api.example.com",
		"http_send:api.example.com":       "http_send GET / HEAD / OPTIONS to api.example.com",
	} {
		if got := mcpServeAlwaysWhat(key); got != want {
			t.Errorf("what(%q) = %q, want %q", key, got, want)
		}
	}
	for key, want := range map[string]string{
		"execute_action:x":          "action x",
		"http_send:h.example/write": "http_send h.example",
		"tasks_run":                 "tasks_run",
	} {
		if got := mcpServeAllowRowName(key); got != want {
			t.Errorf("row(%q) = %q, want %q", key, got, want)
		}
	}
	for args, want := range map[string]string{"": "", "a:b": "", "app": "db_query:app"} {
		if got := mcpServeAlwaysKey(ai.Tool{Name: "db_query", Class: "db"}, map[string]any{"conn": args}); got != want {
			t.Errorf("db key(%q) = %q, want %q", args, got, want)
		}
	}
	if mcpServeAlwaysKey(ai.Tool{Name: "execute_action", Class: "x"}, map[string]any{}) != "" ||
		mcpServeAlwaysKey(ai.Tool{Name: "execute_action", Class: "x"}, map[string]any{"id": "a"}) != "execute_action:a" ||
		mcpServeAlwaysKey(ai.Tool{Name: "debug_evaluate", Class: "x"}, nil) != "" ||
		mcpServeAlwaysKey(ai.Tool{Name: "tasks_run"}, nil) != "" {
		t.Error("always keys")
	}
	for raw, want := range map[string]string{
		"http://[::1]:8080/x":          "http://[::1]:8080",
		"https://API.example.com:443/": "https://api.example.com",
		"http://h:80/":                 "http://h",
		"ftp://h/":                     "",
		"%zz":                          "",
	} {
		if got := mcpServeHTTPOrigin(raw); got != want {
			t.Errorf("origin(%q) = %q, want %q", raw, got, want)
		}
	}
	if mcpServeHTTPAlwaysKey("http:") != "" || mcpServeHTTPAlwaysKey("x") != "" || mcpServeHTTPAlwaysKey("http:h") != "http_send:h" {
		t.Error("http always key")
	}
	if hostOrRaw("", "raw") != "raw" || hostOrRaw("h", "raw") != "h" {
		t.Error("hostOrRaw")
	}
	// Agent call rows of other shapes are skipped.
	for _, line := range []string{"x", "09:41:12  tasks_run  no gate", "09:41:12  tasks_run  gate=auto  by=user"} {
		if _, ok := mcpServeCallRow(line); ok {
			t.Errorf("row for %q", line)
		}
	}
	m := &Model{}
	if _, ok := m.onMCPServeAllowRow("other:1"); ok {
		t.Error("a foreign row was taken")
	}
	// Unknown services: the cards and actions do nothing.
	if m.openMCPRemove("covsvc-nope") != nil || m.openMCPDisable("covsvc-nope") != nil || m.mcpSetDisabled("covsvc-nope", true) != nil || m.mcpRemove("covsvc-nope") != nil {
		t.Error("an unknown service did something")
	}
	if m.answerMCPDisable("keep") != nil || m.answerMCPRemove("keep") != nil {
		t.Error("Keep did something")
	}
}
