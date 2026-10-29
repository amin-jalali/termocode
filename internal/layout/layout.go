package layout

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/amin-jalali/termocode/internal/jsonc"
)

// Layout is one named arrangement. A nil section means "leave this part as
// it is" when the layout is opened.
type Layout struct {
	// Name is the file name without .jsonc (set by the store, not stored).
	Name string `json:"-"`
	// Description is one line shown in the picker.
	Description string `json:"description,omitempty"`

	Sidebar *Sidebar `json:"sidebar,omitempty"`
	Panel   *Panel   `json:"panel,omitempty"`
	Chat    *Chat    `json:"chat,omitempty"`
	Editor  *Editor  `json:"editor,omitempty"`
	// Focus is the part that has the keyboard after opening: "editor",
	// "sidebar" or "terminal". Empty keeps the current focus.
	Focus string `json:"focus,omitempty"`
}

// Sidebar is the left sidebar.
type Sidebar struct {
	Open bool `json:"open"`
	// View is one of SidebarViews. Empty keeps the current view.
	View string `json:"view,omitempty"`
	// Width in columns; 0 keeps the current width.
	Width int `json:"width,omitempty"`
}

// Panel is the bottom panel.
type Panel struct {
	Open bool `json:"open"`
	// Rows is the height; 0 keeps the current height.
	Rows int `json:"rows,omitempty"`
	// Active is the tab that shows (one of PanelTabs); empty keeps it.
	Active string `json:"active,omitempty"`
	// Tabs are the non-terminal tabs to open (Problems, Output, ...).
	Tabs []string `json:"tabs,omitempty"`
	// Terminals are the terminal tabs: a name and a folder, never output.
	Terminals []Terminal `json:"terminals,omitempty"`
}

// Terminal is one terminal tab.
type Terminal struct {
	Name string `json:"name,omitempty"`
	// Cwd is relative to the project folder.
	Cwd string `json:"cwd,omitempty"`
	// Panes are the tab's extra shells, in the order they were split off.
	// The tab's own shell is pane 0; Panes[0] is pane 1, and so on.
	// Older layout files have none.
	Panes []Pane `json:"panes,omitempty"`
}

// Pane is one extra shell of a terminal tab.
type Pane struct {
	// Split is how it was made: "right" (beside its parent) or "down"
	// (below it).
	Split string `json:"split"`
	// Parent is the pane it was split from: 0 is the tab's own shell, 1 the
	// first entry of Panes. It must come before this pane.
	Parent int `json:"parent,omitempty"`
	// Cwd is relative to the project folder.
	Cwd string `json:"cwd,omitempty"`
}

// MaxPanes is the most extra panes a tab can list (a tab holds 4 shells).
const MaxPanes = 3

// PaneSplits are the names of the two ways to split a pane.
var PaneSplits = []string{"right", "down"}

// Chat is the right panel (AI chat and actions).
type Chat struct {
	Open bool `json:"open"`
	// Width in columns; 0 keeps the current width.
	Width int `json:"width,omitempty"`
}

// Editor is the editor area.
type Editor struct {
	// Files are opened (relative paths). Missing files are skipped.
	Files []string `json:"files,omitempty"`
	// Active is the file that shows (a relative path); empty keeps it.
	Active string `json:"active,omitempty"`
	// Split is "", "vertical" or "horizontal".
	Split string `json:"split,omitempty"`
	// Panes is the file of each split window, in order (needs Split).
	Panes []string `json:"panes,omitempty"`
}

// SidebarViews are the names a layout can give the sidebar.
var SidebarViews = []string{"files", "search", "git", "run", "tests", "containers", "databases", "itermoc", "runs"}

// PanelTabs are the names a layout can give the bottom panel's tabs.
var PanelTabs = []string{"terminal", "problems", "output", "tests", "debug-console"}

var focusNames = []string{"editor", "sidebar", "terminal"}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Parse reads a layout file (JSONC). The name is not part of the file.
func Parse(src []byte) (Layout, error) {
	var l Layout
	dec := json.NewDecoder(bytes.NewReader(jsonc.Strip(src)))
	if err := dec.Decode(&l); err != nil {
		return Layout{}, fmt.Errorf("layout file: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return Layout{}, errors.New("layout file: extra text after the layout")
	}
	if err := l.Validate(); err != nil {
		return Layout{}, err
	}
	return l, nil
}

// relPath checks a path of a shared file: relative, inside the project.
func relPath(what, p string) error {
	if p == "" {
		return nil
	}
	clean := path.Clean(strings.ReplaceAll(p, "\\", "/"))
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("%s %q must be a path inside the project", what, p)
	}
	return nil
}

// Validate says what is wrong with l. A layout comes from a file anyone can
// share, so paths must stay inside the project and names must be known.
func (l Layout) Validate() error {
	if s := l.Sidebar; s != nil {
		if s.View != "" && !has(SidebarViews, s.View) {
			return fmt.Errorf("sidebar view %q is not one of %s", s.View, strings.Join(SidebarViews, ", "))
		}
		if s.Width < 0 {
			return errors.New("sidebar width must not be negative")
		}
	}
	if p := l.Panel; p != nil {
		if p.Rows < 0 {
			return errors.New("panel rows must not be negative")
		}
		for _, t := range append([]string{p.Active}, p.Tabs...) {
			if t != "" && !has(PanelTabs, t) {
				return fmt.Errorf("panel tab %q is not one of %s", t, strings.Join(PanelTabs, ", "))
			}
		}
		for _, t := range p.Terminals {
			if err := relPath("terminal cwd", t.Cwd); err != nil {
				return err
			}
			if err := validPanes(t.Panes); err != nil {
				return err
			}
		}
	}
	if c := l.Chat; c != nil && c.Width < 0 {
		return errors.New("chat width must not be negative")
	}
	if e := l.Editor; e != nil {
		switch e.Split {
		case "", "vertical", "horizontal":
		default:
			return fmt.Errorf("editor split %q must be vertical or horizontal", e.Split)
		}
		for _, f := range append(append([]string{e.Active}, e.Files...), e.Panes...) {
			if err := relPath("file", f); err != nil {
				return err
			}
		}
	}
	if l.Focus != "" && !has(focusNames, l.Focus) {
		return fmt.Errorf("focus %q is not one of %s", l.Focus, strings.Join(focusNames, ", "))
	}
	return nil
}

// validPanes checks the split shape of one terminal tab.
func validPanes(panes []Pane) error {
	if len(panes) > MaxPanes {
		return fmt.Errorf("a terminal tab has at most %d extra panes", MaxPanes)
	}
	for k, p := range panes {
		if !has(PaneSplits, p.Split) {
			return fmt.Errorf("pane split %q is not one of %s", p.Split, strings.Join(PaneSplits, ", "))
		}
		// Pane k+1 can only be split off a pane made before it.
		if p.Parent < 0 || p.Parent > k {
			return fmt.Errorf("pane %d: parent %d must be an earlier pane (0 to %d)", k+1, p.Parent, k)
		}
		if err := relPath("pane cwd", p.Cwd); err != nil {
			return err
		}
	}
	return nil
}

// Marshal is the file text of l: a short comment, then indented JSON.
func (l Layout) Marshal() ([]byte, error) {
	if err := l.Validate(); err != nil {
		return nil, err
	}
	body, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return nil, err
	}
	head := "// termocode layout. Open it with Layout: Open... in the command palette.\n"
	return append([]byte(head), append(body, '\n')...), nil
}
