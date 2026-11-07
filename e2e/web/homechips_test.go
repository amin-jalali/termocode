//go:build browser && webdev

package web

import (
	"fmt"
	"strings"
	"testing"
)

// The Home's context chips (MOBUX-3), on the component kit's chip row at
// 390 px with touch: "Needs you" first while something waits, up to 3
// pins next, the state's chips after, one line that scrolls sideways.

// chipRow is the first chip row of the kit (the dark frame).
const chipRow = `document.querySelector("#chips .hchips")`

// chipIDs lists the row's chips in drawn order (data-chip ids).
func chipIDs(p *Page) []string {
	p.T.Helper()
	return Eval[[]string](p, `[...`+chipRow+`.querySelectorAll("[data-chip]")].map((b) => b.dataset.chip)`)
}

// chipByID is a JavaScript expression for the first row's chip id.
func chipByID(id string) string {
	return fmt.Sprintf(`%s.querySelector('[data-chip=%q]')`, chipRow, id)
}

func openChips(t *testing.T) *Page {
	t.Helper()
	p := openKit(t, Phone)
	p.WaitJS(chipRow)
	return p
}

// pinChips enters pin mode, taps ids in order and ends pin mode.
func pinChips(p *Page, ids ...string) {
	p.T.Helper()
	p.TapJS(`[...` + chipRow + `.querySelectorAll("button")].find((b) => b.textContent.includes("Pin chips"))`)
	p.WaitJS(chipRow + `.classList.contains("is-pinning")`)
	for _, id := range ids {
		p.TapJS(chipByID(id))
	}
	p.TapJS(`[...` + chipRow + `.querySelectorAll("button")].find((b) => b.textContent.trim() === "Done")`)
	p.WaitJS("!" + chipRow + `.classList.contains("is-pinning")`)
}

// Calm laptop: no "Needs you" in front; the calm ✓ chip rests at the end.
func TestHomeChipsCalmOrder(t *testing.T) {
	p := openChips(t)
	p.TapJS(ButtonByText("Calm laptop"))
	ids := chipIDs(p)
	if len(ids) < 4 || ids[0] == "needs" || ids[len(ids)-1] != "needs" {
		t.Fatalf("calm order %v: needs must rest last", ids)
	}
	if got := Eval[string](p, chipByID("needs")+`.textContent`); !strings.Contains(got, "✓") || !strings.Contains(got, "Nothing waits") {
		t.Errorf("calm needs chip reads %q, want ✓ Nothing waits", got)
	}
}

// Something waits: "Needs you · 2" is first with its ● shape, then the
// chips the state raises, the bigger count first (review 3 before the
// board and git at 1).
func TestHomeChipsNeedsYouFirst(t *testing.T) {
	p := openChips(t)
	p.TapJS(ButtonByText("Calm laptop"))
	Eval[float64](p, `(`+chipRow+`.scrollLeft = 200)`)
	p.TapJS(ButtonByText("Something waits"))
	p.WaitJS(chipRow + `.querySelector("[data-chip]").dataset.chip === "needs"`)
	ids := chipIDs(p)
	if strings.Join(ids[:4], " ") != "needs review board git" {
		t.Errorf("order %v, want needs review board git first", ids)
	}
	if got := Eval[string](p, chipByID("needs")+`.textContent`); !strings.Contains(got, "●") || !strings.Contains(got, "Needs you · 2") {
		t.Errorf("needs chip reads %q, want ● Needs you · 2", got)
	}
	if got := Eval[string](p, chipByID("git")+`.textContent`); !strings.Contains(got, "✗") {
		t.Errorf("git chip with red CI reads %q, want the ✗ shape", got)
	}
	// The row came back to its start, where Needs you now is.
	if got := Eval[float64](p, chipRow+`.scrollLeft`); got != 0 {
		t.Errorf("scrollLeft %v after something waits, want 0", got)
	}
}

// Pins come right after "Needs you", in pin order; a fourth pin is
// refused with a toast and drops none of the three; pins survive a reload.
func TestHomeChipsPinUpToThree(t *testing.T) {
	p := openChips(t)
	p.TapJS(ButtonByText("Calm laptop"))
	pinChips(p, "mcp", "research", "projects", "tasks")
	if got := toasts(p); !strings.Contains(got, "Up to 3 chips stay pinned") {
		t.Errorf("toasts %q after a fourth pin, want the limit line", got)
	}
	if ids := chipIDs(p); strings.Join(ids[:4], " ") != "mcp research projects board" {
		t.Errorf("calm order with pins %v, want mcp research projects first", ids)
	}
	p.TapJS(ButtonByText("Something waits"))
	p.WaitJS(chipRow + `.querySelector("[data-chip]").dataset.chip === "needs"`)
	if ids := chipIDs(p); strings.Join(ids[:4], " ") != "needs mcp research projects" {
		t.Errorf("order %v, want needs then the 3 pins", ids)
	}
	if got := Eval[string](p, chipByID("mcp")+`.getAttribute("aria-label")`); got != "MCP, pinned" {
		t.Errorf("pinned aria-label %q, want MCP, pinned", got)
	}
	p.Reload()
	p.WaitJS(chipRow)
	p.TapJS(ButtonByText("Calm laptop"))
	if ids := chipIDs(p); strings.Join(ids[:3], " ") != "mcp research projects" {
		t.Errorf("after reload order %v, want the pins kept", ids)
	}
	// Unpin one: it goes back to its place.
	pinChips(p, "research")
	if ids := chipIDs(p); strings.Join(ids[:2], " ") != "mcp projects" {
		t.Errorf("after unpin order %v, want mcp projects first", ids)
	}
}

// Needs you cannot be pinned (the state already puts it first).
func TestHomeChipsNeedsYouIsNotPinnable(t *testing.T) {
	p := openChips(t)
	p.TapJS(ButtonByText("Something waits"))
	p.TapJS(`[...` + chipRow + `.querySelectorAll("button")].find((b) => b.textContent.includes("Pin chips"))`)
	p.WaitJS(chipRow + `.classList.contains("is-pinning")`)
	if !Eval[bool](p, chipByID("needs")+`.disabled`) {
		t.Error("needs chip is pinnable in pin mode")
	}
}

// One line that scrolls sideways at 390 px, every chip 44 px or more.
func TestHomeChipsScrollSidewaysAndBigTargets(t *testing.T) {
	p := openChips(t)
	if got := Eval[string](p, `getComputedStyle(`+chipRow+`).overflowX`); got != "auto" {
		t.Errorf("overflow-x %q, want auto", got)
	}
	if !Eval[bool](p, chipRow+`.scrollWidth > `+chipRow+`.clientWidth`) {
		t.Error("the row fits at 390 px: it must scroll sideways, not wrap")
	}
	if lines := Eval[int](p, `new Set([...`+chipRow+`.querySelectorAll("button")].map((b) => Math.round(b.getBoundingClientRect().top))).size`); lines != 1 {
		t.Errorf("chips on %d lines, want 1", lines)
	}
	if bad := Eval[[]string](p, smallTargetsJS("#chips .hchips button")); len(bad) > 0 {
		t.Errorf("chips under 44 px: %v", bad)
	}
}
