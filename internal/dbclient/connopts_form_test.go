package dbclient

import (
	"reflect"
	"testing"
)

// DBX-28: every option has a form field; a field round-trips through
// its text form (lists are comma lists).
func TestConnOptionFieldsRoundTrip(t *testing.T) {
	var o ConnOptions
	in := map[string]string{
		"initSQL": "SET work_mem = '64MB'", "schema": "app, public", "timeZone": "UTC",
		"isolation": "read committed", "txMode": "manual", "idleMinutes": "15",
		"socket": "/var/run/postgresql", "databases": "app*, main", "schemas": "public",
		"objects": "!tmp_*", "readOnlyTables": "public.audit_*",
	}
	if len(ConnOptionFields) != len(in) {
		t.Fatalf("fields %d, want %d", len(ConnOptionFields), len(in))
	}
	for _, f := range ConnOptionFields {
		v, ok := in[f.ID]
		if !ok {
			t.Fatalf("unexpected field %s", f.ID)
		}
		if err := o.SetField(f.ID, v); err != nil {
			t.Fatalf("%s: %v", f.ID, err)
		}
	}
	if o.IdleMinutes != 15 || !reflect.DeepEqual(o.Databases, []string{"app*", "main"}) || o.Schema != "app, public" {
		t.Fatalf("options %+v", o)
	}
	if got := o.Field("databases"); got != "app*, main" {
		t.Fatalf("databases field %q", got)
	}
	if got := o.Field("idleMinutes"); got != "15" {
		t.Fatalf("idleMinutes field %q", got)
	}
	if err := o.SetField("idleMinutes", "soon"); err == nil {
		t.Fatal("a word was taken as minutes")
	}
	if err := o.SetField("nope", "x"); err == nil {
		t.Fatal("an unknown field was taken")
	}
	if err := o.SetField("databases", ""); err != nil || o.Databases != nil {
		t.Fatalf("empty list: %v %v", err, o.Databases)
	}
}

// DBX-28: SetConnOptions checks the options for the driver, keeps the
// input list as is and drops an empty options block.
func TestSetConnOptions(t *testing.T) {
	cs := []Conn{{ID: "pg", Driver: "postgres", Host: "h", Port: 5432}}
	out, err := SetConnOptions(cs, "pg", ConnOptions{TxMode: "manual"})
	if err != nil || out[0].Options == nil || out[0].Options.TxMode != "manual" {
		t.Fatalf("set: %v %+v", err, out)
	}
	if cs[0].Options != nil {
		t.Fatal("input list changed")
	}
	if _, err := SetConnOptions(cs, "pg", ConnOptions{Isolation: "chaos"}); err == nil {
		t.Fatal("bad isolation saved")
	}
	out, err = SetConnOptions(out, "pg", ConnOptions{})
	if err != nil || out[0].Options != nil {
		t.Fatalf("empty options kept: %v %+v", err, out[0].Options)
	}
	if _, err := SetConnOptions(cs, "nope", ConnOptions{}); err == nil {
		t.Fatal("unknown id saved")
	}
}
