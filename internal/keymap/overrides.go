package keymap

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
// action-name (e.g. {"alt+s": "Save"}). Unknown action names and unknown key
// strings are silently dropped at load time so a stale or hand-edited file
// can never block startup.
type overrideFile map[string]string

// LoadOverrides reads the persisted overrides file at `path` and returns a
// map of bubble-tea key string -> Action. Missing file, unreadable file,
// corrupt JSON, and unknown action names are all treated as "no overrides"
// — the function never returns an error, mirroring the rest of termocode's
// "config can't break startup" rule. Pass an empty path to fall back to
// the default OverridesPath().
func LoadOverrides(path string) map[string]Action {
	if path == "" {
		p, err := OverridesPath()
		if err != nil {
			return nil
		}
		path = p
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return nil
	}
	var raw overrideFile
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil
	}
	out := make(map[string]Action, len(raw))
	defaultBindings := Default().bindings
	for key, name := range raw {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		// Reject any binding the default keymap doesn't already understand —
		// this enforces the "validate before persist" rule on the read side
		// too, so a hand-edited file with a typo can't silently shadow a
		// real binding.
		if _, knownKey := defaultBindings[key]; !knownKey {
			continue
		}
		act := actionFromName(name)
		if act == ActionNone {
			continue
		}
		out[key] = act
	}
	return out
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

// IsKnownKey reports whether `s` is a valid bubble-tea key string —
// i.e. one that Default() already binds to some action. The keybinding-
// customization UI uses this to validate user input BEFORE writing the
// override file: if a user types "ctrl+monkey" we reject it instead of
// shipping a useless entry to disk.
func IsKnownKey(s string) bool {
	_, ok := Default().bindings[strings.TrimSpace(s)]
	return ok
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
	ActionQuit:               "Quit",
	ActionSave:               "Save",
	ActionSaveAll:            "SaveAll",
	ActionFocusExplorer:      "FocusExplorer",
	ActionFocusEditor:        "FocusEditor",
	ActionFocusSwap:          "FocusSwap",
	ActionToggleExplorer:     "ToggleExplorer",
	ActionOpenShell:          "OpenShell",
	ActionToggleTerminal:     "ToggleTerminal",
	ActionCloseBuffer:        "CloseBuffer",
	ActionNextBuffer:         "NextBuffer",
	ActionPrevBuffer:         "PrevBuffer",
	ActionQuickOpen:          "QuickOpen",
	ActionCommandPalette:     "CommandPalette",
	ActionFind:               "Find",
	ActionReplaceInFile:      "ReplaceInFile",
	ActionDuplicateLine:      "DuplicateLine",
	ActionMarkdownPreview:    "MarkdownPreview",
	ActionWorkspaceSearch:    "WorkspaceSearch",
	ActionRevealFile:         "RevealFile",
	ActionReopenClosed:       "ReopenClosed",
	ActionToggleInlayHints:   "ToggleInlayHints",
	ActionPickTheme:          "PickTheme",
	ActionClipboardHistory:   "ClipboardHistory",
	ActionToggleComment:      "ToggleComment",
	ActionCopy:               "Copy",
	ActionPaste:              "Paste",
	ActionCut:                "Cut",
	ActionCodeActions:        "CodeActions",
	ActionGotoSymbolInFile:   "GotoSymbolInFile",
	ActionToggleBookmark:     "ToggleBookmark",
	ActionShowBookmarks:      "ShowBookmarks",
	ActionGotoLine:           "GotoLine",
	ActionSplitVertical:      "SplitVertical",
	ActionSplitHorizontal:    "SplitHorizontal",
	ActionCloseSplit:         "CloseSplit",
	ActionFormatDocument:     "FormatDocument",
	ActionOpenRecent:         "OpenRecent",
	ActionToggleWordWrap:     "ToggleWordWrap",
	ActionAlternateFile:      "AlternateFile",
	ActionNextDiagnostic:     "NextDiagnostic",
	ActionPrevDiagnostic:     "PrevDiagnostic",
	ActionToggleHidden:       "ToggleHidden",
	ActionShowCheatSheet:     "ShowCheatSheet",
	ActionHover:              "Hover",
	ActionFindReferences:     "FindReferences",
	ActionReplaceInWorkspace: "ReplaceInWorkspace",
	ActionGotoTypeDef:        "GotoTypeDef",
	ActionGotoImplementation: "GotoImplementation",
	ActionRunTests:           "RunTests",
	ActionGitBlameLine:       "GitBlameLine",
	ActionFileHistory:        "FileHistory",
	ActionToggleZenMode:      "ToggleZenMode",
	ActionReloadWindow:       "ReloadWindow",
	ActionToggleAutoSave:     "ToggleAutoSave",
	ActionNextHunk:           "NextHunk",
	ActionPrevHunk:           "PrevHunk",
	ActionStageHunk:          "StageHunk",
	ActionUnstageHunk:        "UnstageHunk",
	ActionDiscardHunk:        "DiscardHunk",
	ActionReloadBuffer:       "ReloadBuffer",
	ActionOpenURL:            "OpenURL",
	ActionPinTab:             "PinTab",
	ActionCopyFileRef:        "CopyFileRef",
	ActionShowSnippetPicker:        "ShowSnippetPicker",
	ActionMdLivePreview:            "MdLivePreview",
	ActionApplyPreferredCodeAction: "ApplyPreferredCodeAction",
}

// ActionName returns the stable string name for an Action. Empty string
// for unknown actions (including ActionNone).
func ActionName(a Action) string {
	if name, ok := actionNames[a]; ok {
		return name
	}
	return ""
}

// actionFromName is the reverse lookup. Returns ActionNone on unknown name.
func actionFromName(name string) Action {
	for a, n := range actionNames {
		if n == name {
			return a
		}
	}
	return ActionNone
}
