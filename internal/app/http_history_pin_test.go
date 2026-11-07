package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// httpHistoryPinModel is a model with three history entries: two today,
// one five days ago.
func httpHistoryPinModel(t *testing.T) (*Model, []httpclient.Entry) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	now := time.Now()
	var out []httpclient.Entry
	for _, e := range []httpclient.Entry{
		{Time: now.AddDate(0, 0, -5), Method: "GET", URL: "http://x/old", Status: 200},
		{Time: now.Add(-time.Minute), Method: "POST", URL: "http://x/users", Status: 201},
		{Time: now, Method: "GET", URL: "http://x/new", Status: 200},
	} {
		got, err := httpHistoryStore().AppendEntry(e)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, got)
	}
	return &Model{w: 120, h: 40}, out
}

// The history picker groups rows by day, newest day first; filtering
// still works on the titles.
func TestHTTPHistoryPickerGroupsByDay(t *testing.T) {
	m, _ := httpHistoryPinModel(t)
	m.openHTTPHistory()
	if !m.pickerOpen || m.pickerKind != pickerKindHTTPHistory {
		t.Fatal("no history picker")
	}
	view := ansi.Strip(m.picker.View())
	today := strings.Index(view, "Today")
	old := strings.Index(view, httpclient.DayLabel(time.Now().AddDate(0, 0, -5), time.Now()))
	if today < 0 || old < 0 || today > old {
		t.Fatalf("want Today before the older day:\n%s", view)
	}
	if strings.Index(view, "/new") > strings.Index(view, "/old") {
		t.Fatalf("newest first:\n%s", view)
	}
	if !strings.Contains(view, "Ctrl+T") || !strings.Contains(view, "Ctrl+S") {
		t.Fatalf("hint must name pin and save keys:\n%s", view)
	}
}

// Ctrl+T pins the row: it moves to a Pinned group on top and keeps the
// cursor; Ctrl+T again unpins.
func TestHTTPHistoryPickerPin(t *testing.T) {
	m, es := httpHistoryPinModel(t)
	old := es[0]
	m.openHTTPHistory()
	m.picker.SetCursorID(old.ID)
	if _, ok := m.httpPickerKey(tea.KeyMsg{Type: tea.KeyCtrlT}); !ok {
		t.Fatal("ctrl+t not handled")
	}
	if e, _ := httpHistoryStore().Get(old.ID); !e.Pinned {
		t.Fatal("entry not pinned on disk")
	}
	if !m.pickerOpen {
		t.Fatal("picker closed after pin")
	}
	if it, ok := m.picker.Current(); !ok || it.ID != old.ID {
		t.Fatalf("cursor moved to %+v", it)
	}
	view := ansi.Strip(m.picker.View())
	pin, today := strings.Index(view, "Pinned"), strings.Index(view, "Today")
	if pin < 0 || today < 0 || pin > today || strings.Index(view, "/old") > today {
		t.Fatalf("pinned group must come first with the old row:\n%s", view)
	}
	m.httpPickerKey(tea.KeyMsg{Type: tea.KeyCtrlT})
	if e, _ := httpHistoryStore().Get(old.ID); e.Pinned {
		t.Fatal("second ctrl+t must unpin")
	}
	if strings.Contains(ansi.Strip(m.picker.View()), "Pinned") {
		t.Fatal("empty Pinned group still shown")
	}
}

// Ctrl+S asks for a name, then saves the entry's request to saved.http.
func TestHTTPHistoryPickerSave(t *testing.T) {
	m, es := httpHistoryPinModel(t)
	m.openHTTPHistory()
	m.picker.SetCursorID(es[1].ID)
	if _, ok := m.httpPickerKey(tea.KeyMsg{Type: tea.KeyCtrlS}); !ok {
		t.Fatal("ctrl+s not handled")
	}
	if m.pickerOpen {
		t.Fatal("picker still open under the prompt")
	}
	if !m.promptOpen || m.promptKind != promptKindHTTPSaveName {
		t.Fatal("no save-name prompt")
	}
	m.httpSaveRequest("create user")
	b, err := os.ReadFile(httpSavedPath())
	if err != nil {
		t.Fatal(err)
	}
	f := httpclient.Parse(string(b))
	if len(f.Requests) != 1 || f.Requests[0].Method != "POST" || f.Requests[0].URL != "http://x/users" || f.Requests[0].Label() != "create user" {
		t.Fatalf("saved.http = %q", b)
	}
}
