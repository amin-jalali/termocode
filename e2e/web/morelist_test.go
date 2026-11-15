//go:build browser

package web

import (
	"strings"
	"testing"
)

// The More list (RM-3) on a phone and a tablet: a search box on top,
// grouped rows with a small header and an icon each, the recent views
// first, no lone "Home" row; every row opens its view.
func TestMoreListGroupsSearchRecent(t *testing.T) {
	for _, vp := range []Viewport{Phone, Tablet} {
		t.Run(vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, vp)
			p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
			p.Login("c-morelist")
			p.GoTab("more")
			p.WaitJS(`document.querySelector(".more-find") && document.querySelectorAll(".more-group").length >= 4`)
			if n := Eval[int](p, `document.querySelectorAll('.more-link[data-sub="home"]').length`); n != 0 {
				t.Error("the lone Home row is still there")
			}
			heads := Eval[[]string](p, `[...document.querySelectorAll(".more-group > .group-title")].map((h) => h.textContent)`)
			if len(heads) < 4 {
				t.Errorf("group headers %q", heads)
			}
			bare := Eval[[]string](p, `[...document.querySelectorAll(".more-group .more-link")].filter((b) => !b.querySelector("svg.more-ic")).map((b) => b.dataset.sub)`)
			if len(bare) > 0 {
				t.Errorf("rows without an icon: %q", bare)
			}
			// the search box filters the rows
			p.Tap(".more-find")
			p.Key("kube")
			p.WaitJS(`[...document.querySelectorAll(".more-link")].map((b) => b.dataset.sub).join() === "kube"`)
			Eval[int](p, `(() => { const i = document.querySelector(".more-find"); i.value = ""; i.dispatchEvent(new Event("input", {bubbles: true})); return 1; })()`)
			p.WaitJS(`document.querySelectorAll(".more-group").length >= 4`)
			// a visited view comes first, under Recent
			p.Tap(`.more-link[data-sub="containers"]`)
			p.WaitJS(`location.hash === "#/more/containers"`)
			p.GoTab("more")
			p.WaitJS(`document.querySelector(".more-group.is-recent .more-link")?.dataset.sub === "containers"`)
			if got := Eval[string](p, `document.querySelector(".more-group.is-recent > .group-title").textContent`); !strings.Contains(got, "Recent") {
				t.Errorf("recent header %q", got)
			}
			p.noOverflow("more")
		})
	}
}
