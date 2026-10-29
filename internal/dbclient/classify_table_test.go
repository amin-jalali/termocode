package dbclient

import "testing"

// The table name of a SELECT comes from its FROM, never from a FROM
// written inside a string.
func TestClassifyTableSkipsStringText(t *testing.T) {
	for _, tc := range []struct{ sql, dialect string }{
		{`SELECT 'FROM fake' FROM real_t`, "postgres"},
		{`SELECT 'x\'; FROM fake' FROM real_t`, "mysql"},
	} {
		_, sts := Classify(tc.sql, tc.dialect)
		if len(sts) != 1 || sts[0].Table != "real_t" {
			t.Errorf("Classify(%q) = %+v, want table real_t", tc.sql, sts)
		}
	}
}
