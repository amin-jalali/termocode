package debounce

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

type fired struct{ n int }

func TestDoOnlyNewestFires(t *testing.T) {
	d := New()
	first := d.Do("k", 0, func() tea.Msg { return fired{1} })
	second := d.Do("k", 0, func() tea.Msg { return fired{2} })
	if msg := first(); msg != nil {
		t.Errorf("superseded call fired: %#v", msg)
	}
	if msg, ok := second().(fired); !ok || msg.n != 2 {
		t.Errorf("newest call = %#v, want fired{2}", msg)
	}
}

func TestDoKeysIndependent(t *testing.T) {
	d := New()
	a := d.Do("a", 0, func() tea.Msg { return fired{1} })
	b := d.Do("b", 0, func() tea.Msg { return fired{2} })
	if a() == nil || b() == nil {
		t.Error("different keys must not supersede each other")
	}
}

func TestCancel(t *testing.T) {
	d := New()
	c := d.Do("k", 0, func() tea.Msg { return fired{1} })
	d.Cancel("k")
	if msg := c(); msg != nil {
		t.Errorf("cancelled call fired: %#v", msg)
	}
}

func TestNextCurrent(t *testing.T) {
	var d Debouncer // zero value is usable
	g := d.Next("req")
	if !d.Current("req", g) {
		t.Error("fresh generation should be current")
	}
	d.Next("req")
	if d.Current("req", g) {
		t.Error("old generation should be stale")
	}
}

func TestNilDebouncer(t *testing.T) {
	var d *Debouncer
	c := d.Do("k", 0, func() tea.Msg { return fired{1} })
	if c() == nil {
		t.Error("nil debouncer should still run fn")
	}
	if d.Do("k", 0, nil) != nil {
		t.Error("nil fn should yield nil cmd")
	}
}
