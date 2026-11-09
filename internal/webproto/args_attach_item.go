package webproto

import (
	"bytes"
	"encoding/json"
	"slices"
	"unicode/utf8"
)

// An item attach (Add to AI Chat) is one thing of the page the user
// gives the AI as context: a database connection, table, column, rows or
// a cell, a host, a compose project or a container. Its Spec is one JSON
// object (ItemSpec): the item's exact address, in the names termocode's
// MCP tools take (conn, schema, table: db_schema, db_query; a container
// name: containers_inspect target), and a small summary the page read:
// a table's DDL, at most MaxItemRows rows. A value the user chose to
// mask arrives as "•••" and its column is named in Masked.

// The item kinds of ItemSpec.Item.
const (
	ItemDBConn    = "db_conn"
	ItemDBTable   = "db_table"
	ItemDBColumn  = "db_column"
	ItemDBRows    = "db_rows"
	ItemDBCell    = "db_cell"
	ItemHost      = "host"
	ItemProject   = "compose_project"
	ItemContainer = "container"
)

// Limits of an item attach.
const (
	MaxItemSpec = MaxAttachBytes // bytes of the spec
	MaxItemRows = 50
	MaxItemCols = 256
	MaxItemDDL  = 16 << 10
)

// ItemSpec is the spec of an item attach.
type ItemSpec struct {
	Item      string `json:"item"`
	Host      string `json:"host,omitempty"`      // the engine context (containers.servers name)
	Project   string `json:"project,omitempty"`   // compose project
	Container string `json:"container,omitempty"` // container name
	Conn      string `json:"conn,omitempty"`      // database connection id (db_connections)
	Schema    string `json:"schema,omitempty"`
	Table     string `json:"table,omitempty"`
	Column    string `json:"column,omitempty"`
	// Key names the columns that find a row; Columns and Rows are the
	// rows the user picked (nil: NULL).
	Key     []string    `json:"key,omitempty"`
	Columns []string    `json:"columns,omitempty"`
	Rows    [][]*string `json:"rows,omitempty"`
	Masked  []string    `json:"masked,omitempty"`
	DDL     string      `json:"ddl,omitempty"` // db_table: its CREATE statement
}

// ParseItemSpec reads and checks an item attach's spec.
func ParseItemSpec(s string) (ItemSpec, error) {
	var sp ItemSpec
	if s == "" || len(s) > MaxItemSpec {
		return sp, invalid("bad item spec")
	}
	d := json.NewDecoder(bytes.NewReader([]byte(s)))
	d.DisallowUnknownFields()
	if err := d.Decode(&sp); err != nil || d.More() {
		return sp, invalid("bad item spec")
	}
	return sp, sp.check()
}

func (s *ItemSpec) check() error {
	name := func(v string) bool { return validName(v, MaxLabel) }
	none := func(v ...string) bool {
		for _, x := range v {
			if x != "" {
				return false
			}
		}
		return true
	}
	rows := len(s.Columns) > 0 || len(s.Rows) > 0 || len(s.Key) > 0 || len(s.Masked) > 0
	ok := false
	switch s.Item {
	case ItemDBConn:
		ok = validName(s.Conn, MaxID) && none(s.Host, s.Project, s.Container, s.Table, s.Column, s.DDL) && !rows
	case ItemDBTable:
		ok = validName(s.Conn, MaxID) && name(s.Table) && none(s.Host, s.Project, s.Container, s.Column) && !rows && len(s.DDL) <= MaxItemDDL && utf8.ValidString(s.DDL)
	case ItemDBColumn:
		ok = validName(s.Conn, MaxID) && name(s.Table) && name(s.Column) && none(s.Host, s.Project, s.Container, s.DDL) && !rows
	case ItemDBRows, ItemDBCell:
		ok = validName(s.Conn, MaxID) && name(s.Table) && none(s.Host, s.Project, s.Container, s.DDL) && s.validRows()
		if s.Item == ItemDBRows {
			ok = ok && s.Column == ""
		} else {
			ok = ok && len(s.Rows) == 1 && slices.Contains(s.Columns, s.Column)
		}
	case ItemHost:
		ok = name(s.Host) && none(s.Project, s.Container, s.Conn, s.Schema, s.Table, s.Column, s.DDL) && !rows
	case ItemProject:
		ok = name(s.Host) && name(s.Project) && none(s.Container, s.Conn, s.Schema, s.Table, s.Column, s.DDL) && !rows
	case ItemContainer:
		ok = name(s.Host) && name(s.Container) && (s.Project == "" || name(s.Project)) && none(s.Conn, s.Schema, s.Table, s.Column, s.DDL) && !rows
	}
	if !ok || (s.Schema != "" && !name(s.Schema)) {
		return invalid("bad %s item", s.Item)
	}
	return nil
}

// validRows: 1..MaxItemRows rows as wide as Columns; Key and Masked
// name columns of them.
func (s *ItemSpec) validRows() bool {
	if len(s.Columns) == 0 || len(s.Columns) > MaxItemCols || len(s.Rows) == 0 || len(s.Rows) > MaxItemRows {
		return false
	}
	for _, c := range s.Columns {
		if !validName(c, MaxLabel) {
			return false
		}
	}
	for _, r := range s.Rows {
		if len(r) != len(s.Columns) {
			return false
		}
		for _, v := range r {
			if v != nil && !utf8.ValidString(*v) {
				return false
			}
		}
	}
	for _, list := range [][]string{s.Key, s.Masked} {
		for _, c := range list {
			if !slices.Contains(s.Columns, c) {
				return false
			}
		}
	}
	return true
}
