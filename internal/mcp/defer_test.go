package mcp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

func deferFixture(n int) []ai.Tool {
	var tools []ai.Tool
	tools = append(tools, ai.Tool{Name: "read_file", Description: "built in"})
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("tool%d", i)
		desc := "does thing " + name
		if i == 3 {
			desc = "create an issue in the tracker"
		}
		tools = append(tools, ai.Tool{Name: WireName("linear", name), Display: DisplayName("linear", name),
			Description: "[MCP linear] " + desc,
			Schema:      map[string]any{"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string"}}}})
	}
	return tools
}

func shownNames(tools []ai.Tool) []string {
	var n []string
	for _, t := range tools {
		if !t.IsHidden() {
			n = append(n, t.Name)
		}
	}
	return n
}

func TestDeferToolsOffAndSmallAutoKeepAll(t *testing.T) {
	tools := deferFixture(3)
	if got := DeferTools(tools, ToolSearchOff); len(got) != 4 || len(shownNames(got)) != 4 {
		t.Fatalf("off: %v", shownNames(got))
	}
	if got := DeferTools(tools, ToolSearchAuto); len(shownNames(got)) != 4 {
		t.Fatalf("auto small: %v", shownNames(got))
	}
}

func TestDeferToolsHidesMCPToolsAndSearchLoadsThem(t *testing.T) {
	got := DeferTools(deferFixture(5), ToolSearchOn)
	shown := shownNames(got)
	if strings.Join(shown, ",") != "read_file,"+ToolSearchName {
		t.Fatalf("shown = %v", shown)
	}
	search, ok := ai.FindTool(got, ToolSearchName)
	if !ok || search.Mutating {
		t.Fatalf("search tool missing or mutating")
	}
	if !strings.Contains(search.Description, "linear (5)") {
		t.Fatalf("description should name the servers: %q", search.Description)
	}
	out, err := search.Run(context.Background(), map[string]any{"query": "issue tracker"})
	if err != nil || !strings.Contains(out, WireName("linear", "tool3")) || !strings.Contains(out, `"title"`) {
		t.Fatalf("search out = %q, %v", out, err)
	}
	if strings.Contains(out, WireName("linear", "tool1")+"\n") {
		t.Fatalf("an unmatched tool was returned: %q", out)
	}
	shown = shownNames(got)
	if strings.Join(shown, ",") != "read_file,"+WireName("linear", "tool3")+","+ToolSearchName {
		t.Fatalf("after search shown = %v", shown)
	}
	// select: loads exact names.
	if _, err := search.Run(context.Background(), map[string]any{"query": "select:" + WireName("linear", "tool0")}); err != nil {
		t.Fatal(err)
	}
	if len(shownNames(got)) != 4 {
		t.Fatalf("select did not load: %v", shownNames(got))
	}
	out, _ = search.Run(context.Background(), map[string]any{"query": "zzz-nothing"})
	if !strings.Contains(out, "No MCP tool matches") {
		t.Fatalf("no-match out = %q", out)
	}
}

func TestDeferToolsAutoDefersBigLists(t *testing.T) {
	got := DeferTools(deferFixture(400), ToolSearchAuto)
	if len(shownNames(got)) != 2 {
		t.Fatalf("auto big list was not deferred: %d shown", len(shownNames(got)))
	}
}

func TestParseToolSearchMode(t *testing.T) {
	for in, want := range map[string]string{"": ToolSearchAuto, "auto": ToolSearchAuto, "ON": ToolSearchOn, "off": ToolSearchOff, "bad": ToolSearchAuto} {
		if got := ParseToolSearchMode(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

func TestLoadSettingsToolSearch(t *testing.T) {
	p := t.TempDir() + "/config.json"
	if s := LoadSettings(p); s.ToolSearch != ToolSearchAuto {
		t.Fatalf("default = %q", s.ToolSearch)
	}
	if err := SaveSetting(p, "mcp_tool_search", "off"); err != nil {
		t.Fatal(err)
	}
	if s := LoadSettings(p); s.ToolSearch != ToolSearchOff {
		t.Fatalf("saved = %q", s.ToolSearch)
	}
}
