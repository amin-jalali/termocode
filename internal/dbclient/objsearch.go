package dbclient

import (
	"sort"
	"strings"
)

// ObjectSource is the loaded schemas of one connection, for object
// search (DBX-14).
type ObjectSource struct {
	Conn    string
	Schemas []CanonSchema
}

// ObjectHit is one object that matches a search. Kind is table, view,
// column or routine; Parent is the table of a column.
type ObjectHit struct {
	Conn, Schema, Kind, Name, Parent string
	rank                             int
}

// Path is the object name, with its table for a column.
func (h ObjectHit) Path() string {
	if h.Parent != "" {
		return h.Parent + "." + h.Name
	}
	return h.Name
}

// ListObjects is every table, view, column and routine of sources,
// sorted by connection, schema and path: the list a picker filters.
func ListObjects(sources []ObjectSource) []ObjectHit {
	var out []ObjectHit
	for _, src := range sources {
		for _, s := range src.Schemas {
			for _, t := range s.Tables {
				out = append(out, ObjectHit{Conn: src.Conn, Schema: s.Schema, Kind: "table", Name: t.Name})
				for _, c := range t.Columns {
					out = append(out, ObjectHit{Conn: src.Conn, Schema: s.Schema, Kind: "column", Name: c.Name, Parent: t.Name})
				}
			}
			for _, v := range s.Views {
				out = append(out, ObjectHit{Conn: src.Conn, Schema: s.Schema, Kind: "view", Name: v.Name})
			}
			for _, r := range s.Routines {
				out = append(out, ObjectHit{Conn: src.Conn, Schema: s.Schema, Kind: "routine", Name: r.Name})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Conn != b.Conn {
			return a.Conn < b.Conn
		}
		if a.Schema != b.Schema {
			return a.Schema < b.Schema
		}
		return a.Path() < b.Path()
	})
	return out
}

// SearchObjects finds the objects of sources whose name holds query (any
// case): exact names first, then names that start with it, then the
// rest; ties by connection, schema and path. limit <= 0 means no limit.
func SearchObjects(sources []ObjectSource, query string, limit int) []ObjectHit {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	var out []ObjectHit
	add := func(h ObjectHit) {
		n := strings.ToLower(h.Name)
		switch {
		case n == q:
			h.rank = 0
		case strings.HasPrefix(n, q):
			h.rank = 1
		case strings.Contains(n, q):
			h.rank = 2
		default:
			return
		}
		out = append(out, h)
	}
	for _, src := range sources {
		for _, s := range src.Schemas {
			for _, t := range s.Tables {
				add(ObjectHit{Conn: src.Conn, Schema: s.Schema, Kind: "table", Name: t.Name})
				for _, c := range t.Columns {
					add(ObjectHit{Conn: src.Conn, Schema: s.Schema, Kind: "column", Name: c.Name, Parent: t.Name})
				}
			}
			for _, v := range s.Views {
				add(ObjectHit{Conn: src.Conn, Schema: s.Schema, Kind: "view", Name: v.Name})
			}
			for _, r := range s.Routines {
				add(ObjectHit{Conn: src.Conn, Schema: s.Schema, Kind: "routine", Name: r.Name})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.rank != b.rank {
			return a.rank < b.rank
		}
		if a.Conn != b.Conn {
			return a.Conn < b.Conn
		}
		if a.Schema != b.Schema {
			return a.Schema < b.Schema
		}
		return a.Path() < b.Path()
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}
