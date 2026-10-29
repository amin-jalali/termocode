package app

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func kubeKey(t *testing.T, m *Model, keys ...string) tea.Cmd {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case " ":
			msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		nm, c := m.dispatchToFocus(msg)
		*m = nm
		cmd = c
	}
	return cmd
}

func kubeCursorName(m *Model) string {
	r, _ := m.kube.currentRow()
	if r.kind == kubeRowSection {
		return "#" + r.section
	}
	return r.name
}

func TestKubeKeysMoveFoldAndJump(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	if got := kubeCursorName(m); got != "#pods" {
		t.Fatalf("start = %q", got)
	}
	kubeKey(t, m, "j", "j")
	if got := kubeCursorName(m); got != "worker-5c4b-77k2p" {
		t.Errorf("j j = %q", got)
	}
	kubeKey(t, m, "tab")
	if got := kubeCursorName(m); got != "#deployments" {
		t.Errorf("tab = %q", got)
	}
	kubeKey(t, m, " ") // fold DEPLOYMENTS
	kubeKey(t, m, "j")
	if got := kubeCursorName(m); got != "#services" {
		t.Errorf("j past a folded section = %q", got)
	}
	kubeKey(t, m, "G")
	if got := kubeCursorName(m); got != "broken" {
		t.Errorf("G = %q", got)
	}
	kubeKey(t, m, "esc")
	if m.focus != FocusEditor {
		t.Error("esc kept the focus")
	}
}

func TestKubeFilterNarrowsEverySection(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	kubeKey(t, m, "/", "w", "o", "r")
	st := m.kube
	if st.filter != "wor" || !st.filterTyping {
		t.Fatalf("filter %q typing %v", st.filter, st.filterTyping)
	}
	if got := kubeCursorName(m); got != "worker-5c4b-77k2p" {
		t.Errorf("cursor = %q", got)
	}
	var items []string
	for _, r := range st.rows() {
		if r.kind == kubeRowItem {
			items = append(items, r.section+"/"+r.name)
		}
	}
	if len(items) != 2 || items[0] != "pods/worker-5c4b-77k2p" || items[1] != "deployments/worker" {
		t.Errorf("items = %v", items)
	}
	if st.count("pods") != "1/5" || st.count("jobs") != "0/3" {
		t.Errorf("counts = %s %s", st.count("pods"), st.count("jobs"))
	}
	kubeKey(t, m, "enter") // keep the filter, give the keys back
	kubeKey(t, m, "j")
	if got := kubeCursorName(m); got != "#deployments" {
		t.Errorf("j after enter = %q", got)
	}
	kubeKey(t, m, "esc") // clears the filter, focus stays
	if st.filter != "" || m.focus != FocusExplorer {
		t.Errorf("esc: filter %q focus %v", st.filter, m.focus)
	}
}

func TestKubeRefreshKeyLoadsNow(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	kubeRunLoad(t, m, kubeKey(t, m, "R"))
	if n := f.Count("get pods"); n != 2 {
		t.Errorf("pod lists = %d, want 2", n)
	}
}
