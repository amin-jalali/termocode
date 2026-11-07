package webproto

import (
	"sort"
	"strings"
)

// Views across projects (MOB-57). A view spec, a saved view and a pinned
// view carry the workspace key of their project (ws; "" is the home
// project). The page puts that key on the view's frames so the door
// (MOB-50) routes them to the project's own hub; a project that does not
// run answers "gone" and the pinned view says "asleep — Open" instead of
// data. The AI's view_show and the Views menu's project picker name a
// project by its id, its workspace key or its name; ProjectRef turns that
// into the key, from the projects topic, never from a path.

// Pinned view states on the phone's Home.
const (
	ViewWSLive    = "live"    // the project runs (or is this window's): the view runs
	ViewWSAsleep  = "asleep"  // the project is known but nothing runs there: "asleep — Open"
	ViewWSUnknown = "unknown" // the key is in no project row (forgotten folder): "Project gone"
)

// MaxProjectRef caps a project reference (an id, a key or a name).
const MaxProjectRef = 120

// ProjectRef finds the project a reference names, in this order: its id
// (ProjectIDLen hex), its workspace key (WSLen hex), its name (case
// does not matter). A name two projects share is refused with their ids,
// so the caller (the AI) can ask again by id. An empty reference is the
// home project (this window's, State "here").
func ProjectRef(projects []ProjectItem, ref string) (ProjectItem, error) {
	ref = strings.TrimSpace(ref)
	if ref != "" && !validName(ref, MaxProjectRef) {
		return ProjectItem{}, invalid("bad project: one line, at most 120 bytes")
	}
	if ref == "" {
		for _, p := range projects {
			if p.State == "here" {
				return p, nil
			}
		}
		return ProjectItem{}, invalid("no home project")
	}
	for _, p := range projects {
		if p.ID == ref || (p.WS != "" && p.WS == ref) {
			return p, nil
		}
	}
	var hits []ProjectItem
	for _, p := range projects {
		if strings.EqualFold(p.Name, ref) {
			hits = append(hits, p)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		names := make([]string, 0, len(projects))
		for _, p := range projects {
			names = append(names, p.Name)
		}
		sort.Strings(names)
		if len(names) > 12 {
			names = append(names[:12], "…")
		}
		return ProjectItem{}, invalid("%s", "no project "+quoteRef(ref)+"; known: "+strings.Join(names, ", "))
	}
	ids := make([]string, 0, len(hits))
	for _, p := range hits {
		ids = append(ids, p.ID+" ("+p.Where+")")
	}
	return ProjectItem{}, invalid("%s", "two or more projects are called "+quoteRef(ref)+"; name one by id: "+strings.Join(ids, ", "))
}

func quoteRef(s string) string { return "\"" + s + "\"" }

// ViewWSState says whether a view of project ws can run now: the home
// project ("" or home) always can; another project only while it runs.
func ViewWSState(projects []ProjectItem, home, ws string) string {
	if ws == "" || ws == home {
		return ViewWSLive
	}
	for _, p := range projects {
		if p.WS != ws {
			continue
		}
		if p.State == "here" || p.State == "running" {
			return ViewWSLive
		}
		return ViewWSAsleep
	}
	return ViewWSUnknown
}

// ViewWSField checks a spec's or a saved view's ws: "" (the home
// project) or a workspace key; anything else (a path, a name) is refused.
func ViewWSField(ws string) error {
	if !validWSField(ws) {
		return invalid("bad ws: a workspace key (16 hex digits) from the projects topic, or none")
	}
	return nil
}
