//go:build browser

package web

import (
	"fmt"
	"testing"
	"time"
)

// DO NOT MERGE: repeats TestNowFabNoBadge's tap on the macOS runner and
// logs what the ✦ button gets. It logs, never fails.
func TestZZDiagNowFabTap(t *testing.T) {
	for i := 0; i < 8; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			now := time.Now().UnixMilli()
			p, _ := openNow(t, map[string]any{
				"items":  []any{nowItem("run:c2/r1", "run", "working", "docs-sync agent", "none", "Running", "", 4, now-4*60_000, "stop")},
				"counts": map[string]any{"working": 1},
				"now":    nowInfo(true, 5),
			})
			p.WaitJS(nowHome + `?.querySelector(".now-clear")`)
			p.WaitJS(`!!document.querySelector(".rm-fab")`)
			Eval[bool](p, `(() => {
				window.__ev = [];
				const fab = () => document.querySelector(".rm-fab");
				const st = () => { const f = fab(); if (!f) return "nofab"; const r = f.getBoundingClientRect(); return [Math.round(r.left), Math.round(r.top), Math.round(r.width), f.className, getComputedStyle(f).position, f.hidden, document.documentElement.style.getPropertyValue("--now-badge") || "-"].join(" "); };
				window.__ev.push("start " + st());
				for (const n of ["pointerdown", "pointerup", "pointercancel", "lostpointercapture", "click", "touchstart", "touchend"]) {
					document.addEventListener(n, (e) => { const tg = e.target; window.__ev.push(n + "@" + ((tg && (tg.className && tg.className.baseVal !== undefined ? tg.className.baseVal : tg.className)) || (tg && tg.nodeName) || "?").toString().slice(0, 30) + " xy=" + Math.round(e.clientX ?? (e.touches && e.touches[0] ? e.touches[0].clientX : -1)) + "," + Math.round(e.clientY ?? -1) + " t=" + Math.round(e.timeStamp) + " " + st()); }, true);
				}
				new MutationObserver(() => window.__ev.push("mut " + st() + " overlay=" + !!document.querySelector(".rm-overlay"))).observe(document.body, { childList: true });
				return true;
			})()`)
			p.Tap(".rm-fab")
			time.Sleep(1500 * time.Millisecond)
			open := Eval[bool](p, `!!document.querySelector(".rm-overlay")`)
			t.Log("overlay:", open, "hash:", Eval[string](p, `location.hash`))
			for _, e := range Eval[[]string](p, `window.__ev`) {
				t.Log("  ", e)
			}
		})
	}
}
