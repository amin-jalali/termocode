package picker

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func typeKey(m Model, k string) Model {
	var msg tea.KeyMsg
	switch k {
	case "bs":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	case " ":
		msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	m, _ = m.Update(msg)
	return m
}

// Spec: Given a query that only grew by the typed key,
// when the list filters again,
// then only the rows the shorter query matched are searched (a longer
// query can only match fewer rows), not the whole list.
func TestRefilterSearchesOnlyPreviousHits(t *testing.T) {
	var items []Item
	for i := 0; i < 1000; i++ {
		items = append(items, Item{ID: fmt.Sprint(i), Title: fmt.Sprintf("dir%d/file%d.go", i%7, i)})
	}
	m := NewItems("t", items)
	m = typeKey(m, "5")
	m = typeKey(m, ".")
	if m.fc.scanned >= len(items) || m.fc.scanned == 0 {
		t.Fatalf("second key searched %d rows of %d", m.fc.scanned, len(items))
	}
}

// Spec: the filtered list after any typing (keys, spaces, backspaces)
// equals filtering the full list from scratch with the same query, for
// flat and grouped lists, and after the items are replaced.
func TestRefilterIncrementalMatchesFresh(t *testing.T) {
	r := rand.New(rand.NewSource(9))
	words := []string{"src", "mod", "handler", "Main", "test", "a_b", "x"}
	mk := func(grouped bool) []Item {
		var items []Item
		for i := 0; i < 300; i++ {
			if grouped && i%50 == 0 {
				items = append(items, Item{Header: true, Group: fmt.Sprint(i / 50), Title: "G" + fmt.Sprint(i/50)})
			}
			title := words[r.Intn(len(words))] + "/" + words[r.Intn(len(words))] + fmt.Sprint(r.Intn(40))
			it := Item{ID: fmt.Sprint(i), Title: title}
			if grouped {
				it.Group = fmt.Sprint(i / 50)
			}
			items = append(items, it)
		}
		return items
	}
	keys := []string{"s", "r", "c", "m", "o", "d", "h", "a", "1", "2", "/", "_", "M", " ", "bs", "bs", "x"}
	for _, grouped := range []bool{false, true} {
		m := NewItems("t", mk(grouped))
		for step := 0; step < 600; step++ {
			if step%150 == 149 { // new items, same query
				m, _ = m.Update(itemsLoadedMsg{items: mk(grouped)})
			} else {
				m = typeKey(m, keys[r.Intn(len(keys))])
			}
			fresh := Model{items: m.items, input: m.input}
			fresh.refilter()
			if !reflect.DeepEqual(m.matches, fresh.matches) || m.cursor != fresh.cursor {
				t.Fatalf("grouped=%v step %d query %q: incremental filter differs from a fresh one", grouped, step, m.input)
			}
		}
	}
}

// BenchmarkFilter50kTyping types a query into a 50 000-row list.
func BenchmarkFilter50kTyping(b *testing.B) {
	var items []Item
	for d := 0; d < 500; d++ {
		for f := 0; f < 100; f++ {
			items = append(items, Item{ID: fmt.Sprint(d, f), Title: fmt.Sprintf("src/mod%03d/handler_%03d.go", d, f)})
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		m := NewItems("t", items)
		for _, r := range "mod42handler7" {
			m = typeKey(m, string(r))
		}
	}
}
