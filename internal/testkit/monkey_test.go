package testkit

import (
	"fmt"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/keymap"
)

// envInt reads an int env var, or def.
func envInt(name string, def int64) int64 {
	if v, err := strconv.ParseInt(os.Getenv(name), 10, 64); err == nil {
		return v
	}
	return def
}

// monkeyKeys are the named keys the monkey presses: every named key
// except those bound to commands the walk skips (network, debuggers).
func monkeyKeys() []string {
	skip := map[string]bool{}
	for id := range skipCommands {
		if a, ok := app.PaletteActionFor(id); ok {
			for _, k := range keymap.Default().KeysFor(a) {
				skip[k] = true
			}
		}
	}
	var out []string
	for _, k := range KeyNames() {
		if !skip[k] {
			out = append(out, k)
		}
	}
	return out
}

// TestMonkey sends a seeded random mix of keys (with ctrl / alt /
// function keys), mouse moves, clicks, wheel and drags all over the
// screen (biased to the Source Control graph), resizes between 60×20 and
// 220×60 and palette commands, checking the frame after every step.
//
//	TERMOCODE_MONKEY_STEPS=5000 go test ./internal/testkit -run TestMonkey
//	TERMOCODE_MONKEY_SEED=1234  go test ./internal/testkit -run TestMonkey   # replay
//	TERMOCODE_MONKEY_NVIM=1     … with a real embedded nvim
//
// On failure the seed and every step are printed.
func TestMonkey(t *testing.T) {
	fakeEngine(t) // it may open a container view
	steps := envInt("TERMOCODE_MONKEY_STEPS", 300)
	seed := envInt("TERMOCODE_MONKEY_SEED", time.Now().UnixNano())
	useNvim := os.Getenv("TERMOCODE_MONKEY_NVIM") == "1"
	rng := rand.New(rand.NewPCG(uint64(seed), 0x7e57))
	t.Logf("monkey: seed %d, %d steps (replay: TERMOCODE_MONKEY_SEED=%d)", seed, steps, seed)

	d := New(t, Options{W: 120, H: 36, Git: true, Nvim: useNvim})
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("monkey FAILED: replay with TERMOCODE_MONKEY_SEED=%d TERMOCODE_MONKEY_STEPS=%d\nsteps:\n  %s",
				seed, steps, strings.Join(d.History(), "\n  "))
		}
	})
	if useNvim {
		d.Action("open:main.go")
	}
	keys := monkeyKeys()
	cmds := walkCommands()
	runes := []rune("aqzé界 /.:-_1é0jkx")
	views := []activity.View{activity.ViewFiles, activity.ViewSearch, activity.ViewGit, activity.ViewRun, activity.ViewTests}
	pt := func() (int, int) { return rng.IntN(d.W), rng.IntN(d.H) }
	// sidebarPt is a point over the sidebar (Source Control graph rows
	// when that view is open).
	sidebarPt := func() (int, int) {
		return activity.Width + rng.IntN(max(1, min(40, d.W-activity.Width))), rng.IntN(d.H)
	}

	for i := int64(0); i < steps && !d.Failed(); i++ {
		switch r := rng.IntN(100); {
		case r < 25:
			d.Key(keys[rng.IntN(len(keys))])
		case r < 35:
			k := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{runes[rng.IntN(len(runes))]}, Alt: rng.IntN(4) == 0}
			d.Send(k)
		case r < 50:
			d.Move(pt())
		case r < 60:
			d.Move(sidebarPt())
		case r < 68:
			d.Click(pt())
		case r < 71:
			d.RightClick(pt())
		case r < 77:
			x, y := pt()
			d.Wheel(x, y, rng.IntN(2) == 0)
		case r < 81:
			x0, y0 := pt()
			x1, y1 := pt()
			d.Drag(x0, y0, x1, y1)
		case r < 85:
			d.Resize(60+rng.IntN(161), 20+rng.IntN(41))
		case r < 90:
			d.Send(activity.SwitchMsg{View: views[rng.IntN(len(views))]})
		case r < 93:
			d.Action("view:git")
			for j := 0; j < 4; j++ {
				d.Move(sidebarPt())
			}
		case r < 97:
			d.Palette(cmds[rng.IntN(len(cmds))])
		default:
			d.Key("esc")
		}
		if d.Quit() {
			d.quit = false // keep going on the same model
		}
	}
}

// Regression (7421632): hovering commits in the Source Control graph put
// a hover card over the frame that cut a multi-byte glyph in half — the
// row had broken UTF-8, was too wide, wrapped and the screen scrolled.
// Commit subjects here have wide and accented glyphs.
func TestGitGraphHoverKeepsFrame(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 36}, {220, 60}, {60, 20}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			d := New(t, Options{W: size[0], H: size[1], Git: true})
			d.Action("view:git")
			d.WaitForText("changelog", 3*time.Second)
			sawCard := false
			for y := 0; y < d.H && !d.Failed(); y++ {
				for _, x := range []int{activity.Width + 2, activity.Width + 16} {
					d.Move(x, y)
					d.Settle(30 * time.Millisecond) // commit detail loads async
					if strings.Contains(ansi.Strip(d.Frame()), " ago ") {
						sawCard = true
					}
				}
			}
			if !sawCard && !d.Failed() && d.W >= 80 { // narrower: no room for the card
				t.Errorf("no commit hover card appeared:\n%s", ansi.Strip(d.Frame()))
			}
		})
	}
}
