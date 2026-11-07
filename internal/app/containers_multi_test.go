package app

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
)

func allContextsState() *servicesState {
	st := svcHostState()
	st.allContexts, st.multiLoaded, st.multiGen = true, true, 1
	st.multi = containers.MultiResult{
		Containers: []containers.SourceContainer{
			{Source: "default", Container: containers.Container{ID: "a1", Names: []string{"web"}, Image: "nginx", State: "running", Status: "Up 1 hour"}},
			{Source: "prod", Container: containers.Container{ID: "b1", Names: []string{"api"}, Image: "api:1", State: "running", Status: "Up 2 days"}},
		},
		Problems: []containers.SourceProblem{{Source: "edge", Err: errors.New("cannot connect")}},
	}
	return st
}

func TestAllContextsRowsGroupBySourceWithProblemNote(t *testing.T) {
	st := allContextsState()
	rows := st.allContextsRows()
	var groups, items []string
	note := ""
	for _, r := range rows {
		switch {
		case r.kind == svcRowGroup:
			groups = append(groups, r.name)
		case r.kind == svcRowHostItem && r.host == hostAllContainer:
			items = append(items, r.name)
		case r.kind == svcRowNote:
			note = r.text
		}
	}
	if strings.Join(groups, ",") != "default,prod" || strings.Join(items, ",") != "web,api" {
		t.Fatalf("groups %v items %v", groups, items)
	}
	if !strings.Contains(note, "edge") || !strings.Contains(note, "cannot connect") {
		t.Fatalf("problem note = %q", note)
	}
	if st.allContextsCount() != "2 · 1 not answering" {
		t.Fatalf("count = %q", st.allContextsCount())
	}
	st.allContexts = false
	if st.allContextsRows() != nil {
		t.Fatal("rows while the section is off")
	}
}

func TestAllContextsSectionSitsAboveHelp(t *testing.T) {
	st := allContextsState()
	order := []string{}
	for _, r := range st.hostRows() {
		if r.kind == svcRowSection {
			order = append(order, r.section)
		}
	}
	all, help := -1, -1
	for i, s := range order {
		switch s {
		case hostSection(allContextsSection):
			all = i
		case hostSection("help"):
			help = i
		}
	}
	if all < 0 || (help >= 0 && all > help) {
		t.Fatalf("section order = %v", order)
	}
}

func TestAllContextsRowCopiesIDAndOtherKeysDoNothing(t *testing.T) {
	m := svcTestModel(allContextsState(), 120, 40)
	for i, r := range m.services.rows() {
		if r.host == hostAllContainer && r.name == "api" {
			m.services.cursor = i
		}
	}
	if c, ok := m.selectedAllContainer(); !ok || c.Source != "prod" {
		t.Fatalf("selected = %+v %v", c, ok)
	}
	if _, ok := m.selectedContainer(); ok {
		t.Fatal("an ALL CONTEXTS row counts as an active-context container")
	}
	m2, _ := m.handleServicesSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if m2.confirmOpen || len(m2.terminalTabs) != 0 {
		t.Fatal("x on another context's row did something")
	}
}

func TestAllContextsPaletteEntryHasPolicy(t *testing.T) {
	found := false
	for _, c := range servicesPaletteItems() {
		found = found || c.ID == "containers-all-contexts"
	}
	if _, ok := aiActionPolicies["containers-all-contexts"]; !found || !ok {
		t.Fatalf("palette %v policy %v", found, ok)
	}
}
