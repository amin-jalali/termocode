package layout

// Built-in layouts. They have no files and no terminal folders, so they
// fit any project. A user or project file with the same name wins.
//
// "default" is what termocode shows on a first start: sidebar with files,
// no chat panel, no bottom panel, one editor window.
var builtins = []Layout{
	{
		Name:        "default",
		Description: "Files and editor, as on a first start.",
		Sidebar:     &Sidebar{Open: true, View: "files"},
		Panel:       &Panel{Open: false},
		Chat:        &Chat{Open: false},
		Editor:      &Editor{Split: ""},
		Focus:       "editor",
	},
	{
		Name:        "focus",
		Description: "Editor only. No sidebar, panel or chat.",
		Sidebar:     &Sidebar{Open: false},
		Panel:       &Panel{Open: false},
		Chat:        &Chat{Open: false},
		Editor:      &Editor{Split: ""},
		Focus:       "editor",
	},
	{
		Name:        "debug",
		Description: "Editor, terminal and the run and debug view.",
		Sidebar:     &Sidebar{Open: true, View: "run"},
		Panel:       &Panel{Open: true, Rows: 14, Active: "debug-console", Tabs: []string{"debug-console", "problems"}, Terminals: []Terminal{{}}},
		Chat:        &Chat{Open: false},
		Focus:       "editor",
	},
	{
		Name:        "review",
		Description: "Source control changes and the AI chat.",
		Sidebar:     &Sidebar{Open: true, View: "git", Width: 40},
		Panel:       &Panel{Open: false},
		Chat:        &Chat{Open: true, Width: 60},
		Focus:       "editor",
	},
}

// Builtins are the layouts that ship with termocode (copies, safe to edit).
func Builtins() []Entry {
	out := make([]Entry, len(builtins))
	for i, l := range builtins {
		out[i] = Entry{Layout: clone(l), Source: Builtin}
	}
	return out
}

// IsBuiltin tells if name is one of the built-in layout names.
func IsBuiltin(name string) bool {
	for _, l := range builtins {
		if l.Name == name {
			return true
		}
	}
	return false
}

// clone copies the pointers and slices of l so a caller cannot change the
// built-in table.
func clone(l Layout) Layout {
	if l.Sidebar != nil {
		c := *l.Sidebar
		l.Sidebar = &c
	}
	if l.Panel != nil {
		c := *l.Panel
		c.Tabs = append([]string(nil), c.Tabs...)
		c.Terminals = append([]Terminal(nil), c.Terminals...)
		l.Panel = &c
	}
	if l.Chat != nil {
		c := *l.Chat
		l.Chat = &c
	}
	if l.Editor != nil {
		c := *l.Editor
		c.Files = append([]string(nil), c.Files...)
		c.Panes = append([]string(nil), c.Panes...)
		l.Editor = &c
	}
	return l
}
