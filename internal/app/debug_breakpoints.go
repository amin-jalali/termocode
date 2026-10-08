package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
)

// ── Persistent breakpoints (Group D) ─────────────────────────────────────
//
// nvim-dap keeps breakpoints as signs on loaded buffers only — they vanish
// with the buffer and with the process. Go owns the durable copy in
// ~/.config/termocode/breakpoints.json:
//
//	{ "version": 1,
//	  "breakpoints": { "/abs/file.go": [ {"line": 12, "text": "x := f()"} ] } }
//
// Flow:
//   - startup: load the file, hand it to Lua (_termocode_dap.set_stored);
//     Lua places them on every BufReadPost, re-checking each line against
//     the stored line text (dap_lua.go H.relocate).
//   - any change (toggle, remove, save of a buffer with breakpoints): Go
//     pulls _termocode_dap.breakpoints() — the live set of every LOADED
//     buffer — and merges it in: loaded files are replaced, unloaded files
//     keep their stored entries. Then the file is rewritten and Lua gets
//     the new stored map.

// storedBreakpoint is one persisted breakpoint. Text is the trimmed source
// line, used to re-find the line after the file changed on disk.
type storedBreakpoint struct {
	Line         int    `json:"line"`
	Text         string `json:"text,omitempty"`
	Condition    string `json:"condition,omitempty"`
	HitCondition string `json:"hitCondition,omitempty"`
	LogMessage   string `json:"logMessage,omitempty"`
}

// liveBreakpoint is a breakpoint as reported by Lua; Verified is nil until
// an adapter answered setBreakpoints.
type liveBreakpoint struct {
	storedBreakpoint
	Verified *bool `json:"verified,omitempty"`
}

// liveBreakpoints is the decoded _termocode_dap.breakpoints() payload.
type liveBreakpoints struct {
	Loaded []string                    `json:"loaded"`
	Bps    map[string][]liveBreakpoint `json:"bps"`
}

type breakpointFile struct {
	Version     int                           `json:"version"`
	Breakpoints map[string][]storedBreakpoint `json:"breakpoints"`
}

// breakpointsPath is ~/.config/termocode/breakpoints.json (XDG aware).
func breakpointsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "breakpoints.json"), nil
}

