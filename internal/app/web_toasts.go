package app

import (
	"encoding/json"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The toasts topic (WEB-12): the toasts the laptop shows now. A new toast
// (or a keyed one updated in place) is a push, one that went away a
// dismiss; the page shows them as its own toasts.

func init() {
	webTopics["toasts"] = webTopicDef{snapshot: webToastsSnapshot, apply: webApplyToasts}
	webAfterUpdates = append(webAfterUpdates, (*Model).webPublishToasts)
}

// webToastsPub is what the toasts topic sent last (Update only).
type webToastsPub struct {
	last []webproto.Toast // the toasts the browsers have
}

func webToastsSnapshot(m *Model, _ string) (any, error) {
	cur := webToastsOf(m.toast)
	if m.web != nil {
		m.web.shell.toasts.last = cur // the deltas continue from this snap
	}
	return webproto.ToastsSnap{Toasts: cur}, nil
}

// webPublishToasts sends what changed since the last call.
func (m *Model) webPublishToasts(tea.Msg) tea.Cmd {
	h := m.web
	p := &h.shell.toasts
	if !h.webListens("toasts") {
		p.last = nil
		return nil
	}
	if m.toast.Empty() && len(p.last) == 0 {
		return nil
	}
	cur := webToastsOf(m.toast)
	old := make(map[int]webproto.Toast, len(p.last))
	for _, t := range p.last {
		old[t.ID] = t
	}
	now := make(map[int]bool, len(cur))
	for _, t := range cur {
		now[t.ID] = true
	}
	for _, t := range p.last {
		if !now[t.ID] {
			h.publish("toasts", webproto.ToastsDelta{Op: webproto.ToastDismiss, ID: t.ID})
		}
	}
	for _, t := range cur {
		if o, ok := old[t.ID]; !ok || o != t {
			t := t
			h.publish("toasts", webproto.ToastsDelta{Op: webproto.ToastPush, Toast: &t})
		}
	}
	p.last = cur
	return nil
}

// webToastsOf lists the laptop's toasts for the page.
func webToastsOf(tm toast.Model) []webproto.Toast {
	live := tm.Live()
	out := make([]webproto.Toast, 0, len(live))
	for _, t := range live {
		if t.Private { // a one-time link stays on the laptop
			continue
		}
		out = append(out, webproto.Toast{ID: t.ID, Severity: webSeverity(t.Severity), Title: t.Title, Detail: t.Detail})
	}
	return out
}

func webSeverity(s toast.Severity) string {
	switch s {
	case toast.Warn:
		return "warn"
	case toast.Errr:
		return "error"
	}
	return "info"
}

// webApplyToasts folds one toasts delta into a snapshot (the browser
// store does the same).
func webApplyToasts(snap, delta json.RawMessage) (json.RawMessage, error) {
	var s webproto.ToastsSnap
	var d webproto.ToastsDelta
	if err := json.Unmarshal(snap, &s); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(delta, &d); err != nil {
		return nil, err
	}
	switch d.Op {
	case webproto.ToastPush:
		if d.Toast == nil {
			return nil, fmt.Errorf("toasts: push without a toast")
		}
		i := 0
		for i < len(s.Toasts) && s.Toasts[i].ID != d.Toast.ID {
			i++
		}
		if i == len(s.Toasts) {
			s.Toasts = append(s.Toasts, *d.Toast)
		} else {
			s.Toasts[i] = *d.Toast
		}
	case webproto.ToastDismiss:
		out := s.Toasts[:0]
		for _, t := range s.Toasts {
			if t.ID != d.ID {
				out = append(out, t)
			}
		}
		s.Toasts = out
	default:
		return nil, fmt.Errorf("toasts: unknown op %q", d.Op)
	}
	return json.Marshal(s)
}
