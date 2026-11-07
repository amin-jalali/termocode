package containers

import (
	"context"
	"sort"
	"sync"

	"github.com/amin-jalali/termocode/internal/crash"
)

// Several engines/hosts in one view (CTX-41): each Source is one engine
// on one host (a docker context or podman connection). ListAll asks all
// of them at the same time; a source that fails becomes a problem row and
// never hides the containers of the others.

// Source is one engine/host of a several-engines view.
type Source struct {
	Name   string // shown in the source column ("" = Label from the engine)
	Engine Engine
}

// Label is the name shown for s: its Name, else kind/context, else kind.
func (s Source) Label() string {
	if s.Name != "" {
		return s.Name
	}
	if s.Engine.Context != "" {
		return s.Engine.Kind + "/" + s.Engine.Context
	}
	return s.Engine.Kind
}

// SourceContainer is one container row and the source it came from.
type SourceContainer struct {
	Source string
	Container
}

// SourceProblem is a source that did not answer.
type SourceProblem struct {
	Source string
	Err    error
}

// MultiResult is the merged list of several sources.
type MultiResult struct {
	Containers []SourceContainer // source order, then name
	Problems   []SourceProblem   // source order
}

// ListAll lists the containers of every source at the same time.
func ListAll(ctx context.Context, sources []Source) MultiResult {
	type one struct {
		rows []Container
		err  error
	}
	got := make([]one, len(sources))
	var wg sync.WaitGroup
	for i := range sources {
		wg.Add(1)
		i := i
		crash.Go("containers.ListAll", func() {
			defer wg.Done()
			e := sources[i].Engine
			rows, err := e.ListContainers(ctx)
			got[i] = one{rows: rows, err: err}
		})
	}
	wg.Wait()
	var res MultiResult
	for i, g := range got {
		label := sources[i].Label()
		if g.err != nil {
			res.Problems = append(res.Problems, SourceProblem{Source: label, Err: g.err})
			continue
		}
		rows := append([]Container(nil), g.rows...)
		sort.SliceStable(rows, func(a, b int) bool { return multiSortName(rows[a]) < multiSortName(rows[b]) })
		for _, c := range rows {
			res.Containers = append(res.Containers, SourceContainer{Source: label, Container: c})
		}
	}
	return res
}

func multiSortName(c Container) string {
	if len(c.Names) > 0 {
		return c.Names[0]
	}
	return c.ID
}

// SourcesFromContexts is one source per context of base's engine (docker
// contexts or podman connections), the current one first, then by name.
// Contexts that report an error are left out.
func SourcesFromContexts(base Engine, ctxs []Context) []Source {
	list := make([]Context, 0, len(ctxs))
	for _, c := range ctxs {
		if c.Error == "" && c.Name != "" {
			list = append(list, c)
		}
	}
	sort.SliceStable(list, func(a, b int) bool {
		if list[a].Current != list[b].Current {
			return list[a].Current
		}
		return list[a].Name < list[b].Name
	})
	out := make([]Source, 0, len(list))
	for _, c := range list {
		e := base
		e.Context = c.Name
		out = append(out, Source{Name: c.Name, Engine: e})
	}
	return out
}
