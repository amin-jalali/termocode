package diffview

// DemoFiles is a sample review (two files) for screenshots, golden tests
// and the "diffdemo" developer action.
func DemoFiles() []File {
	return []File{
		{
			Name: "internal/toast/model.go",
			Old: `package toast

import "time"

// Tick expires toasts whose time is up.
func (m Model) Tick(now time.Time) (Model, Cmd) {
	var out []*Toast
	for _, t := range m.toasts {
		if !t.ExpiresAt.After(now) {
			t.leaving = true
			t.slide.Set(1)
		}
		out = append(out, t)
	}
	m.toasts = out
	return m, m.next()
}

// Step advances the slide animation by one frame.
func (m Model) Step() (Model, Cmd) {
	for _, t := range m.toasts {
		t.slide.Step()
	}
	return m.drop(), nil
}

// drop removes the toasts that finished leaving.
func (m Model) drop() Model {
	keep := m.toasts[:0]
	for _, t := range m.toasts {
		if !(t.leaving && t.slide.Done()) {
			keep = append(keep, t)
		}
	}
	m.toasts = keep
	return m
}
`,
			New: `package toast

import "time"

// Tick expires toasts whose time is up. A toast that is already
// leaving is skipped, so it slides out only once.
func (m Model) Tick(now time.Time) (Model, Cmd) {
	var out []*Toast
	for _, t := range m.toasts {
		if !t.ExpiresAt.After(now) && !t.leaving {
			t.leaving = true // slide out once
			t.slide.Set(1)
		}
		out = append(out, t)
	}
	m.toasts = out
	return m, m.next()
}

// Step advances the slide animation by one frame.
func (m Model) Step() (Model, Cmd) {
	for _, t := range m.toasts {
		t.slide.Step()
	}
	return m.drop(), nil
}

// drop removes the toasts that finished leaving.
func (m Model) drop() Model {
	keep := m.toasts[:0]
	for _, t := range m.toasts {
		if t.leaving && t.slide.Done() {
			continue
		}
		keep = append(keep, t)
	}
	m.toasts = keep
	return m
}
`,
		},
		{
			Name: "internal/toast/model_test.go",
			Tag:  "new file",
			New: `package toast

import (
	"testing"
	"time"
)

func TestTickSlidesOutOnce(t *testing.T) {
	m := New()
	m, _ = m.Push(Info, "hi")
	now := time.Now().Add(time.Hour)
	m, _ = m.Tick(now)
	m, _ = m.Tick(now)
	if got := m.toasts[0].slide.Starts(); got != 1 {
		t.Fatalf("slide started %d times", got)
	}
}
`,
		},
	}
}
