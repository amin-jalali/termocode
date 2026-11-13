package app

import (
	"strings"
	"testing"
)

// DBX-66: a cloud warehouse row in the add picker opens the URL prompt
// with its own URL shape (account, warehouse, project or region go in
// the URL, which a host/port form cannot ask), takes only a URL of that
// engine, then asks for its token or key.
func TestDBAddWarehouseAsksItsURL(t *testing.T) {
	for _, tc := range []struct{ id, url, param, want string }{
		{"snowflake", "snowflake://ada@xy12345.snowflakecomputing.com/ANALYTICS/PUBLIC?warehouse=WH", "warehouse", "WH"},
		{"databricks", "databricks://dbc-1.cloud.databricks.com/main/sales?warehouse=wh-1", "warehouse", "wh-1"},
		{"bigquery", "bigquery://my-proj?location=EU", "location", "EU"},
		{"athena", "athena://AKIDTEST@athena.eu-west-1.amazonaws.com/sales?output=s3://b/r/", "region", "eu-west-1"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			var row bool
			for _, it := range dbTypePickerItems(false) {
				row = row || it.ID == "engine:"+tc.id
			}
			if !row {
				t.Fatalf("the add picker has no %s row", tc.id)
			}
			m, _ := covSvcDB(t, "")
			m.databaseStartAdd("engine:" + tc.id)
			if m.promptKind != promptKindDBAdd || !strings.HasPrefix(m.prompt.Value(), tc.id+"://") {
				t.Fatalf("no %s URL prompt: kind=%v value=%q", tc.id, m.promptKind, m.prompt.Value())
			}
			m.onDBPrompt(promptKindDBAdd, "postgres://u@h:5432/x")
			if !strings.Contains(covSvcToast(m), tc.id+"://") {
				t.Fatalf("another engine's URL was taken: %q", covSvcToast(m))
			}
			m.databaseStartAdd("engine:" + tc.id)
			m.onDBPrompt(promptKindDBAdd, tc.url)
			if c := m.databases.pendingConn; c.Driver != tc.id || c.ID == "" || c.Params[tc.param] != tc.want {
				t.Fatalf("pending = %+v", c)
			}
			if m.promptKind != promptKindDBPassword {
				t.Fatalf("no secret prompt after the URL: kind=%v", m.promptKind)
			}
		})
	}
}
