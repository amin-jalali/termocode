package app

import (
	"encoding/json"
	"os"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// GRAPH columns (GITX-39): the author initials, the date (age) and the
// short SHA of each GRAPH row can be shown or hidden. Author and date
// are on by default, SHA is off. The choice is saved in config.json as
// git_graph_columns (a list of the shown columns) and read at start.

// gitGraphCol is one GRAPH column; its bit in gitGraphCols means "not
// the default", so the zero value is author + date.
type gitGraphCol uint32

const (
	gitColAuthor gitGraphCol = 1 << iota // bit set: hidden
	gitColDate                           // bit set: hidden
	gitColSHA                            // bit set: shown
)

const gitGraphColsKey = "git_graph_columns"

// gitGraphCols holds the column bits (read by the row renderer).
var gitGraphCols atomic.Uint32

var gitGraphColNames = []struct {
	col  gitGraphCol
	name string
}{{gitColAuthor, "author"}, {gitColDate, "date"}, {gitColSHA, "sha"}}

// gitGraphColOn says whether column c is shown.
func gitGraphColOn(c gitGraphCol) bool {
	set := gitGraphCols.Load()&uint32(c) != 0
	if c == gitColSHA {
		return set
	}
	return !set
}

// gitGraphColToggle flips column c and says whether it is now shown.
func gitGraphColToggle(c gitGraphCol) bool {
	for {
		old := gitGraphCols.Load()
		if gitGraphCols.CompareAndSwap(old, old^uint32(c)) {
			return gitGraphColOn(c)
		}
	}
}

// gitGraphColsInit reads git_graph_columns from config.json; a missing
// or bad value means the default. The bits are a process global, so it
// resets them first: a choice made by an earlier model never leaks in.
func gitGraphColsInit() {
	gitGraphCols.Store(0)
	path, err := settingsConfigPath()
	if err != nil {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var s struct {
		Cols *[]string `json:"git_graph_columns"`
	}
	if json.Unmarshal(b, &s) != nil || s.Cols == nil {
		return
	}
	on := map[string]bool{}
	for _, n := range *s.Cols {
		on[n] = true
	}
	var bits uint32
	if !on["author"] {
		bits |= uint32(gitColAuthor)
	}
	if !on["date"] {
		bits |= uint32(gitColDate)
	}
	if on["sha"] {
		bits |= uint32(gitColSHA)
	}
	gitGraphCols.Store(bits)
}

// saveGitGraphCols writes the shown columns to config.json.
func saveGitGraphCols() error {
	shown := []string{}
	for _, c := range gitGraphColNames {
		if gitGraphColOn(c.col) {
			shown = append(shown, c.name)
		}
	}
	return saveHTTPSetting(gitGraphColsKey, shown)
}

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "git-graph-col-author", Title: "Git: Toggle Author Column in GRAPH"},
			{ID: "git-graph-col-date", Title: "Git: Toggle Date Column in GRAPH"},
			{ID: "git-graph-col-sha", Title: "Git: Toggle SHA Column in GRAPH"},
		},
		policies: map[string]aiActionPolicy{
			"git-graph-col-author": aiAuto(),
			"git-graph-col-date":   aiAuto(),
			"git-graph-col-sha":    aiAuto(),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			switch id {
			case "git-graph-col-author":
				return m.toggleGitGraphCol(gitColAuthor, "Author"), true
			case "git-graph-col-date":
				return m.toggleGitGraphCol(gitColDate, "Date"), true
			case "git-graph-col-sha":
				return m.toggleGitGraphCol(gitColSHA, "SHA"), true
			}
			return nil, false
		},
	})
}

// toggleGitGraphCol flips one GRAPH column, saves the choice and says so.
func (m *Model) toggleGitGraphCol(c gitGraphCol, label string) tea.Cmd {
	text := label + " column hidden in GRAPH"
	if gitGraphColToggle(c) {
		text = label + " column shown in GRAPH"
	}
	detail := ""
	if c != gitColDate {
		detail = "Shows when the sidebar is at least 40 cells wide"
	}
	if err := saveGitGraphCols(); err != nil {
		return m.pushToastDetail(toast.Errr, "Settings error", err.Error())
	}
	return m.pushToastDetail(toast.Info, text, detail)
}
