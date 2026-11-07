package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Learned view fields (MOBUX-14): a tool without an outputSchema is
// typed from its first structured result, so its view shows a typed
// table from the second call on. What was learned is kept beside the
// server's tool cache (<name>.views.json, field names and kinds only,
// never values) and dropped when the server's entry changes.

type learnedFile struct {
	Hash  string             `json:"hash"`
	Tools map[string]Learned `json:"tools"`
}

func (m *Manager) learnedPath(name string) string {
	if m.opts.CacheDir == "" {
		return ""
	}
	return filepath.Join(m.opts.CacheDir, name+".views.json")
}

// LearnResult keeps what a tool's result teaches its view, the first
// time only (a tool with an outputSchema is typed by it and learns
// nothing). It never starts a server.
func (m *Manager) LearnResult(server, tool string, res CallResult) {
	l, ok := LearnFields(res)
	if !ok {
		return
	}
	m.mu.Lock()
	st, found := m.servers[server]
	if !found {
		m.mu.Unlock()
		return
	}
	m.loadLearnedLocked(st)
	if _, have := st.learned[tool]; have || len(st.learned) >= MaxLearnedTools || hasOutputSchema(st.tools, tool) {
		m.mu.Unlock()
		return
	}
	if st.learned == nil {
		st.learned = map[string]Learned{}
	}
	st.learned[tool] = l
	file := learnedFile{Hash: st.cfg.Hash(), Tools: copyLearned(st.learned)}
	path := m.learnedPath(server)
	m.mu.Unlock()
	if path == "" {
		return
	}
	b, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(path), 0o755) == nil {
		_ = os.WriteFile(path, b, 0o644)
	}
}

// LearnedViews is what the server's tools have taught their views.
func (m *Manager) LearnedViews(server string) map[string]Learned {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.servers[server]
	if !ok {
		return nil
	}
	m.loadLearnedLocked(st)
	return copyLearned(st.learned)
}

// ServerViews builds the default views of a server from what is known
// (memory or disk cache); it never starts the server. ok false: no such
// server or its tools are not known yet.
func (m *Manager) ServerViews(server string, rule ViewRule) ([]ToolView, bool) {
	cfg, tools, have := m.KnownTools(server)
	if cfg.Name == "" || !have {
		return nil, false
	}
	m.mu.Lock()
	caps := ServerCaps{}
	if st, ok := m.servers[server]; ok {
		caps = st.caps
	}
	m.mu.Unlock()
	return ToolViews(cfg, tools, caps, m.LearnedViews(server), rule), true
}

func (m *Manager) loadLearnedLocked(st *serverState) {
	if st.learnedRead {
		return
	}
	st.learnedRead = true
	p := m.learnedPath(st.cfg.Name)
	if p == "" {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var f learnedFile
	if json.Unmarshal(b, &f) != nil || f.Hash != st.cfg.Hash() {
		return
	}
	st.learned = map[string]Learned{}
	for k, v := range f.Tools {
		if len(st.learned) == MaxLearnedTools {
			break
		}
		if k == "" || len(v.Fields) == 0 {
			continue
		}
		v.Fields = capFields(v.Fields, MaxViewFields)
		st.learned[k] = v
	}
}

func hasOutputSchema(tools []Tool, name string) bool {
	for _, t := range tools {
		if t.Name == name {
			return t.OutputSchema != nil
		}
	}
	return false
}

func copyLearned(in map[string]Learned) map[string]Learned {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]Learned, len(in))
	for k, v := range in {
		v.Fields = append([]ViewField(nil), v.Fields...)
		out[k] = v
	}
	return out
}
