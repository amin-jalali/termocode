package views

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/blocks"
)

// Fetch gets a view's rows on the laptop for a resolved spec (its args).
// It reads a topic snapshot, calls an op's handler in-process under the
// same policy row, or runs a read-only tool. It may give more rows than
// the spec shows (the spec is applied after), at most MaxFetchRows.
type Fetch func(ctx context.Context, spec blocks.ViewSpec) ([]Row, error)

// Registry holds the views of one hub, in registration order.
type Registry struct {
	mu    sync.RWMutex
	defs  map[string]*entry
	order []string
	now   func() time.Time
}

type entry struct {
	def   Def
	fetch Fetch
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{defs: map[string]*entry{}, now: time.Now}
}

// Add registers a view. A def that fails Check, a repeated id or a nil
// fetch is refused.
func (r *Registry) Add(d Def, fetch Fetch) error {
	if err := d.Check(); err != nil {
		return err
	}
	if fetch == nil {
		return invalid("view %s: no fetch", d.ID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.defs[d.ID] != nil {
		return invalid("view %s registered twice", d.ID)
	}
	r.defs[d.ID] = &entry{def: d, fetch: fetch}
	r.order = append(r.order, d.ID)
	return nil
}

// Def returns the view with id.
func (r *Registry) Def(id string) (Def, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e := r.defs[id]
	if e == nil {
		return Def{}, false
	}
	return e.def, true
}

// Defs returns every view, in registration order.
func (r *Registry) Defs() []Def {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Def, 0, len(r.order))
	for _, id := range r.order {
		out = append(out, r.defs[id].def)
	}
	return out
}

// unknown names the views a wrong id may have meant: the same feature's
// views, else every view (cut to a short line).
func (r *Registry) unknown(id string) error {
	feature, _, _ := strings.Cut(id, ".")
	var near, all []string
	for _, v := range r.order {
		all = append(all, v)
		if r.defs[v].def.Feature == feature || strings.HasPrefix(v, feature+".") {
			near = append(near, v)
		}
	}
	list := near
	if len(list) == 0 {
		list = all
	}
	slices.Sort(list)
	s := strings.Join(list, ", ")
	if len(s) > 400 {
		s = s[:400] + "…"
	}
	if s == "" {
		s = "none"
	}
	return invalid("no view %q (views: %s)", id, s)
}

// Resolve checks a spec against its view and fills the defaults.
func (r *Registry) Resolve(s blocks.ViewSpec) (Def, blocks.ViewSpec, error) {
	if err := s.Validate(); err != nil {
		return Def{}, blocks.ViewSpec{}, err
	}
	r.mu.RLock()
	e := r.defs[s.View]
	if e == nil {
		err := r.unknown(s.View)
		r.mu.RUnlock()
		return Def{}, blocks.ViewSpec{}, err
	}
	d := e.def
	r.mu.RUnlock()
	out, err := Resolve(&d, s)
	return d, out, err
}

// Run checks the spec, fetches the rows on the laptop and renders the
// block with id. Only row layouts are rendered here; a view whose layout
// is a diff, a text or a board builds its block in its own hub code.
func (r *Registry) Run(ctx context.Context, id string, s blocks.ViewSpec) (blocks.Block, error) {
	d, spec, err := r.Resolve(s)
	if err != nil {
		return blocks.Block{}, err
	}
	r.mu.RLock()
	fetch := r.defs[d.ID].fetch
	r.mu.RUnlock()
	rows, err := fetch(ctx, spec)
	if err != nil {
		return blocks.Block{}, err
	}
	cutRows := len(rows) > MaxFetchRows
	if cutRows {
		rows = rows[:MaxFetchRows]
	}
	return Render(id, &d, spec, rows, cutRows, r.now().UnixMilli())
}
