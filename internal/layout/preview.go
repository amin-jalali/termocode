package layout

import (
	"fmt"
	"strings"
)

var viewLabels = map[string]string{
	"files": "Explorer", "search": "Search", "git": "Source Control", "run": "Run and Debug",
	"tests": "Testing", "containers": "Containers", "databases": "Databases", "itermoc": "Itermoc", "runs": "Runs",
}

var tabLabels = map[string]string{
	"terminal": "Terminal", "problems": "Problems", "output": "Output", "tests": "Tests", "debug-console": "Debug Console",
}

func plural(n int, one string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%d %ss", n, one)
}

// Preview says in short lines what opening l does. Sections the layout does
// not set are not listed: opening it leaves them alone.
func (l Layout) Preview() []string {
	var out []string
	if s := l.Sidebar; s != nil {
		switch {
		case !s.Open:
			out = append(out, "Sidebar: hidden")
		default:
			line := "Sidebar: shown"
			if v := viewLabels[s.View]; v != "" {
				line = "Sidebar: " + v
			}
			if s.Width > 0 {
				line += fmt.Sprintf(" (%d columns)", s.Width)
			}
			out = append(out, line)
		}
	}
	if p := l.Panel; p != nil {
		if !p.Open {
			out = append(out, "Bottom panel: closed")
		} else {
			var parts []string
			seen := map[string]bool{}
			for _, t := range p.Tabs {
				if !seen[t] && t != "terminal" {
					seen[t] = true
					parts = append(parts, tabLabels[t])
				}
			}
			if n := len(p.Terminals); n > 0 {
				parts = append(parts, plural(n, "terminal"))
				panes := 0
				for _, t := range p.Terminals {
					panes += len(t.Panes)
				}
				if panes > 0 {
					parts = append(parts, plural(panes, "split pane"))
				}
			} else if has(p.Tabs, "terminal") {
				parts = append(parts, "Terminal")
			}
			line := "Bottom panel: open"
			if len(parts) > 0 {
				line += " with " + strings.Join(parts, ", ")
			}
			if p.Rows > 0 {
				line += fmt.Sprintf(" (%d rows)", p.Rows)
			}
			out = append(out, line)
		}
	}
	if c := l.Chat; c != nil {
		line := "Chat panel: closed"
		if c.Open {
			line = "Chat panel: open"
			if c.Width > 0 {
				line += fmt.Sprintf(" (%d columns)", c.Width)
			}
		}
		out = append(out, line)
	}
	if e := l.Editor; e != nil {
		var parts []string
		if e.Split != "" {
			parts = append(parts, fmt.Sprintf("split %s into %s", e.Split, plural(max(len(e.Panes), 2), "window")))
		}
		if len(e.Files) > 0 {
			parts = append(parts, "opens "+plural(len(e.Files), "file"))
		}
		if len(parts) == 0 && e.Split == "" {
			parts = append(parts, "one window")
		}
		out = append(out, "Editor: "+strings.Join(parts, ", "))
	}
	if l.Focus != "" {
		out = append(out, "Focus: "+l.Focus)
	}
	if len(out) == 0 {
		out = append(out, "Changes nothing")
	}
	return out
}
