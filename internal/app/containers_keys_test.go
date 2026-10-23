package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func contKey(m Model, s string) (Model, tea.Cmd) {
	var k tea.KeyMsg
	switch s {
	case "tab":
		k = tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		k = tea.KeyMsg{Type: tea.KeyShiftTab}
	default:
		k = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	return m.handleServicesSidebarKey(k)
}

func sectionIdx(st *servicesState) []int {
	var out []int
	for i, r := range st.rows() {
		if r.kind == svcRowSection {
			out = append(out, i)
		}
	}
	return out
}

// CP-11: Tab / Shift+Tab jump from header to header and wrap around.
func TestContainersTabJumpsBetweenSectionHeaders(t *testing.T) {
	st := svcHostState()
	st.loaded = true
	m := svcTestModel(st, 100, 40)
	heads := sectionIdx(st)
	if len(heads) < 4 {
		t.Fatalf("only %d section headers", len(heads))
	}
	st.cursor = heads[0] + 1 // a row inside the first section
	for rows := st.rows(); rows[st.cursor].kind == svcRowSection; rows = st.rows() {
		st.cursor++
	}
	m, _ = contKey(m, "tab")
	if st.cursor != heads[1] {
		t.Errorf("Tab: cursor %d, want header %d", st.cursor, heads[1])
	}
	m, _ = contKey(m, "shift+tab")
	if st.cursor != heads[0] {
		t.Errorf("Shift+Tab: cursor %d, want header %d", st.cursor, heads[0])
	}
	m, _ = contKey(m, "shift+tab")
	if st.cursor != heads[len(heads)-1] {
		t.Errorf("Shift+Tab wrap: cursor %d, want last header %d", st.cursor, heads[len(heads)-1])
	}
	m, _ = contKey(m, "tab")
	if st.cursor != heads[0] {
		t.Errorf("Tab wrap: cursor %d, want first header %d", st.cursor, heads[0])
	}
}

// CP-11: "?" shows the keys of the row kind under the cursor.
func TestContainersQuestionMarkShowsKeysOfRowKind(t *testing.T) {
	st := svcHostState()
	st.loaded = true
	for sec := range st.collapsed {
		st.collapsed[sec] = false
	}
	cases := []struct {
		name string
		pick func(r svcRow) bool
		want string
	}{
		{"container", func(r svcRow) bool { return r.kind == svcRowHostItem && r.host == hostContainer }, "stop"},
		{"image", func(r svcRow) bool { return r.kind == svcRowHostItem && r.host == hostImage }, "pull"},
		{"images header", func(r svcRow) bool { return r.kind == svcRowSection && r.section == hostSection("images") }, "prune"},
		{"network", func(r svcRow) bool { return r.kind == svcRowHostItem && r.host == hostNetwork }, "remove"},
		{"context", func(r svcRow) bool { return r.kind == svcRowHostItem && r.host == hostContext }, "default"},
	}
	for _, c := range cases {
		m := svcTestModel(st, 100, 40)
		found := false
		for i, r := range st.rows() {
			if c.pick(r) {
				st.cursor, found = i, true
				break
			}
		}
		if !found {
			t.Fatalf("%s: no row", c.name)
		}
		m, _ = contKey(m, "?")
		// X-1: the keys are a card that stays open, not a toast.
		m.confirm.SetSize(140, 50)
		got := ansi.Strip(m.confirm.View())
		if !m.confirmOpen || m.confirmKind != confirmKindKeyCard || !strings.Contains(got, "keys") || !strings.Contains(strings.ToLower(got), c.want) {
			t.Errorf("%s: card %q lacks keys / %q", c.name, got, c.want)
		}
	}
}
