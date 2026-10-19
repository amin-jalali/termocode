package dbclient

import (
	"fmt"
	"time"
)

type Column struct {
	Name, Type, Table string
	PK                bool
}
type Cell struct {
	Text string
	Null bool
	Kind string
}

func (c Cell) String() string {
	if c.Null {
		return "<null>"
	}
	return c.Text
}

type Result struct {
	Columns   []Column
	Rows      [][]Cell
	Affected  int64
	Elapsed   time.Duration
	Truncated bool
	Notices   []string
	Err       error
	Key       []int
	Editable  bool
	Why       string
}

func CellOf(v any) Cell {
	switch x := v.(type) {
	case nil:
		return Cell{Null: true}
	case []byte:
		return Cell{Text: fmt.Sprintf("0x%x", x), Kind: "binary"}
	case time.Time:
		return Cell{Text: x.Format(time.RFC3339Nano), Kind: "time"}
	default:
		return Cell{Text: fmt.Sprint(v)}
	}
}
