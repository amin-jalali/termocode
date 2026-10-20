package keymap

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// OverridesPath returns the absolute path of the user-overrides JSON file.
// Mirrors the resolution rule used by theme.ConfigPath: honor
// $XDG_CONFIG_HOME, fall back to ~/.config.
func OverridesPath() (string, error) {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "termocode", "keymap.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "termocode", "keymap.json"), nil
}

// overrideFile is the on-disk schema. It is a flat map of key-string to
// action-name (e.g. {"alt+s": "Save"}). Entries with a malformed key or an
// unknown action name are dropped at load time (and reported by
// LoadOverridesReport) so a stale or hand-edited file can never block
// startup.
type overrideFile map[string]string

// LoadOverrides reads the persisted overrides file at `path` and returns a
// map of bubble-tea key string -> Action. Missing file, unreadable file,
// corrupt JSON, and bad entries are all treated as "no override" — the
// function never returns an error, mirroring the rest of termocode's
// "config can't break startup" rule. Pass an empty path to fall back to
// the default OverridesPath().
func LoadOverrides(path string) map[string]Action {
	out, _ := LoadOverridesReport(path)
	return out
}

// LoadOverridesReport is LoadOverrides plus one human-readable problem
// line per entry it dropped (corrupt JSON, malformed key, unknown action
// name), so the UI can tell the user about typos. A missing file is not a
// problem. "ext:" values belong to LoadExtBindings and are skipped
// silently.
func LoadOverridesReport(path string) (map[string]Action, []string) {
	if path == "" {
		p, err := OverridesPath()
		if err != nil {
			return nil, nil
		}
		path = p
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, []string{err.Error()}
	}
	var raw overrideFile
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, []string{"not valid JSON: " + err.Error()}
	}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]Action, len(raw))
	var problems []string
	for _, rawKey := range keys {
		name := raw[rawKey]
		if strings.HasPrefix(name, ExtPrefix) {
			continue
		}
		key := strings.TrimSpace(rawKey)
		if err := ValidateKey(key); err != nil {
			problems = append(problems, err.Error())
			continue
		}
		act := actionFromName(strings.TrimSpace(name))
		if act == ActionNone {
			problems = append(problems, fmt.Sprintf("%q: unknown action %q", key, name))
			continue
		}
		out[key] = act
	}
	return out, problems
}

