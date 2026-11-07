package app

import (
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/picker"
)

// ── Containers list: sort key + size column (CTX-22) ─────────────────────
//
// O opens the "Sort Containers" list: project order (the default), name,
// image, state, created or size, and a line that turns the SIZE column
// on or off. Rows stay in their compose project; the sort works inside
// each group. The size column lists with `ps --size`, which is slower,
// so it is off until asked for; sorting by size turns it on.

var containerSortKeys = [][2]string{
	{"", "project order"},
	{"name", "name"},
	{"image", "image"},
	{"state", "state (running first)"},
	{"created", "created (newest first)"},
	{"size", "size (largest first; turns sizes on)"},
}

func containerSortLabel(key string) string {
	for _, k := range containerSortKeys {
		if k[0] == key {
			return k[1]
		}
	}
	return key
}

// containerSortItems are the rows of the sort list.
func (m *Model) containerSortItems() []picker.Item {
	st := m.services
	var items []picker.Item
	for _, k := range containerSortKeys {
		hint := ""
		if k[0] == st.containerSort {
			hint = "current"
		}
		items = append(items, picker.Item{ID: "svc:list-sort:" + k[0], Title: "Sort by " + k[1], Hint: hint})
	}
	size := "Show the SIZE column (slower list)"
	if st.containerSizes {
		size = "Hide the SIZE column"
	}
	return append(items, picker.Item{ID: "svc:list-size:toggle", Title: size})
}

func (m *Model) openContainerSort() {
	m.openTasksPickerItems(" Sort Containers ", m.containerSortItems())
}

// onContainerSortPicked answers "svc:list-sort:<key>" / "svc:list-size:toggle".
func (m *Model) onContainerSortPicked(verb, key string) tea.Cmd {
	st := m.services
	cur, had := m.servicesCurrentRow()
	refresh := false
	switch verb {
	case "list-sort":
		st.containerSort = key
		if key == "size" && !st.containerSizes {
			st.containerSizes, refresh = true, true
		}
	case "list-size":
		st.containerSizes = !st.containerSizes
		refresh = st.containerSizes
		if !st.containerSizes && st.containerSort == "size" {
			st.containerSort = ""
		}
	default:
		return nil
	}
	st.touch()
	if had {
		for i, r := range st.rows() {
			if sameRow(r, cur) {
				st.cursor = i
				break
			}
		}
	}
	m.servicesClampCursor()
	if refresh {
		return m.containersRefresh("containers")
	}
	return nil
}

// sortGroupIdx orders one group's container indexes by the sort key
// (nil change for the default order).
func (st *servicesState) sortGroupIdx(idx []int) []int {
	if st.containerSort == "" || len(idx) < 2 {
		return idx
	}
	cs := make([]containers.Container, len(idx))
	for i, k := range idx {
		cs[i] = st.containerRows[k]
	}
	containers.SortContainers(cs, st.containerSort)
	rank := make(map[string]int, len(cs))
	for i, c := range cs {
		rank[c.ID] = i
	}
	out := append([]int(nil), idx...)
	sort.SliceStable(out, func(a, b int) bool {
		return rank[st.containerRows[out[a]].ID] < rank[st.containerRows[out[b]].ID]
	})
	return out
}
