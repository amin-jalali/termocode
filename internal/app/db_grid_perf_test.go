package app

import "testing"

// BenchmarkDatabasesGridPage100k is one page change (] / [) in a result
// of 100 000 rows: the page after the next is formatted.
func BenchmarkDatabasesGridPage100k(b *testing.B) {
	b.Setenv("XDG_CONFIG_HOME", b.TempDir())
	m := dbGridModel(dbWideResult(100000))
	s := m.dbConsole()
	_ = m.databasePage()
	pages := s.pageCount()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.page = (i * 7) % pages
		_ = m.databasePage()
	}
}
