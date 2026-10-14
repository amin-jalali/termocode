package testkit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/theme"
)

// K2 rows of TestAIToolsTable: settings_list / settings_set, layout_set
// and get_ide_state through the real app. A separate block so other
// Group K steps can add their rows without merge conflicts.
func init() {
	aiToolCases = append(aiToolCases, aiSettingsToolCases...)
}

// sidebarBgSeq is the SGR background of theme id's sidebar.
func sidebarBgSeq(id string) string {
	t, _ := theme.LookupTheme(id)
	c := theme.ToRGB(t.Palette.BgSidebar)
	return fmt.Sprintf("48;2;%d;%d;%d", c.R, c.G, c.B)
}

// configJSON reads the sandbox config.json.
func configJSON(t *testing.T) map[string]any {
	t.Helper()
	b, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "config.json"))
	out := map[string]any{}
	_ = json.Unmarshal(b, &out)
	return out
}

// explorerEdgeCol is the screen column of the explorer's right edge, -1
// when no row shows it.
func explorerEdgeCol(d *Driver) int {
	for _, line := range strings.Split(screen(d), "\n") {
		if i := strings.Index(line, "▕"); i >= 0 {
			return ansi.StringWidth(line[:i])
		}
	}
	return -1
}

var aiSettingsToolCases = []aiToolCase{
	{
		name: "settings_list shows settings with values and choices, never keys left out",
		call: toolCall{"settings_list", nil},
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], `"key":"theme"`) || !strings.Contains(r[0], "tokyo-night") ||
				!strings.Contains(r[0], `"key":"tab_size"`) {
				t.Errorf("results %q", r)
			}
			for _, never := range []string{"ai_provider", "ai_auto_approve", "credentials"} {
				if len(r) == 1 && strings.Contains(r[0], never) {
					t.Errorf("%s listed: %s", never, r[0])
				}
			}
		},
	},
	{
		name:    "settings_set theme asks, then repaints the IDE and saves",
		call:    toolCall{"settings_set", map[string]any{"key": "theme", "value": "tokyo-night"}},
		confirm: true,
		setup: func(d *Driver) {
			if !strings.Contains(d.Frame(), sidebarBgSeq("vscode-dark-plus")) {
				d.T.Fatalf("default sidebar colour not on screen")
			}
		},
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "theme = tokyo-night") {
				t.Errorf("results %q", r)
			}
			if !eventually(d, func() bool { return strings.Contains(d.Frame(), sidebarBgSeq("tokyo-night")) }) {
				t.Errorf("tokyo-night sidebar colour not on screen")
			}
			if strings.Contains(d.Frame(), sidebarBgSeq("vscode-dark-plus")) {
				t.Errorf("old sidebar colour still on screen")
			}
			want, _ := theme.LookupTheme("tokyo-night")
			bg := lua(d, `return string.format('#%06x', vim.api.nvim_get_hl(0, { name = 'Normal' }).bg or 0)`)
			if bg != theme.ToRGB(want.Palette.BgEditor).Hex() {
				t.Errorf("nvim Normal bg %s, want %s", bg, theme.ToRGB(want.Palette.BgEditor).Hex())
			}
			if configJSON(t)["theme"] != "tokyo-night" {
				t.Errorf("config.json: %v", configJSON(t))
			}
		},
	},
	{
		name:    "settings_set tab_size asks, then sets the buffer's tabstop and saves",
		call:    toolCall{"settings_set", map[string]any{"key": "tab_size", "value": 2}},
		confirm: true,
		check: func(t *testing.T, d *Driver, r []string) {
			if got := lua(d, `return vim.fn.fnamemodify(vim.api.nvim_buf_get_name(0), ':t') .. ':' .. vim.bo.tabstop .. ':' .. vim.bo.shiftwidth`); got != "a.txt:2:2" {
				t.Errorf("buffer options %q; results %q", got, r)
			}
			if configJSON(t)["tab_size"] != float64(2) {
				t.Errorf("config.json: %v", configJSON(t))
			}
		},
	},
	{
		name: "settings_set never key is refused without a dialog",
		call: toolCall{"settings_set", map[string]any{"key": "ai_auto_approve", "value": "run"}},
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "not for the AI") {
				t.Errorf("results %q", r)
			}
			if v := configJSON(t)["ai_auto_approve"]; strings.Contains(fmt.Sprint(v), "run") {
				t.Errorf("ai_auto_approve changed: %v", configJSON(t)["ai_auto_approve"])
			}
		},
	},
	{
		name: "layout_set sidebar_width resizes the sidebar on screen without asking",
		call: toolCall{"layout_set", map[string]any{"sidebar_width": 45}},
		check: func(t *testing.T, d *Driver, r []string) {
			if len(r) != 1 || !strings.Contains(r[0], "sidebar width 45") {
				t.Errorf("results %q", r)
			}
			// activity bar (4 cols) + sidebar 45 cols; the edge is the splitter column.
			if !eventually(d, func() bool { return explorerEdgeCol(d) == 4+45-1 }) {
				t.Errorf("explorer edge at col %d:\n%s", explorerEdgeCol(d), screen(d))
			}
		},
	},
	{
		name: "get_ide_state reads the IDE without asking",
		call: toolCall{"get_ide_state", nil},
		git:  true,
		check: func(t *testing.T, d *Driver, r []string) {
			var st map[string]any
			if len(r) != 1 || json.Unmarshal([]byte(r[0]), &st) != nil {
				t.Fatalf("results %q", r)
			}
			if st["theme"] != "vscode-dark-plus" || st["zen"] != false {
				t.Errorf("state %s", r[0])
			}
			if g, _ := st["git"].(map[string]any); g["branch"] == "" || g["branch"] == nil {
				t.Errorf("no git branch: %s", r[0])
			}
			if ap, _ := st["ai_panel"].(map[string]any); ap["visible"] != true {
				t.Errorf("ai panel not visible: %s", r[0])
			}
		},
	},
}
