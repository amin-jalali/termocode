package run

import (
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/workflow"
)

// frame is one step list being run: the run's own list, an item of an
// each, or an else list (which shares its owner's frame).
type frame struct {
	parent *frame
	item   *Item // nil at the top
	env    ItemEnv
	// states is where this list's step states live (RunFile.Steps or
	// ItemState.Steps).
	states map[string]*StepState
	// steps holds the outputs of steps with an id, for steps.<id>.
	steps map[string]any
	// lists holds the item lists of source / plan / filter steps by id.
	lists  map[string][]Item
	cur    []Item // the current list (the last source / plan / filter)
	hasCur bool
}

func newFrame(parent *frame, item *Item, states map[string]*StepState) *frame {
	return &frame{parent: parent, item: item, states: states, steps: map[string]any{}, lists: map[string][]Item{}}
}

// current is the nearest current list.
func (f *frame) current() []Item {
	for x := f; x != nil; x = x.parent {
		if x.hasCur {
			return x.cur
		}
	}
	return nil
}

func (f *frame) setCurrent(items []Item) { f.cur, f.hasCur = items, true }

// list finds the item list of a step id in this frame or above.
func (f *frame) list(id string) ([]Item, bool) {
	for x := f; x != nil; x = x.parent {
		if l, ok := x.lists[id]; ok {
			return l, true
		}
	}
	return nil, false
}

func (f *frame) nearestItem() *Item {
	for x := f; x != nil; x = x.parent {
		if x.item != nil {
			return x.item
		}
	}
	return nil
}

func (f *frame) nearestEnv() ItemEnv {
	for x := f; x != nil; x = x.parent {
		if x.item != nil {
			return x.env
		}
	}
	return ItemEnv{}
}

// itemPath is the keys of the items from the top down ("" at the top).
func (f *frame) itemPath() string {
	var keys []string
	for x := f; x != nil; x = x.parent {
		if x.item != nil {
			keys = append(keys, x.item.Key)
		}
	}
	for i, j := 0, len(keys)-1; i < j; i, j = i+1, j-1 {
		keys[i], keys[j] = keys[j], keys[i]
	}
	return strings.Join(keys, "/")
}

// stepKey names a step in the run file and events: its id, or line<N>.
func stepKey(s *workflow.Step) string {
	if s.ID != "" {
		return s.ID
	}
	return "line" + strconv.Itoa(s.Line)
}

// itemScope is the item as expressions see it: its object fields plus
// key and plan; any other value is under value.
func itemScope(it Item) map[string]any {
	m := map[string]any{}
	if obj, ok := it.Data.(map[string]any); ok {
		maps.Copy(m, obj)
	} else {
		m["value"] = it.Data
	}
	m["key"] = it.Key
	if it.Plan != nil {
		labels := make([]any, len(it.Plan.Labels))
		for i, l := range it.Plan.Labels {
			labels[i] = l
		}
		var skip any
		if it.Plan.Skip != "" {
			skip = it.Plan.Skip
		}
		m["plan"] = map[string]any{"rank": float64(it.Plan.Rank), "skip": skip, "labels": labels}
	}
	return m
}

func listScope(items []Item) []any {
	out := make([]any, len(items))
	for i, it := range items {
		out[i] = itemScope(it)
	}
	return out
}

// itemsFrom turns an evaluated list back into items: an object with a
// string "key" keeps it, others are keyed by place.
func itemsFrom(v any) ([]Item, error) {
	list, ok := v.([]any)
	if !ok {
		if v == nil {
			return nil, nil
		}
		return nil, fmt.Errorf("items is not a list")
	}
	out := make([]Item, 0, len(list))
	for i, x := range list {
		it := Item{Key: strconv.Itoa(i + 1), Data: x}
		if m, ok := x.(map[string]any); ok {
			if k, ok := m["key"].(string); ok && k != "" {
				it.Key = k
			}
		}
		out = append(out, it)
	}
	return out, nil
}

// scope builds the expression scope of a frame.
func (r *runner) scope(f *frame) map[string]any {
	var chain []*frame
	for x := f; x != nil; x = x.parent {
		chain = append(chain, x)
	}
	steps := map[string]any{}
	for i := len(chain) - 1; i >= 0; i-- {
		maps.Copy(steps, chain[i].steps)
	}
	m := map[string]any{
		"vars":     r.vars,
		"run":      r.runInfo,
		"workflow": map[string]any{"name": r.file.Name, "hash": r.file.Hash},
		"steps":    steps,
	}
	if it := f.nearestItem(); it != nil {
		m["item"] = itemScope(*it)
	}
	if f.current() != nil {
		m["items"] = listScope(f.current())
	}
	return m
}
