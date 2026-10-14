package app

import "strings"

// tooltipSpec is the hover label of one kind of hit target.
type tooltipSpec struct {
	name string
	// palette is the command-palette id of the same command: its key
	// comes from the live keymap (keymap.json included), shown the way
	// the palette shows it, so it cannot drift.
	palette string
	// local is a key the widget handles itself (not in the keymap).
	local string
}

// tooltipLabels maps a hit target id (see hit_targets.go) to its label.
// Ids ending in ":" are prefixes for numbered targets ("close:3").
// Activity-bar icons use their own name (activity.Item.Label).
var tooltipLabels = map[string]tooltipSpec{
	"overflow:glyph": {name: "More actions"},

	// AI chat panel header (+ ≡ ⚐ ×) and the launcher chip.
	"ai:new":             {name: "New chat", local: "Ctrl+N"}, // aiPanelKey
	"ai:history":         {name: "Chat history"},
	"ai:btn:attach-file": {name: "Attach file"},
	"actions:pin":        {name: "Unpin panel"},
	"actions:close":      {name: "Close panel", palette: "toggle-actions-panel"},
	"actions:launcher":   {name: "AI chat", palette: "toggle-actions-panel"},

	// Bottom panel tab bar.
	"panel:new":         {name: "New terminal", palette: "terminal-new-tab"},
	"panel:maximize":    {name: "Maximize panel"},
	"panel:minimize":    {name: "Minimize panel"},
	"panel:close":       {name: "Close panel", palette: "toggle-terminal"},
	"panel:term-close:": {name: "Close terminal", palette: "terminal-close-tab"},
	"panel:tab-close:":  {name: "Close tab"},

	// Editor tab bar.
	"close:":            {name: "Close", palette: "close-buffer"},
	"tabs:scroll-left":  {name: "Scroll tabs left"},
	"tabs:scroll-right": {name: "Scroll tabs right"},

	// Status bar.
	"status:ai":       {name: "AI chat", palette: "toggle-actions-panel"},
	"status:lsp":      {name: "Language servers"},
	"status:diag":     {name: "Problems", palette: "problems-panel"},
	"status:tests":    {name: "Test results"},
	"status:debug":    {name: "Call stack"},
	"status:branch":   {name: "Git branch"},
	"status:indent":   {name: "Indentation"},
	"status:encoding": {name: "File encoding"},
	"status:lang":     {name: "Language"},
	"status:term":     {name: "Terminal", palette: "toggle-terminal"},
	"status:pos":      {name: "Line and column"},

	// Source Control header.
	"scm:toggle-view": {name: "Tree or list view"},
	"scm:btn:sync":    {name: "Sync changes"},
	"scm:btn:more":    {name: "More git actions"},

	// Find bar.
	"find:chevron":     {name: "Toggle replace"},
	"find:case":        {name: "Match case"},
	"find:word":        {name: "Match whole word"},
	"find:regex":       {name: "Use regular expression"},
	"find:prev":        {name: "Previous match"},
	"find:next":        {name: "Next match"},
	"find:menu":        {name: "More options"},
	"find:close":       {name: "Close"},
	"find:replace":     {name: "Replace"},
	"find:replace-all": {name: "Replace all"},

	// Side-by-side diff winbar.
	"diff:prev": {name: "Previous change"},
	"diff:next": {name: "Next change"},
}

// tooltipSpecFor finds the spec of hit target id.
func tooltipSpecFor(id string) (tooltipSpec, bool) {
	if s, ok := tooltipLabels[id]; ok {
		return s, true
	}
	if i := strings.LastIndexByte(id, ':'); i > 0 {
		if s, ok := tooltipLabels[id[:i+1]]; ok {
			return s, true
		}
	}
	return tooltipSpec{}, false
}

// tooltipLabel is the name and key of hit target id ("" name = none).
func (m Model) tooltipLabel(id string) (name, key string) {
	if label, ok := strings.CutPrefix(id, "activity:"); ok {
		key := ""
		if label == "Explorer" {
			key = m.paletteKey("toggle-explorer")
		}
		return label, key
	}
	s, ok := tooltipSpecFor(id)
	if !ok {
		return "", ""
	}
	if s.palette != "" {
		return s.name, m.paletteKey(s.palette)
	}
	return s.name, s.local
}

// paletteKey is the main key of palette command id: the first key the
// palette shows for it (keyHint), "" when nothing is bound.
func (m Model) paletteKey(id string) string {
	a, ok := paletteActions[id]
	if !ok {
		return ""
	}
	static := ""
	for _, it := range paletteItems() {
		if it.ID == id {
			static = it.Hint
			break
		}
	}
	first, _, _ := strings.Cut(keyHint(m.keys.KeysFor(a), static), " / ")
	return first
}

// tooltipText is the label of hit target id as drawn, without padding.
func (m Model) tooltipText(id string) string {
	name, key := m.tooltipLabel(id)
	if name == "" || key == "" {
		return name
	}
	return name + " (" + key + ")"
}