// SaveOverrides writes the given key->action map to `path`. Existing entries
// in the on-disk file that aren't in the provided map are preserved unless
// they collide on key (read-modify-write); empty `path` falls back to
// OverridesPath(). Returns an error so the UI can toast it.
func SaveOverrides(path string, m map[string]Action) error {
	if path == "" {
		p, err := OverridesPath()
		if err != nil {
			return err
		}
		path = p
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out := overrideFile{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	for k, a := range m {
		out[k] = ActionName(a)
	}
	enc, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, enc, 0o644)
}

// MergeInto returns a new KeyMap with the supplied overrides layered on top
// of `k`. Tests rely on Default() staying untouched, so this never mutates
// the receiver.
func (k KeyMap) MergeInto(overrides map[string]Action) KeyMap {
	merged := make(map[string]Action, len(k.bindings)+len(overrides))
	for key, a := range k.bindings {
		merged[key] = a
	}
	for key, a := range overrides {
		merged[key] = a
	}
	return KeyMap{bindings: merged}
}

// Bindings returns a copy of the live bindings map (key string -> Action).
// Used by the keybinding-customization UI so it can render rows without
// reaching into unexported state.
func (k KeyMap) Bindings() map[string]Action {
	out := make(map[string]Action, len(k.bindings))
	for k2, v := range k.bindings {
		out[k2] = v
	}
	return out
}

// namedKeys are the non-character key names Bubble Tea (and the CSI-u
// spellings termocode binds) use after the modifiers.
var namedKeys = map[string]bool{
	"enter": true, "tab": true, "esc": true, "backspace": true, "space": true,
	"up": true, "down": true, "left": true, "right": true,
	"home": true, "end": true, "pgup": true, "pgdown": true,
	"delete": true, "insert": true,
}

// ValidateKey reports whether s is a key string Bubble Tea can produce:
// optional "ctrl+", "alt+", "shift+" modifiers (each at most once), then
// one character ("a", "T", "/", "`", "+") or a named key ("enter",
// "pgdown", "f1".."f24"). It catches typos such as "ctrl+monkey",
// "alt+" or "ctrl+ctrl+s" before they reach keymap.json. Any key that
// passes may be bound, even one no default binding uses.
func ValidateKey(s string) error {
	if s == "" {
		return errors.New("empty key")
	}
	if strings.TrimSpace(s) != s {
		return fmt.Errorf("%q: key has spaces around it", s)
	}
	rest := s
	seen := map[string]bool{}
	for {
		i := strings.Index(rest, "+")
		// A "+" at index 0 or a lone last char is the key itself ("ctrl++").
		if i <= 0 || i == len(rest)-1 {
			break
		}
		mod := rest[:i]
		if mod != "ctrl" && mod != "alt" && mod != "shift" {
			break
		}
		if seen[mod] {
			return fmt.Errorf("%q: %s given twice", s, mod)
		}
		seen[mod] = true
		rest = rest[i+1:]
	}
	if rest == "" || strings.HasSuffix(s, "+") && rest != "+" {
		return fmt.Errorf("%q: missing key after the modifiers", s)
	}
	if utf8.RuneCountInString(rest) == 1 {
		r, _ := utf8.DecodeRuneInString(rest)
		if unicode.IsPrint(r) && r != ' ' {
			return nil
		}
		return fmt.Errorf("%q: %q is not a printable key", s, rest)
	}
	if namedKeys[rest] {
		return nil
	}
	if n, err := strconv.Atoi(strings.TrimPrefix(rest, "f")); err == nil && rest[0] == 'f' && n >= 1 && n <= 24 {
		return nil
	}
	return fmt.Errorf("%q: unknown key %q (use a character, f1-f24, or one of enter, tab, esc, backspace, space, up, down, left, right, home, end, pgup, pgdown, delete, insert)", s, rest)
}

// IsKnownKey reports whether s is a valid key string (see ValidateKey).
// The keybinding-customization UI checks it before writing keymap.json.
func IsKnownKey(s string) bool {
	return ValidateKey(strings.TrimSpace(s)) == nil
}

// AllActions returns every Action the keymap layer knows about, sorted by
// stable name. Used by the keybinding picker so each Action can be
// surfaced as a row even if it isn't bound by default.
func AllActions() []Action {
	out := make([]Action, 0, len(actionNames))
	for a := range actionNames {
		if a == ActionNone {
			continue
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		return ActionName(out[i]) < ActionName(out[j])
	})
	return out
}

// ─── Action <-> name plumbing ─────────────────────────────────────────
//
// We keep the names in a single declared map so adding a new Action is a
// one-line edit. ActionName / actionFromName run through the map both
// ways. The names are stable strings — they end up in the persisted
// keymap.json — so renaming an existing entry would orphan user
// overrides on the next launch.

var actionNames = map[Action]string{
	ActionQuit:                     "Quit",
	ActionSave:                     "Save",
	ActionSaveAll:                  "SaveAll",
	ActionFocusExplorer:            "FocusExplorer",
	ActionFocusEditor:              "FocusEditor",
	ActionFocusSwap:                "FocusSwap",
	ActionToggleExplorer:           "ToggleExplorer",
	ActionOpenShell:                "OpenShell",
	ActionToggleTerminal:           "ToggleTerminal",
	ActionCloseBuffer:              "CloseBuffer",
	ActionNextBuffer:               "NextBuffer",
	ActionPrevBuffer:               "PrevBuffer",
	ActionQuickOpen:                "QuickOpen",
	ActionCommandPalette:           "CommandPalette",
	ActionFind:                     "Find",
	ActionReplaceInFile:            "ReplaceInFile",
	ActionDuplicateLine:            "DuplicateLine",
	ActionMarkdownPreview:          "MarkdownPreview",
	ActionWorkspaceSearch:          "WorkspaceSearch",
	ActionRevealFile:               "RevealFile",
	ActionReopenClosed:             "ReopenClosed",
	ActionToggleInlayHints:         "ToggleInlayHints",
	ActionPickTheme:                "PickTheme",
	ActionClipboardHistory:         "ClipboardHistory",
	ActionToggleComment:            "ToggleComment",
	ActionCopy:                     "Copy",
	ActionPaste:                    "Paste",
	ActionCut:                      "Cut",
	ActionCodeActions:              "CodeActions",
	ActionGotoSymbolInFile:         "GotoSymbolInFile",
	ActionToggleBookmark:           "ToggleBookmark",
	ActionShowBookmarks:            "ShowBookmarks",
	ActionGotoLine:                 "GotoLine",
	ActionSplitVertical:            "SplitVertical",
	ActionSplitHorizontal:          "SplitHorizontal",
	ActionCloseSplit:               "CloseSplit",
	ActionFormatDocument:           "FormatDocument",
	ActionOpenRecent:               "OpenRecent",
	ActionToggleWordWrap:           "ToggleWordWrap",
	ActionAlternateFile:            "AlternateFile",
	ActionNextDiagnostic:           "NextDiagnostic",
	ActionPrevDiagnostic:           "PrevDiagnostic",
	ActionToggleHidden:             "ToggleHidden",
	ActionShowCheatSheet:           "ShowCheatSheet",
	ActionHover:                    "Hover",
	ActionFindReferences:           "FindReferences",
	ActionReplaceInWorkspace:       "ReplaceInWorkspace",
	ActionGotoTypeDef:              "GotoTypeDef",
	ActionGotoImplementation:       "GotoImplementation",
	ActionRunTests:                 "RunTests",
	ActionGitBlameLine:             "GitBlameLine",
	ActionFileHistory:              "FileHistory",
	ActionToggleZenMode:            "ToggleZenMode",
	ActionReloadWindow:             "ReloadWindow",
	ActionToggleAutoSave:           "ToggleAutoSave",
	ActionNextHunk:                 "NextHunk",
	ActionPrevHunk:                 "PrevHunk",
	ActionStageHunk:                "StageHunk",
	ActionUnstageHunk:              "UnstageHunk",
	ActionDiscardHunk:              "DiscardHunk",
	ActionReloadBuffer:             "ReloadBuffer",
	ActionOpenURL:                  "OpenURL",
	ActionPinTab:                   "PinTab",
	ActionCopyFileRef:              "CopyFileRef",
	ActionShowSnippetPicker:        "ShowSnippetPicker",
	ActionApplyPreferredCodeAction: "ApplyPreferredCodeAction",
	// Group C
	ActionRunTask:      "RunTask",
	ActionRerunTask:    "RerunTask",
	ActionShowProblems: "ShowProblems",
	// Group A — AI.
	ActionAITriggerInline: "AITriggerInline",
	ActionAIEditSelection: "AIEditSelection",
	ActionAIToggleInline:  "AIToggleInline",
	ActionAINextProfile:   "AINextProfile",
	ActionAIToggleAgents:  "AIToggleAgents",
	ActionContextHelp:     "ContextHelp",
	// Docs: these had default keys but no name, so keymap.json could not
	// rebind them.
	ActionShowBufferInfo:     "ShowBufferInfo",
	ActionToggleActionsPanel: "ToggleActionsPanel",
	ActionTerminalNewTab:     "TerminalNewTab",
	ActionTerminalCloseTab:   "TerminalCloseTab",
	ActionTerminalNextTab:    "TerminalNextTab",
	ActionTerminalPrevTab:    "TerminalPrevTab",
	ActionNextSearchResult:   "NextSearchResult",
	ActionPrevSearchResult:   "PrevSearchResult",
	ActionDetachSession:      "DetachSession",
	ActionServicesFocus:      "ServicesFocus",
	ActionDatabasesFocus:     "DatabasesFocus",
	ActionHTTPSend:           "HTTPSend",
}

// ActionName returns the stable string name for an Action. Empty string
// for unknown actions (including ActionNone).
func ActionName(a Action) string {
	if name, ok := actionNames[a]; ok {
		return name
	}
	return ""
}

// legacyActionNames maps retired action names that may still sit in a
// user's keymap.json to the action that does that job today.
// "MdLivePreview" was a tab-based live Markdown preview that was never
// finished; the F7 Markdown preview replaced it.
var legacyActionNames = map[string]Action{
	"MdLivePreview": ActionMarkdownPreview,
}

// actionFromName is the reverse lookup. Returns ActionNone on unknown name.
func actionFromName(name string) Action {
	for a, n := range actionNames {
		if n == name {
			return a
		}
	}
	if a, ok := legacyActionNames[name]; ok {
		return a
	}
	return ActionNone
}
