package app

import (
	"strings"
	"testing"
)

// containerOrder is the ids of the container rows as shown.
func containerOrder(m *Model) []string {
	var ids []string
	for _, r := range m.services.rows() {
		if r.kind == svcRowHostItem && r.host == hostContainer {
			ids = append(ids, m.services.containerRows[r.idx].ID)
		}
	}
	return ids
}

func TestContainerSortPickerItems(t *testing.T) {
	mm := svcTestModel(svcHostState(), 120, 50)
	m := &mm
	items := m.containerSortItems()
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	all := strings.Join(ids, " ")
	for _, want := range []string{"svc:list-sort:", "svc:list-sort:name", "svc:list-sort:state", "svc:list-sort:created", "svc:list-sort:size", "svc:list-size:toggle"} {
		if !strings.Contains(all+" ", want+" ") {
			t.Errorf("sort items lack %q: %s", want, all)
		}
	}
}

func TestContainerSortByNameReordersInsideGroups(t *testing.T) {
	mm := svcTestModel(svcHostState(), 120, 50)
	m := &mm
	before := strings.Join(containerOrder(m), ",")
	m.handleServicesPickerSelect("svc:list-sort:name")
	if m.services.containerSort != "name" {
		t.Fatalf("sort = %q", m.services.containerSort)
	}
	after := containerOrder(m)
	// termocode group: api-1, postgres-1, redis-1 by name.
	if strings.Join(after[:3], ",") != "c3,c1,c2" {
		t.Fatalf("name order: %v (before %s)", after, before)
	}
	m.handleServicesPickerSelect("svc:list-sort:")
	if strings.Join(containerOrder(m), ",") != before {
		t.Fatal("default order not restored")
	}
}

func TestContainerSizeColumnToggle(t *testing.T) {
	mm := svcTestModel(svcHostState(), 120, 50)
	m := &mm
	m.handleServicesPickerSelect("svc:list-size:toggle")
	if !m.services.containerSizes {
		t.Fatal("size column not on")
	}
	m.services.containerRows[0].Size = "12.3kB (virtual 400MB)"
	cursorOnContainer(t, m, "c1")
	r, _ := m.servicesCurrentRow()
	if line := m.services.renderHostItem(r, false, false, 60); !strings.Contains(line, "12.3kB") {
		t.Fatalf("no size in row: %q", line)
	}
	// Sorting by size turns the column on.
	m.services.containerSizes = false
	m.handleServicesPickerSelect("svc:list-sort:size")
	if !m.services.containerSizes {
		t.Fatal("size sort without sizes")
	}
}

func TestContainerSortKeyAndPalette(t *testing.T) {
	mm := svcTestModel(svcHostState(), 120, 50)
	m := &mm
	cursorOnContainer(t, m, "c1")
	next, _ := m.handleServicesSidebarKey(keyMsg("O"))
	*m = next
	if !m.pickerOpen {
		t.Fatal("O opened no picker")
	}
	_, rows := hostKeysHelp(svcRow{kind: svcRowHostItem, host: hostContainer})
	found := false
	for _, r := range rows {
		found = found || (r.Key == "O" && strings.Contains(r.What, "sort"))
	}
	if !found {
		t.Fatal("key card lacks O sort")
	}
	ids := map[string]bool{}
	for _, it := range servicesPaletteItems() {
		ids[it.ID] = true
	}
	if !ids["containers-sort"] || !ids["containers-toggle-size"] {
		t.Fatal("palette lacks sort / size")
	}
}
