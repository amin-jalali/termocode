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

// DialectFor is the SQL dialect of a driver name, or nil for a driver that
// does not speak SQL (redis) or is unknown.
func DialectFor(driver string) Dialect {
	switch normDriver(driver) {
	case "postgres", "postgresql":
		return PostgresDialect{}
	case "mysql":
		return MySQLDialect{}
	case "sqlite":
		return SQLiteDialect{}
	}
	return nil
}
