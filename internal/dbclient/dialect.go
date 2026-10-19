package dbclient

// Dialect supplies the SQL syntax and catalog operations used by sessions.
// It deliberately has no connection or process ownership.
type Dialect interface {
	QuoteIdent(string) string
	Placeholder(int) string
	Limit(limit, offset int) string
	ReadOnlyBegin() string
	CatalogQueries() map[string]string
	DDL(table string) (string, []any)
	Explain(sql string, analyze bool) string
	CountSQL(table string) string
}
