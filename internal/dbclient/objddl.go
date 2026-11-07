package dbclient

import (
	"errors"
	"fmt"
	"strings"
)

// More object kinds for Show DDL (DBX-13).
const (
	ObjectSequence ObjectKind = "sequence"
	ObjectMatView  ObjectKind = "materialized view"
)

// ObjectDDLQuery is the read-only query that returns the DDL of one
// object of any kind the tree shows: views, materialized views,
// routines, triggers and sequences. Names are bound, never spliced.
func ObjectDDLQuery(driver string, kind ObjectKind, schema, name string) (SourceQuery, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return SourceQuery{}, errors.New("object name is empty")
	}
	pg := normDriver(driver) == "postgres" || normDriver(driver) == "postgresql"
	switch kind {
	case ObjectSequence:
		if !pg {
			break
		}
		return SourceQuery{SQL: `SELECT 'CREATE SEQUENCE ' || quote_ident(schemaname) || '.' || quote_ident(sequencename) || E'\n  AS ' || data_type::text || E'\n  START WITH ' || start_value || E'\n  INCREMENT BY ' || increment_by || E'\n  MINVALUE ' || min_value || E'\n  MAXVALUE ' || max_value || E'\n  CACHE ' || cache_size || CASE WHEN cycle THEN E'\n  CYCLE' ELSE E'\n  NO CYCLE' END || ';' FROM pg_sequences WHERE schemaname = $1 AND sequencename = $2`, Args: []any{schema, name}}, nil
	case ObjectMatView:
		if !pg {
			break
		}
		return SourceQuery{SQL: `SELECT 'CREATE MATERIALIZED VIEW ' || quote_ident(schemaname) || '.' || quote_ident(matviewname) || E' AS\n' || definition FROM pg_matviews WHERE schemaname = $1 AND matviewname = $2`, Args: []any{schema, name}}, nil
	default:
		q := name
		if pg && schema != "" && kind != ObjectTrigger {
			q = PostgresDialect{}.QuoteIdent(schema) + "." + PostgresDialect{}.QuoteIdent(name)
		}
		return ObjectSourceQuery(driver, kind, q)
	}
	return SourceQuery{}, fmt.Errorf("showing the DDL of a %s is not supported for %s", kind, driver)
}
