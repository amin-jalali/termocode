package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/tracker"
)

// The shared task list (CMP-81): plain Markdown files in
// <main checkout>/.termocode/tasks/ that agents (termocode task …) and
// people both edit. Tracker: Show Tasks… lists them, open ones first;
// a pick opens its file or sets its status. Tracker: Add Task… asks a
// title. Task text is shown as plain text, never run.

const (
	trackerShowPaletteID = "tracker-show"
	trackerAddPaletteID  = "tracker-add"
)

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: trackerShowPaletteID, Title: "Tracker: Show Tasks…", Hint: "shared with agents"},
			{ID: trackerAddPaletteID, Title: "Tracker: Add Task…"},
		},
		policies: map[string]aiActionPolicy{
			trackerShowPaletteID: aiNever(aiNoPicker),
			trackerAddPaletteID:  aiNever(aiNoPicker),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			switch id {
			case trackerShowPaletteID:
				return m.openTracker(), true
			case trackerAddPaletteID:
				return m.trackerAdd(), true
			}
			return nil, false
		},
	})
}

// trackerDir is where the tasks are (a var: tests).
var trackerDir = tracker.Dir

// openTracker reads the tasks off the UI loop, then lists them.
func (m *Model) openTracker() tea.Cmd {
	root, err := m.rootDir()
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		dir := trackerDir(root)
		ts, err := tracker.List(dir)
		return gitxDoneMsg{apply: func(m *Model) tea.Cmd {
			if err != nil {
				return m.gitxFail("Could not read the tasks", err)
			}
			return m.onTrackerList(dir, ts)
		}}
	}
}

// trackerOrder is the list order: doing, blocked, todo, then done.
func trackerOrder(s string) int {
	switch s {
	case tracker.Doing:
		return 0
	case tracker.Blocked:
		return 1
	case tracker.Todo:
		return 2
	}
	return 3
}

// trackerItems are the picker rows (pure: tests).
func trackerItems(ts []tracker.Task) []picker.Item {
	sorted := append([]tracker.Task(nil), ts...)
	for i := 1; i < len(sorted); i++ { // stable insertion sort by status
		for j := i; j > 0 && trackerOrder(sorted[j].Status) < trackerOrder(sorted[j-1].Status); j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	dot := theme.ASCIIText(" · ")
	items := make([]picker.Item, 0, len(sorted)+1)
	for _, t := range sorted {
		hint := t.Status
		if t.Owner != "" {
			hint += dot + t.Owner
		}
		items = append(items, picker.Item{ID: "task-" + strconv.Itoa(t.ID), Title: "#" + strconv.Itoa(t.ID) + " " + t.Title, Hint: hint})
	}
	return append(items, picker.Item{ID: "add", Title: "Add Task…", Hint: "a new todo"})
}

func (m *Model) onTrackerList(dir string, ts []tracker.Task) tea.Cmd {
	m.gitxPick(" Tasks ", trackerItems(ts), func(m *Model, id string) tea.Cmd {
		if id == "add" {
			return m.trackerAddIn(dir, "")
		}
		for _, t := range ts {
			if id == "task-"+strconv.Itoa(t.ID) {
				return m.openTrackerTask(dir, t)
			}
		}
		return nil
	})
	return nil
}

// openTrackerTask: open the file, or set another status.
func (m *Model) openTrackerTask(dir string, t tracker.Task) tea.Cmd {
	ref := "#" + strconv.Itoa(t.ID)
	items := []picker.Item{{ID: "open", Title: "Open the Task File", Hint: "notes and text"}}
	for _, s := range tracker.Statuses {
		if s != t.Status {
			items = append(items, picker.Item{ID: "status-" + s, Title: "Set Status: " + s})
		}
	}
	items = append(items, picker.Item{ID: "copy", Title: "Copy the Claim Command", Hint: "termocode task claim " + strconv.Itoa(t.ID)})
	m.gitxPick(" "+ref+" "+t.Title+" ", items, func(m *Model, id string) tea.Cmd {
		switch {
		case id == "open":
			m.openFileAtLine(t.File, 1)
			return nil
		case id == "copy":
			m.copyToClipboard("termocode task claim " + strconv.Itoa(t.ID))
			return m.gitxInfo("Copied the claim command for " + ref)
		case strings.HasPrefix(id, "status-"):
			s := strings.TrimPrefix(id, "status-")
			return func() tea.Msg {
				_, err := tracker.SetStatus(dir, t.ID, s, "", "", clock.Now())
				return gitxDoneMsg{apply: func(m *Model) tea.Cmd {
					if err != nil {
						return m.gitxFail("Status not set", err)
					}
					return m.gitxInfo(ref + " is " + s)
				}}
			}
		}
		return nil
	})
	return nil
}

// trackerAdd asks a title and adds a todo task.
func (m *Model) trackerAdd() tea.Cmd {
	root, err := m.rootDir()
	if err != nil {
		return nil
	}
	return m.trackerAddIn("", root)
}

// trackerAddIn adds to dir ("": the tasks folder of root, found off the
// UI loop).
func (m *Model) trackerAddIn(dir, root string) tea.Cmd {
	m.gitxPrompt("Add Task", "Title (agents see it with termocode task list)", "", func(m *Model, title string) tea.Cmd {
		if strings.TrimSpace(title) == "" {
			return nil
		}
		return func() tea.Msg {
			d := dir
			if d == "" {
				d = trackerDir(root)
			}
			t, err := tracker.Add(d, title, "", clock.Now())
			return gitxDoneMsg{apply: func(m *Model) tea.Cmd {
				if err != nil {
					return m.gitxFail("Task not added", err)
				}
				return m.gitxInfo("Added #" + strconv.Itoa(t.ID) + " " + t.Title)
			}}
		}
	})
	return nil
}
