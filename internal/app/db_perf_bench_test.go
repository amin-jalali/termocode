package app

import (
	"fmt"
	"testing"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/dbclient"
)

// perfDBTree is a SQLite connection with 500 tables loaded, every 10th one
// expanded to its 20 columns (PERF-6).
func perfDBTree() *dbState {
	s := newDBState()
	s.loaded = true
	s.conns = []dbclient.Conn{{ID: "big", Driver: "sqlite", Path: "big.db"}}
	s.nodes["big"] = dbNode{kind: dbNodeConnection, name: "big", path: "big", conn: "big"}
	s.addIntrospection("big", dbclient.IntrospectionResult{Databases: []string{"main"}})
	db := "big/db/main"
	var objs []dbclient.SchemaObject
	for i := 0; i < 500; i++ {
		objs = append(objs, dbclient.SchemaObject{Name: fmt.Sprintf("t%03d", i), Kind: "table"})
	}
	s.addIntrospection(db, dbclient.IntrospectionResult{Objects: objs})
	for i := 0; i < 500; i += 10 {
		var cols []dbclient.Column
		for c := 0; c < 20; c++ {
			cols = append(cols, dbclient.Column{Name: fmt.Sprintf("col_%02d", c), Type: "TEXT", PK: c == 0})
		}
		s.addIntrospection(fmt.Sprintf("%s/Tables/t%03d", db, i), dbclient.IntrospectionResult{Columns: cols})
	}
	return s
}

func BenchmarkPerfDBTreeLoad(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		perfDBTree()
	}
}

func BenchmarkPerfDBTreeRows(b *testing.B) {
	s := perfDBTree()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.rows()
	}
}

func BenchmarkPerfDBTreeFilterKey(b *testing.B) {
	s := perfDBTree()
	m := &Model{databases: s, w: 40, h: 40, showExp: true, activity: activity.New()}
	texts := []string{"t", "t4", "t42", "t4", "t", ""}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.databaseSetFilter(texts[i%len(texts)])
		_ = m.renderDatabasesSidebar(40, 40)
	}
}

// One frame of the sidebar when nothing changed.
func BenchmarkPerfDBSidebarFrame(b *testing.B) {
	s := perfDBTree()
	m := Model{databases: s, w: 40, h: 40, showExp: true, activity: activity.New()}
	_ = m.renderDatabasesSidebar(40, 40)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.renderDatabasesSidebar(40, 40)
	}
}

// The completion schema of a 500-table connection.
func BenchmarkPerfDBSchemaPayload(b *testing.B) {
	s := perfDBTree()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.schemaPayload(s.conns[0])
	}
}

// The sidebar scrolls exactly as far as before the one-sum scroll: until
// the cursor row (wrapped notes take several lines) fits.
func TestDatabaseSidebarScrollFitsCursorWithWrappedRows(t *testing.T) {
	s := perfDBTree()
	db := "big/db/main/Tables/t005"
	n := s.nodes[db]
	n.problem = "a long problem text that wraps over several lines of the narrow sidebar"
	s.nodes[db] = n
	m := Model{databases: s, w: 30, h: 20, showExp: true, activity: activity.New()}
	rows := s.rows()
	for _, cursor := range []int{0, 3, 40, 41, 42, 43, 200, len(rows) - 1} {
		s.cursor, s.scroll = cursor, 0
		m.renderDatabasesSidebarRaw(30, 20, true)
		available := 20 - 4 - 2
		want := 0
		for want < cursor && dbRowsHeight(rows, want, cursor+1, 30) > available {
			want++
		}
		if s.scroll != want {
			t.Fatalf("cursor %d: scroll %d, want %d", cursor, s.scroll, want)
		}
	}
}
