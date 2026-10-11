package testkit

import (
	"fmt"
	"os"
	"sort"
	"testing"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/theme"
)

// skipCommands are palette ids the walk never runs: they reach the
// network or start external tools / debuggers.
var skipCommands = map[string]string{
	"git-push":             "network",
	"git-pull":             "network",
	"git-fetch":            "network",
	"git-sync":             "network",
	"git-clone":            "network",
	"ai-sign-in-claude":    "network (OAuth in a browser)",
	"dap-install":          "downloads a debug adapter",
	"debug-start":          "starts a debugger",
	"debug-run-nodebug":    "starts a debugger",
	"debug-restart":        "starts a debugger",
	"testing-debug-cursor": "starts a debugger",
}

// walkCommands returns the palette ids to walk, sorted.
func walkCommands() []string {
	var ids []string
	for _, e := range app.PaletteCatalog() {
		if _, skip := skipCommands[e.ID]; !skip {
			ids = append(ids, e.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

// resetUI closes whatever a command opened (pickers, prompts, menus).
func resetUI(d *Driver) {
	d.Key("esc", "esc")
}

// runEach runs step for every item on a driver from mk, rebuilding the
// driver every `every` items so state does not pile up too much.
func runEach(t *testing.T, items []string, every int, mk func() *Driver, step func(d *Driver, item string)) {
	t.Helper()
	var d *Driver
	for i, it := range items {
		if i%every == 0 || d.Quit() {
			d = mk()
		}
		step(d, it)
		resetUI(d)
		if d.Failed() {
			t.Fatalf("stopped after %q", it)
		}
	}
}

// TestWalkPaletteCommands runs every command-palette entry from the
// dashboard (no nvim), opens what it opens, moves once and closes it.
func TestWalkPaletteCommands(t *testing.T) {
	cmds := walkCommands()
	if len(cmds) < 100 {
		t.Fatalf("only %d palette commands", len(cmds))
	}
	runEach(t, cmds, 40, func() *Driver { return New(t, Options{W: 120, H: 36, Git: true}) },
		func(d *Driver, id string) {
			d.Palette(id)
			d.Key("down")
		})
}

// fullWalk reports whether TERMOCODE_WALK=full asks for the slow, complete
// nvim walks (every command / every size). By default the nvim walks run
// on a sample so `go test ./...` stays fast.
func fullWalk() bool { return os.Getenv("TERMOCODE_WALK") == "full" }

// sample returns every n-th item (all of them with TERMOCODE_WALK=full).
func sample(items []string, n int) []string {
	if fullWalk() {
		return items
	}
	var out []string
	for i := 0; i < len(items); i += n {
		out = append(out, items[i])
	}
	return out
}

// TestWalkPaletteCommandsNvim runs palette entries with a real nvim and a
// file open: a sample of them, or all with TERMOCODE_WALK=full.
func TestWalkPaletteCommandsNvim(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real nvim")
	}
	runEach(t, sample(walkCommands(), 10), 50, func() *Driver {
		d := New(t, Options{W: 120, H: 36, Git: true, Nvim: true})
		d.Action("open:main.go")
		return d
	}, func(d *Driver, id string) {
		d.Palette(id)
		d.Key("down")
		resetUI(d)
		// Back to a file for the next command (some commands close it).
		d.Action("open:main.go")
	})
}

// TestWalkKeyBindings presses every default key binding, from the
// dashboard (no nvim).
func TestWalkKeyBindings(t *testing.T) {
	skipActions := map[keymap.Action]bool{}
	for id := range skipCommands {
		if a, ok := app.PaletteActionFor(id); ok {
			skipActions[a] = true
		}
	}
	var keys []string
	for k, a := range keymap.Default().Bindings() {
		if !skipActions[a] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if len(keys) < 30 {
		t.Fatalf("only %d key bindings", len(keys))
	}
	runEach(t, keys, 40, func() *Driver { return New(t, Options{W: 120, H: 36, Git: true}) },
		func(d *Driver, k string) {
			d.Key(k)
		})
}

// settingValues returns the values the walk tries for a setting, valid
// and invalid ones.
func settingValues(s app.SettingEntry) []string {
	switch {
	case len(s.Choices) > 0:
		return append(append([]string{}, s.Choices...), "bogus")
	case s.Key == "theme":
		var ids []string
		for _, th := range theme.AvailableThemes() {
			ids = append(ids, th.ID)
		}
		return append(ids, "no-such-theme")
	case s.Key == "ai_provider":
		return append(append([]string{}, ai.Kinds...), "none", "", "bogus")
	case s.Type == "bool":
		return []string{"true", "false", "maybe", "false"}
	case s.Type == "int":
		return []string{"-1", "0", "1", "16", "99999", "x", s.Default}
	default:
		return []string{"x界", "", s.Default}
	}
}

// TestWalkSettings sets every Settings row through its values, at a wide
// and a narrow size.
func TestWalkSettings(t *testing.T) {
	for _, size := range [][2]int{{120, 36}, {60, 20}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			d := New(t, Options{W: size[0], H: size[1], Git: true})
			n := 0
			for _, s := range app.SettingsCatalog() {
				if s.ID == "" {
					continue // file-only key
				}
				for _, v := range settingValues(s) {
					d.Action("setting:" + s.ID + "=" + v)
					resetUI(d)
					n++
				}
				d.Palette("open-settings-ui")
				d.Key("down", "esc")
			}
			if n < 30 {
				t.Errorf("only %d setting values tried", n)
			}
		})
	}
}

// TestWalkViewsAndPanels opens every activity-bar view and every bottom
// panel kind, at a wide and a narrow size, with and without nvim.
func TestWalkViewsAndPanels(t *testing.T) {
	views := []activity.View{activity.ViewFiles, activity.ViewSearch, activity.ViewGit,
		activity.ViewRun, activity.ViewTests, activity.ViewSettings}
	panels := []string{"terminal", "problems", "output", "tests", "debug-console"}
	for _, nv := range []bool{false, true} {
		for _, size := range [][2]int{{120, 36}, {60, 20}} {
			name := fmt.Sprintf("%dx%d/nvim=%v", size[0], size[1], nv)
			t.Run(name, func(t *testing.T) {
				if nv && (testing.Short() || !fullWalk() && size[0] < 100) {
					t.Skip("slow: real nvim (TERMOCODE_WALK=full runs it)")
				}
				d := New(t, Options{W: size[0], H: size[1], Git: true, Nvim: nv})
				if nv {
					d.Action("open:main.go")
				}
				for _, v := range views {
					d.Send(activity.SwitchMsg{View: v})
					resetUI(d)
					// Click every row of the sidebar once.
					for y := 0; y < d.H; y += 6 {
						d.Click(6, y)
						resetUI(d)
					}
				}
				d.Send(activity.ToggleSidebarMsg{})
				d.Send(activity.ToggleSidebarMsg{})
				// The activity bar itself.
				step := 1
				if nv && !fullWalk() {
					step = 3 // the icons are 3 rows apart; SwitchMsg above covers the views
				}
				for y := 0; y < d.H; y += step {
					d.Click(0, y)
					resetUI(d)
				}
				for _, p := range panels {
					d.Action("panel:" + p)
					d.Resize(d.W, d.H) // re-layout with the panel open
				}
			})
		}
	}
}
