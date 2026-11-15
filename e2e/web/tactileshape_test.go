//go:build browser

package web

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Tactile keyboard's shape. Landscape (a phone turned sideways) and a
// tablet keep their keys exactly as recorded in testdata (the look the
// owner likes); a phone held upright gets keys that look like those,
// scaled down: not tall and thin, every label on one line.

// tkShapeUpdate rewrites the landscape records (only when the landscape
// look is meant to change).
const tkShapeUpdate = "TACTILE_SHAPE_UPDATE"

// PhonePortraitSafari is the phone upright with Safari's real page
// height (its bars take the rest of the 844).
var PhonePortraitSafari = Viewport{Name: "portrait-safari", W: 390, H: 664, Scale: 3, Mobile: true}

// tkShapeJS reads every key of the visible board: its box, its face and
// the computed type of its legends (never text widths: those follow the
// machine's fonts).
const tkShapeJS = `(() => {
  const r2 = (n) => Math.round(n * 100) / 100;
  const box = (e, o) => { const b = e.getBoundingClientRect(); return [r2(b.left - o.left), r2(b.top - o.top), r2(b.width), r2(b.height)]; };
  const kb = document.querySelector(".tk-dock:not([hidden]) .tk-kb");
  const o = kb.getBoundingClientRect();
  const keys = [...kb.querySelectorAll(".tk-key")].map((k) => {
    const face = k.querySelector(".tk-face");
    const legends = [...k.querySelectorAll(".tk-legend, .tk-label, .tk-main, .tk-up, .tk-icon, .tk-fnum, .tk-sec, .tk-led")].map((l) => {
      const s = getComputedStyle(l);
      return [l.className, s.fontSize, s.lineHeight, s.letterSpacing, s.whiteSpace, s.overflowWrap, s.paddingLeft, s.paddingRight].join("|");
    });
    return { key: k.dataset.key, cls: k.className, box: box(k, o), face: face ? box(face, o) : null, faceStyle: face ? (() => { const s = getComputedStyle(face); return [s.paddingLeft, s.paddingRight, s.justifyContent, s.textAlign].join("|"); })() : "", legends };
  });
  return JSON.stringify({ kb: [r2(o.width), r2(o.height)], keys }, null, 1);
})()`

// tkOpen docks the keyboard on the phone terminal at vp.
func tkOpen(t *testing.T, vp Viewport) *Page {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, vp)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-1")
	p.GoTab("term")
	c.Subs(t, "terms")
	showTerm(t, p, c, termSnap(3, 0, 80, "$ "), termItem(3, "zsh", false, true))
	p.TapJS(termView + `.querySelector(".pterm-x .xterm-screen")`)
	p.WaitJS(tkDock + ` && !` + tkDock + `.hidden && document.querySelector(".tk-dock .tk-key")`)
	return p
}

type tkShape struct {
	KB   [2]float64 `json:"kb"`
	Keys []struct {
		Key  string     `json:"key"`
		Box  [4]float64 `json:"box"`
		Face [4]float64 `json:"face"`
	} `json:"keys"`
}

func tkGolden(vp Viewport) string {
	return filepath.Join("testdata", "tactile-shape-"+vp.Name+".json")
}

func readTkShape(t *testing.T, raw string) tkShape {
	t.Helper()
	var s tkShape
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("shape %q: %v", short(raw), err)
	}
	return s
}

// tkAspect is the width over the height of the A key.
func tkAspect(t *testing.T, s tkShape) float64 {
	t.Helper()
	for _, k := range s.Keys {
		if k.Key == "A" {
			return k.Box[2] / k.Box[3]
		}
	}
	t.Fatal("no A key")
	return 0
}

// Landscape and a tablet: every key's box, face and legend type exactly
// as recorded, so a change for the upright phone cannot move them.
func TestTactileKeyboardLandscapeShapeHolds(t *testing.T) {
	for _, vp := range []Viewport{PhoneLandscape, TabletTouch} {
		t.Run(vp.Name, func(t *testing.T) {
			p := tkOpen(t, vp)
			got := Eval[string](p, tkShapeJS)
			path := tkGolden(vp)
			if os.Getenv(tkShapeUpdate) != "" {
				if err := os.WriteFile(path, []byte(got+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (record it with %s=1)", err, tkShapeUpdate)
			}
			if strings.TrimSpace(string(want)) != strings.TrimSpace(got) {
				gl, wl := strings.Split(got, "\n"), strings.Split(strings.TrimSpace(string(want)), "\n")
				for i := 0; i < len(gl) && i < len(wl); i++ {
					if gl[i] != wl[i] {
						t.Fatalf("the %s keyboard changed at line %d:\n got %s\nwant %s", vp.Name, i+1, gl[i], wl[i])
					}
				}
				t.Fatalf("the %s keyboard changed: %d lines, want %d", vp.Name, len(gl), len(wl))
			}
		})
	}
}

// The phone upright: the keys look like the landscape ones scaled down.
// A key is at least half as wide (per height) as in landscape, never
// wider, and every word label sits on one line inside its keycap.
func TestTactileKeyboardPortraitProportions(t *testing.T) {
	raw, err := os.ReadFile(tkGolden(PhoneLandscape))
	if err != nil {
		t.Fatal(err)
	}
	land := tkAspect(t, readTkShape(t, string(raw)))
	p := tkOpen(t, PhonePortraitSafari)
	s := readTkShape(t, Eval[string](p, tkShapeJS))
	if a := tkAspect(t, s); a < land/2 || a > land {
		t.Errorf("upright key A is %.2f wide per high, want %.2f…%.2f (landscape %.2f)", a, land/2, land, land)
	}
	// Each label's text: one line box, inside its face.
	bad := Eval[[]string](p, `[...document.querySelectorAll(".tk-dock:not([hidden]) .tk-key")].flatMap((k) => {
	  const face = k.querySelector(".tk-face");
	  if (!face) return [];
	  const f = face.getBoundingClientRect();
	  return [...k.querySelectorAll(".tk-label, .tk-main, .tk-up")].flatMap((l) => {
	    const rg = document.createRange();
	    rg.selectNodeContents(l);
	    const lines = new Set([...rg.getClientRects()].map((r) => Math.round(r.top)));
	    const b = rg.getBoundingClientRect();
	    const out = b.left < f.left - 0.5 || b.right > f.right + 0.5 || b.top < f.top - 0.5 || b.bottom > f.bottom + 0.5;
	    return lines.size !== 1 || out ? [k.dataset.key + " " + JSON.stringify(l.textContent) + " lines=" + lines.size + (out ? " outside its keycap" : "")] : [];
	  });
	})`)
	if len(bad) > 0 {
		t.Errorf("labels that wrap or spill on the upright phone: %v", bad)
	}
	// Backspace upright: the symbol, as big as the letters' legends.
	bk := Eval[[]string](p, `(() => {
	  const q = (k) => document.querySelector('.tk-dock:not([hidden]) .tk-key[data-key="' + k + '"]');
	  const b = q("backspace"), a = q("A").querySelector(".tk-main"), m = b.querySelector(".tk-main");
	  return [b.getAttribute("aria-label"), m ? m.textContent : "", m ? getComputedStyle(m).fontSize : "none", getComputedStyle(a).fontSize];
	})()`)
	if bk[0] != "backspace" || bk[1] != "\u232b" || bk[2] != bk[3] {
		t.Errorf("upright backspace: aria-label %q, legend %q at %s, want \"backspace\", \"\u232b\" at the letters' %s", bk[0], bk[1], bk[2], bk[3])
	}
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("problems (CSP, console): %v", probs)
	}
}
