//go:build browser

package web

import (
	"testing"
	"time"
)

// The laptop's toasts (WEB-12, web UX 7.3): a push shows, a push with the
// same id updates in place, a dismiss hides; info hides by itself after
// 5 s while an error stays until closed; a toast closed here stays closed
// when a fresh snapshot still lists it.

const toastTitles = `[...document.querySelectorAll(".toasts .toast-title")].map((e) => e.textContent).join("|")`

func toastData(id int, sev, title, detail string) map[string]any {
	t := map[string]any{"id": id, "severity": sev, "title": title}
	if detail != "" {
		t["detail"] = detail
	}
	return t
}

func push(id int, sev, title, detail string) map[string]any {
	return map[string]any{"op": "push", "toast": toastData(id, sev, title, detail)}
}

func TestToastsFromLaptop(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, vp)
			c := p.Login("c-1")
			c.Send(t, Delta("toasts", 2, push(7, "error", "Build failed", "exit 2")))
			p.WaitJS(toastTitles + ` === "Build failed"`)
			if !Eval[bool](p, `document.querySelector(".toasts .toast").classList.contains("sev-error")`) {
				t.Error("the error toast has no sev-error hairline")
			}
			c.Send(t, Delta("toasts", 3, push(9, "warn", "Cloning", "10%")))
			c.Send(t, Delta("toasts", 4, push(9, "warn", "Cloning", "90%")))
			p.WaitJS(`[...document.querySelectorAll(".toasts .toast-detail")].map((e) => e.textContent).join("|") === "exit 2|90%"`)

			start := time.Now()
			c.Send(t, Delta("toasts", 5, push(8, "info", "Saved", "")))
			p.WaitJS(`(` + toastTitles + `).includes("Saved")`)
			p.WaitJS(`!(` + toastTitles + `).includes("Saved")`)
			if d := time.Since(start); d < 4*time.Second {
				t.Errorf("the info toast hid after %s, want about 5 s", d)
			}
			if got := Eval[string](p, toastTitles); got != "Build failed" {
				t.Errorf("after 5 s the toasts are %q, want the error only", got)
			}

			c.Send(t, Delta("toasts", 6, map[string]any{"op": "dismiss", "id": 7}))
			p.WaitJS(`!document.querySelector(".toasts")`)
		})
	}
}

func TestToastClosedHereStaysClosed(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	c := p.Login("c-1")
	c.Send(t, Delta("toasts", 2, push(7, "error", "Build failed", "exit 2")))
	p.WaitJS(toastTitles + ` === "Build failed"`)
	p.Tap(".toasts .toast-x")
	p.WaitJS(`!document.querySelector(".toasts")`)
	c.Send(t, Snap("toasts", 9, map[string]any{"toasts": []any{toastData(7, "error", "Build failed", "exit 2"), toastData(10, "error", "Lint failed", "")}}))
	p.WaitJS(toastTitles + ` === "Lint failed"`)
}
