package mcp

import (
	"runtime"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// "headersCommand" (your own mcp.json only) prints a JSON object of
// headers at each connect; they replace static headers of the same
// name, and their values are redacted from then on.
func TestHeadersCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	tok := "sk-" + "test-headers-cmd-0001"
	body := `{"mcpServers": {"r": {"url": "https://x.example/mcp", "headers": {"Authorization": "old", "X-A": "a"},
	  "headersCommand": "printf '{\"Authorization\": \"Bearer ` + tok + `\"}'"}}}`
	list, _, err := ParseFile([]byte(body), SourceUser, "/u/mcp.json")
	if err != nil || len(list) != 1 || list[0].Problem != "" {
		t.Fatalf("parse: %v %+v", err, list)
	}
	got, err := list[0].Expand(Expander{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if got.Headers["Authorization"] != "Bearer "+tok || got.Headers["X-A"] != "a" {
		t.Errorf("headers = %v", got.Headers)
	}
	if strings.Contains(ai.Redact("log "+tok), tok) {
		t.Error("the command's header value is not redacted")
	}
}

// A failing or non-JSON command stops the start; the error never
// holds what the command printed.
func TestHeadersCommandFailureHidesOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	for _, line := range []string{"echo leaked-value-123; exit 3", "echo leaked-value-123"} {
		s := Server{Name: "r", Type: TypeHTTP, URL: "https://x.example/mcp", HeadersCommand: line, Source: SourceUser}
		_, err := s.Expand(Expander{})
		if err == nil || strings.Contains(err.Error(), "leaked-value-123") {
			t.Errorf("%q: err = %v, want an error without the output", line, err)
		}
	}
}

// A project file can not run a headers command.
func TestHeadersCommandRefusedInProject(t *testing.T) {
	body := `{"mcpServers": {"r": {"url": "https://x.example/mcp", "headersCommand": "cat ~/.token"}}}`
	list, warns, _ := ParseFile([]byte(body), SourceProject, "/p/.mcp.json")
	if len(list) != 1 || !strings.Contains(list[0].Problem, "headersCommand") || len(warns) != 1 {
		t.Errorf("got %+v warns %q", list, warns)
	}
	// Expand refuses it too, whatever the Problem says.
	s := list[0]
	s.Problem = ""
	if _, err := s.Expand(Expander{}); err == nil {
		t.Error("a project entry ran its headersCommand")
	}
}
