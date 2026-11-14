package app

import (
	"strings"
	"testing"
)

// The shotkit result demos put the page each golden needs.
func TestCovDBResultDemoVariants(t *testing.T) {
	for _, c := range []struct {
		variant string
		check   func(*Model) string
	}{
		{"result", func(m *Model) string {
			if r := m.dbConsole().shown; r == nil || len(r.result.Rows) != 30 || !r.result.Rows[2][1].Null {
				return "no 30-row result with a NULL"
			}
			return ""
		}},
		{"table", func(m *Model) string {
			if r := m.dbConsole().shown; r.tv == nil || r.tv.offset != 100 || !strings.Contains(r.sql, `"public"."users"`) {
				return "no data view"
			}
			return ""
		}},
		{"transposed", func(m *Model) string {
			if s := m.dbConsole(); !s.trans || s.trow != 1 {
				return "not transposed"
			}
			return ""
		}},
		{"edits", func(m *Model) string {
			s := m.dbConsole()
			if len(s.tabs) != 2 || !s.tabs[0].pinned || s.curTab().edits.count() != 4 || !m.databases.txOpen("local-pg") {
				return "no pending changes"
			}
			return ""
		}},
		{"submit", func(m *Model) string {
			if !m.confirmOpen {
				return "no submit card: " + cdbToast(m)
			}
			return ""
		}},
		{"describe", func(m *Model) string {
			if r := m.dbConsole().shown; r.describe == nil || len(r.result.Rows) == 0 {
				return "no description"
			}
			return ""
		}},
		{"ddl", func(m *Model) string { return "" }},
		{"export", func(m *Model) string { return "" }},
		{"import", func(m *Model) string {
			if m.dbConsole().imp == nil || !m.confirmOpen {
				return "no import card"
			}
			return ""
		}},
		{"messages", func(m *Model) string {
			if s := m.dbConsole(); s.view != dbViewMessages || s.shown.result.Affected != 1 {
				return "no messages view"
			}
			return ""
		}},
		{"error", func(m *Model) string {
			if s := m.dbConsole(); s.view != dbViewMessages || s.shown.result.Err == nil {
				return "no error"
			}
			return ""
		}},
		{"plan", func(m *Model) string {
			if r := m.dbConsole().shown; r.plan == nil || len(r.plan.parsed.Lines) == 0 {
				return "no plan"
			}
			return ""
		}},
		{"plan-card", func(m *Model) string { return "" }},
	} {
		t.Run(c.variant, func(t *testing.T) {
			m := cdbModel(t)
			if c.variant == "submit" {
				m = cdbRealModel(t) // the page drawn in the editor looks the edit target up
			}
			m.databaseResultDemo(c.variant)
			if why := c.check(m); why != "" {
				t.Fatal(why)
			}
		})
	}
}