// loadBreakpointFile reads the store. A missing or broken file yields an
// empty map — it must never block startup.
func loadBreakpointFile(path string) map[string][]storedBreakpoint {
	out := map[string][]storedBreakpoint{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var f breakpointFile
	if json.Unmarshal(data, &f) != nil {
		return out
	}
	for p, list := range f.Breakpoints {
		var keep []storedBreakpoint
		for _, bp := range list {
			if bp.Line > 0 {
				keep = append(keep, bp)
			}
		}
		if p != "" && len(keep) > 0 {
			out[filepath.Clean(p)] = sortBreakpoints(keep)
		}
	}
	return out
}

// saveBreakpointFile writes the store atomically (tmp + rename).
func saveBreakpointFile(path string, store map[string][]storedBreakpoint) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if store == nil {
		store = map[string][]storedBreakpoint{}
	}
	data, err := json.MarshalIndent(breakpointFile{Version: 1, Breakpoints: store}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func sortBreakpoints(list []storedBreakpoint) []storedBreakpoint {
	sort.SliceStable(list, func(i, j int) bool { return list[i].Line < list[j].Line })
	return list
}

// mergeBreakpoints folds the live state of loaded buffers into the store:
// every loaded path is replaced by its live list (or dropped when it has
// none); other paths are kept. Returns the new store and whether anything
// changed. Pure for tests.
func mergeBreakpoints(store map[string][]storedBreakpoint, live liveBreakpoints) (map[string][]storedBreakpoint, bool) {
	out := make(map[string][]storedBreakpoint, len(store))
	for p, l := range store {
		out[p] = l
	}
	replace := map[string]bool{}
	for _, p := range live.Loaded {
		replace[filepath.Clean(p)] = true
	}
	for p := range live.Bps {
		replace[filepath.Clean(p)] = true
	}
	for p := range replace {
		delete(out, p)
	}
	for p, list := range live.Bps {
		var keep []storedBreakpoint
		for _, bp := range list {
			if bp.Line > 0 {
				keep = append(keep, bp.storedBreakpoint)
			}
		}
		if len(keep) > 0 {
			out[filepath.Clean(p)] = sortBreakpoints(keep)
		}
	}
	return out, !breakpointStoresEqual(store, out)
}

func breakpointStoresEqual(a, b map[string][]storedBreakpoint) bool {
	if len(a) != len(b) {
		return false
	}
	for p, la := range a {
		lb, ok := b[p]
		if !ok || len(la) != len(lb) {
			return false
		}
		for i := range la {
			if la[i] != lb[i] {
				return false
			}
		}
	}
	return true
}

// breakpointRow is one flattened breakpoint for the BREAKPOINTS section.
type breakpointRow struct {
	Path     string
	BP       storedBreakpoint
	Verified *bool
}

// sortedBreakpointRows flattens the store (path, then line order).
func sortedBreakpointRows(store map[string][]storedBreakpoint, live map[string][]liveBreakpoint) []breakpointRow {
	paths := make([]string, 0, len(store))
	for p := range store {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var out []breakpointRow
	for _, p := range paths {
		for _, bp := range store[p] {
			r := breakpointRow{Path: p, BP: bp}
			for _, lb := range live[p] {
				if lb.Line == bp.Line {
					r.Verified = lb.Verified
				}
			}
			out = append(out, r)
		}
	}
	return out
}

// ── Model glue ───────────────────────────────────────────────────────────

// loadDebugState is newDebugState plus the persisted breakpoints. New()
// uses it so the store is in place before nvim attaches.
func loadDebugState() *debugState {
	d := newDebugState()
	if path, err := breakpointsPath(); err == nil {
		d.bpPath = path
		d.bps = loadBreakpointFile(path)
	}
	return d
}

// debugLoadBreakpoints hands the persisted breakpoints to Lua and restores
// those of buffers that are already loaded. Called once while nvim
// attaches (on a Model copy — m.debug is the shared pointer from New).
func (m *Model) debugLoadBreakpoints() {
	if m.debug == nil {
		return
	}
	m.debugPushStored()
	if m.nvim != nil {
		_ = m.nvim.ExecLua(`if _G._termocode_dap and _G._termocode_dap.restore_all then _G._termocode_dap.restore_all() end`)
	}
}

// debugPushStored sends the stored map to Lua (for BufReadPost restore).
func (m *Model) debugPushStored() {
	if m.nvim == nil || m.debug == nil {
		return
	}
	store := m.debug.bps
	if store == nil {
		store = map[string][]storedBreakpoint{}
	}
	data, err := json.Marshal(store)
	if err != nil {
		return
	}
	_ = m.nvim.ExecLuaArgs(`local j = ...; if _G._termocode_dap and _G._termocode_dap.set_stored then _G._termocode_dap.set_stored(j) end`, string(data))
}

// debugSyncBreakpointsCmd pulls the live breakpoints from nvim and merges
// them into the store (persisting when they changed).
func (m *Model) debugSyncBreakpointsCmd() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	c := m.nvim
	return func() tea.Msg {
		raw, err := c.EvalLuaString(`return _G._termocode_dap and _G._termocode_dap.breakpoints and _G._termocode_dap.breakpoints() or ''`)
		if err != nil || raw == "" {
			return nil
		}
		var live liveBreakpoints
		if json.Unmarshal([]byte(raw), &live) != nil {
			return nil
		}
		return debugMsg{apply: func(m *Model) tea.Cmd {
			m.debugApplyLiveBreakpoints(live)
			return nil
		}}
	}
}

// debugApplyLiveBreakpoints merges a live snapshot and persists it.
func (m *Model) debugApplyLiveBreakpoints(live liveBreakpoints) {
	d := m.ensureDebug()
	d.liveBps = live.Bps
	merged, changed := mergeBreakpoints(d.bps, live)
	if !changed {
		return
	}
	d.bps = merged
	if d.bpPath != "" {
		if err := saveBreakpointFile(d.bpPath, merged); err != nil {
			recordError("[debug] save breakpoints: " + err.Error())
		}
	}
	m.debugPushStored()
}

// debugRemoveBreakpoint removes one breakpoint (Run view `x` / Delete).
// Loaded buffers go through nvim-dap (so live sessions hear about it);
// unloaded files are edited in the store directly.
func (m *Model) debugRemoveBreakpoint(path string, line int) tea.Cmd {
	d := m.ensureDebug()
	if m.nvim != nil {
		_ = m.nvim.ExecLuaArgs(`local p, l = ...; if _G._termocode_dap and _G._termocode_dap.remove then _G._termocode_dap.remove(p, l) end`, path, line)
	}
	if list, ok := d.bps[path]; ok {
		var keep []storedBreakpoint
		for _, bp := range list {
			if bp.Line != line {
				keep = append(keep, bp)
			}
		}
		next := make(map[string][]storedBreakpoint, len(d.bps))
		for p, l := range d.bps {
			next[p] = l
		}
		if len(keep) == 0 {
			delete(next, path)
		} else {
			next[path] = keep
		}
		d.bps = next
		if d.bpPath != "" {
			_ = saveBreakpointFile(d.bpPath, next)
		}
		m.debugPushStored()
	}
	return m.debugSyncBreakpointsCmd()
}

// debugClearBreakpoints removes every breakpoint, loaded or not.
func (m *Model) debugClearBreakpoints() tea.Cmd {
	d := m.ensureDebug()
	if m.nvim != nil {
		_ = m.nvim.ExecLua(`if _G._termocode_dap and _G._termocode_dap.clear then _G._termocode_dap.clear() end`)
	}
	d.bps = map[string][]storedBreakpoint{}
	d.liveBps = nil
	if d.bpPath != "" {
		_ = saveBreakpointFile(d.bpPath, d.bps)
	}
	m.debugPushStored()
	return m.debugToast("all breakpoints removed")
}
