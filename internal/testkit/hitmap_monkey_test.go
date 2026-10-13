package testkit

import (
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/activity"
)

// TestHitMapMonkey is the resize monkey: a seeded random walk of window
// resizes, sidebar / AI panel / bottom panel drags and toggles, zen mode
// and overlays. After every step the hit-map check (hitmap_test.go) runs.
//
//	TERMOCODE_MONKEY=long        go test ./internal/testkit -run TestHitMapMonkey   # 600 steps
//	TERMOCODE_MONKEY_STEPS=2000  …
//	TERMOCODE_MONKEY_SEED=1234   …   # replay
//	TERMOCODE_MONKEY_NVIM=1      …   with a real nvim (tabs, bottom panel)
//
// On failure the seed and every step are printed.
func TestHitMapMonkey(t *testing.T) {
	steps := envInt("TERMOCODE_MONKEY_STEPS", 60)
	if os.Getenv("TERMOCODE_MONKEY") == "long" && os.Getenv("TERMOCODE_MONKEY_STEPS") == "" {
		steps = 600
	}
	seed := envInt("TERMOCODE_MONKEY_SEED", time.Now().UnixNano())
	useNvim := os.Getenv("TERMOCODE_MONKEY_NVIM") == "1"
	rng := rand.New(rand.NewPCG(uint64(seed), 0x417))
	t.Logf("hit-map monkey: seed %d, %d steps (replay: TERMOCODE_MONKEY_SEED=%d)", seed, steps, seed)

	opt := Options{W: 120, H: 36, Git: true}
	if useNvim {
		opt = Options{W: 120, H: 36, Nvim: true, Files: scmFiles, Setup: func(tb testing.TB, dir string) {
			r := newRepo(tb, dir)
			r.commit("init")
			r.write("main.go", "package main\n\nfunc main() { println(\"new\") }\n")
		}}
	}
	d := New(t, opt)
	var walk []string
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("hit-map monkey FAILED: replay with TERMOCODE_MONKEY_SEED=%d TERMOCODE_MONKEY_STEPS=%d\nsteps:\n  %s",
				seed, steps, strings.Join(walk, "\n  "))
		}
	})
	if useNvim {
		for _, f := range []string{"README.md", "docs/guide.md", "main.go"} {
			open(d, f)
		}
	}

	type move struct {
		name string
		do   func()
	}
	moves := []move{
		{"resize", func() { d.Resize(60+rng.IntN(161), 20+rng.IntN(41)) }},
		{"resize", func() { d.Resize(60+rng.IntN(161), 20+rng.IntN(41)) }},
		{"toggle sidebar", func() { d.Palette("toggle-explorer") }},
		{"drag sidebar", func() { dragSidebar(d, rng.IntN(31)-15) }},
		{"toggle AI panel", func() { d.Key("alt+a", "esc") }},
		{"drag AI panel", func() {
			if _, y, ok := find(d, "AI CHAT"); ok && y == 0 {
				edge := aiPanelEdge(d)
				d.Drag(edge, 10, edge+rng.IntN(31)-15, 10)
			}
		}},
		{"zen", func() { d.Palette("zen-mode") }},
		{"view", func() {
			views := []activity.View{activity.ViewFiles, activity.ViewGit, activity.ViewRun, activity.ViewTests}
			d.Send(activity.SwitchMsg{View: views[rng.IntN(len(views))]})
		}},
		{"palette", func() { d.Key("f1") }},
		{"esc", func() { d.Key("esc") }},
	}
	if useNvim {
		moves = append(moves,
			move{"terminal panel", func() { d.Action("panel:terminal") }},
			move{"problems panel", func() { d.Action("panel:problems") }},
			move{"drag panel", func() { dragPanelBar(d, rng.IntN(13)-6) }},
			move{"toggle panel", func() { d.Palette("toggle-terminal") }},
		)
	}
	for i := int64(0); i < steps && !d.Failed(); i++ {
		mv := moves[rng.IntN(len(moves))]
		mv.do()
		d.Action("toasts:clear")
		d.Settle(20 * time.Millisecond)
		walk = append(walk, fmt.Sprintf("%d: %s → %dx%d", i, mv.name, d.W, d.H))
		if d.Quit() {
			d.quit = false
		}
		checkHits(t, d, fmt.Sprintf("step %d (%s)", i, mv.name))
	}
}
