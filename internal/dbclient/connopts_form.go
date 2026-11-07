package dbclient

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// ConnOptionField is one row of the connection options form.
type ConnOptionField struct {
	ID, Title, Hint string
}

// ConnOptionFields are the form rows, in the order the form shows them.
var ConnOptionFields = []ConnOptionField{
	{ID: "schema", Title: "Default schema", Hint: "PostgreSQL search_path (comma list) or MySQL database"},
	{ID: "timeZone", Title: "Time zone", Hint: "UTC, Europe/Berlin, +00:00"},
	{ID: "isolation", Title: "Isolation level", Hint: "read uncommitted, read committed, repeatable read, serializable"},
	{ID: "txMode", Title: "Transaction mode", Hint: "auto or manual"},
	{ID: "initSQL", Title: "Start SQL", Hint: "runs on every new session"},
	{ID: "idleMinutes", Title: "Idle disconnect (minutes)", Hint: "0 keeps the session open"},
	{ID: "socket", Title: "Unix socket", Hint: "absolute path (PostgreSQL: the socket folder)"},
	{ID: "databases", Title: "Show only databases", Hint: "comma list of globs; empty shows all"},
	{ID: "schemas", Title: "Show only schemas", Hint: "comma list of globs; empty shows all"},
	{ID: "objects", Title: "Object filter", Hint: "globs show, !glob hides"},
	{ID: "readOnlyTables", Title: "Read-only tables", Hint: "schema.table or table globs"},
}

func joinList(l []string) string { return strings.Join(l, ", ") }

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Field is the text form of one option ("" when it is not set).
func (o ConnOptions) Field(id string) string {
	switch id {
	case "initSQL":
		return o.InitSQL
	case "schema":
		return o.Schema
	case "timeZone":
		return o.TimeZone
	case "isolation":
		return o.Isolation
	case "txMode":
		return o.TxMode
	case "idleMinutes":
		if o.IdleMinutes == 0 {
			return ""
		}
		return strconv.Itoa(o.IdleMinutes)
	case "socket":
		return o.Socket
	case "databases":
		return joinList(o.Databases)
	case "schemas":
		return joinList(o.Schemas)
	case "objects":
		return joinList(o.Objects)
	case "readOnlyTables":
		return joinList(o.ReadOnlyTables)
	}
	return ""
}

// SetField sets one option from its text form; empty clears it. The
// whole set is checked by SetConnOptions.
func (o *ConnOptions) SetField(id, v string) error {
	v = strings.TrimSpace(v)
	switch id {
	case "initSQL":
		o.InitSQL = v
	case "schema":
		o.Schema = v
	case "timeZone":
		o.TimeZone = v
	case "isolation":
		o.Isolation = v
	case "txMode":
		o.TxMode = strings.ToLower(v)
	case "idleMinutes":
		if v == "" {
			o.IdleMinutes = 0
			return nil
		}
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return fmt.Errorf("idle minutes is a whole number, 0 or more")
		}
		o.IdleMinutes = n
	case "socket":
		o.Socket = v
	case "databases":
		o.Databases = splitList(v)
	case "schemas":
		o.Schemas = splitList(v)
	case "objects":
		o.Objects = splitList(v)
	case "readOnlyTables":
		o.ReadOnlyTables = splitList(v)
	default:
		return fmt.Errorf("unknown option %q", id)
	}
	return nil
}

// SetConnOptions sets the options of connection id after checking them
// for its driver. Empty options remove the block. It returns a new list.
func SetConnOptions(cs []Conn, id string, o ConnOptions) ([]Conn, error) {
	i := connIndex(cs, id)
	if i < 0 {
		return nil, fmt.Errorf("unknown connection %q", id)
	}
	if err := o.Check(cs[i].Driver); err != nil {
		return nil, err
	}
	out := cloneConns(cs)
	if reflect.DeepEqual(o, ConnOptions{}) {
		out[i].Options = nil
	} else {
		out[i].Options = &o
	}
	return out, nil
}
