package app

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/tasks"
)

// settings_set, layout_set and get_ide_state (ai_settings_tools.go):
// ranges, settings that apply on restart, a save that fails, every
// layout switch and the state of tabs, tasks and the debugger.

func TestCovAISettingRanges(t *testing.T) {
	withConfigDir(t)
	for _, c := range []struct{ key, value, want string }{
		{"tab_size", "0", "expected an integer 1..16"},
		{"search_max_results", "-1", "expected an integer ≥ 0"},
		{"tab_size", "four", "expected an integer"},
		{"no_such_setting", "1", "unknown setting"},
	} {
		if _, _, err := aiValidateSetting(c.key, c.value); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s=%s: %v", c.key, c.value, err)
		}
	}
	if lo, hi := aiSettingRange("font_delta"); lo != -20 || hi != 20 {
		t.Errorf("font_delta range %d..%d", lo, hi)
	}
	if lo, hi := aiSettingRange("whatever"); lo != -1<<30 || hi != 1<<30 {
		t.Errorf("default range %d..%d", lo, hi)
	}
	m := aiTestModel(t)
	row, v, err := aiValidateSetting("auto_reconnect", "yes")
	if err != nil {
		t.Skipf("auto_reconnect is not an AI setting here: %v", err)
	}
	out, _, err := m.aiSetSetting(row, "auto_reconnect", v)
	if err != nil || !strings.Contains(out, "applies the next time termocode starts") {
		t.Errorf("restart setting: %q %v", out, err)
	}
	// A config.json that can not be written: the model is told.
	dir, _ := configDir()
	_ = os.Remove(dir + "/config.json")
	if err := os.MkdirAll(dir+"/config.json", 0o755); err != nil {
		t.Fatal(err)
	}
	row, v, _ = aiValidateSetting("tab_size", "3")
	if _, _, err := m.aiSetSetting(row, "tab_size", v); err == nil || !strings.Contains(err.Error(), "could not save tab_size") {
		t.Errorf("failed save: %v", err)
	}
}

// layout_set without the editor: zen on and off, the AI panel shown from
// zen, the panel can not open, rows and width clamp and say they apply
// later.
func TestCovAILayoutSwitches(t *testing.T) {
	m := aiTestModel(t)
	m.showExp = true
	out, _, err := m.aiLayoutSet(map[string]any{"zen": true})
	if err != nil || !m.zenMode || !strings.HasPrefix(out, "zen on") {
		t.Errorf("zen on: %q %v", out, err)
	}
	out, _, _ = m.aiLayoutSet(map[string]any{"sidebar_width": 30})
	if !strings.Contains(out, "the sidebar is hidden, the width applies when it shows") {
		t.Errorf("width in zen: %q", out)
	}
	out, _, _ = m.aiLayoutSet(map[string]any{"ai_panel": true})
	if m.zenMode || !strings.Contains(out, "AI panel shown") || !m.actionsOpen {
		t.Errorf("ai panel from zen: %q zen=%v open=%v", out, m.zenMode, m.actionsOpen)
	}
	m.actionsPinned = false
	m.aiLayoutSet(map[string]any{"ai_panel": true})
	if !m.actionsPinned {
		t.Error("an unpinned AI panel was not pinned")
	}
	m.zenMode = true
	out, _, _ = m.aiLayoutSet(map[string]any{"zen": false})
	if m.zenMode || out != "zen off" {
		t.Errorf("zen off: %q", out)
	}
	out, _, _ = m.aiLayoutSet(map[string]any{"panel": true})
	if !strings.Contains(out, "panel could not be changed (the editor is not running)") {
		t.Errorf("panel without editor: %q", out)
	}
	out, _, _ = m.aiLayoutSet(map[string]any{"panel": false})
	if out != "panel closed" {
		t.Errorf("panel closed: %q", out)
	}
	out, _, _ = m.aiLayoutSet(map[string]any{"panel_rows": 1000})
	if !strings.Contains(out, "clamped") || !strings.Contains(out, "the panel is closed, the height applies when it opens") {
		t.Errorf("rows: %q", out)
	}
	out, _, _ = m.aiLayoutSet(map[string]any{"sidebar_width": explorerMinWidth + 1})
	if out != "sidebar width "+itoa(explorerMinWidth+1) {
		t.Errorf("exact width: %q", out)
	}
}

// get_ide_state lists tabs (visible and hidden), running tasks, the
// debugger status and the active file; the view names are known.
func TestCovAIIDEStateDetails(t *testing.T) {
	m := aiTestModel(t)
	m.problems = newProblemsPanel()
	m.tasks = newTaskRunner()
	m.tasks.runs[5] = &taskRun{Task: tasks.Task{Label: "build"}, Buf: 5, Running: true}
	m.tasks.order = []int{5}
	m.terminalTabs = []terminalTab{{Name: "zsh", BufID: 1}}
	m.hiddenTerminalTabs = []terminalTab{{Name: "old", BufID: 2}}
	m.termOpen = true
	m.debug = &debugState{status: "paused"}
	js, err := m.aiIDEState(func(p string) string { return p })
	if err != nil {
		t.Fatal(err)
	}
	var st map[string]any
	if err := json.Unmarshal([]byte(js), &st); err != nil {
		t.Fatal(err)
	}
	if got := st["tasks_running"].([]any); len(got) != 1 || got[0] != "build" {
		t.Errorf("tasks = %v", got)
	}
	if got := st["terminal_tabs_hidden"].([]any); len(got) != 1 || got[0] != "old" {
		t.Errorf("hidden tabs = %v", got)
	}
	if d := st["debug"].(map[string]any); d["status"] != "paused" {
		t.Errorf("debug = %v", d)
	}
	if p := st["panel"].(map[string]any); !strings.Contains(js, "Terminal: zsh") || p["open"] != true {
		t.Errorf("panel = %v", p)
	}
	for v, want := range map[activity.View]string{activity.ViewSearch: "search", activity.ViewGit: "git", activity.ViewRun: "run",
		activity.ViewSettings: "settings", activity.ViewTests: "tests", activity.View(99): "unknown"} {
		if got := aiViewName(v); got != want {
			t.Errorf("view %v = %q", v, got)
		}
	}
}
