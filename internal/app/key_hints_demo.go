package app

import (
	"time"

	"github.com/amin-jalali/termocode/internal/keyhint"
)

// demoHints is the canned editor of the developer screens (DevActionMsg
// "keyhintdemo"): a space leader and a few user mappings.
type demoHints struct{}

func (demoHints) Leader() string         { return "<space>" }
func (demoHints) Timeout() time.Duration { return 0 }
func (demoHints) Send(string)            {}
func (demoHints) Maps() []keyhint.Mapping {
	return []keyhint.Mapping{
		{LHS: "<Space>f", Desc: "Format the buffer"},
		{LHS: "<Space>x", Desc: "Show the query plan"},
		{LHS: "<Space>X", Desc: "EXPLAIN ANALYZE the statement"},
		{LHS: "<Space>ff", Desc: "Find files"},
		{LHS: "<Space>fg", Desc: "Search in files"},
		{LHS: "<Space>fr", Desc: "Recent files"},
		{LHS: "<Space>gs", Desc: "Stage hunk"},
		{LHS: "<Space>gp", Desc: "Push"},
		{LHS: "<Space>r", Desc: "Rename symbol"},
	}
}

// keyHintsDemo shows the popup: arg "" = the leader level, "group" = one
// level deeper, "notice" = the "no binding" hint.
func (m *Model) keyHintsDemo(arg string) {
	m.hintSrc = demoHints{}
	m.hints = &keyhint.Tracker{Leader: "<space>", Load: m.hintSrc.Maps}
	m.hints.Press("<space>")
	m.hints.Show()
	switch arg {
	case "group":
		m.hints.Press("f")
	case "notice":
		m.hints.Press("q")
	}
}
