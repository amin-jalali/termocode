package app

import (
	"encoding/json"

	tea "github.com/charmbracelet/bubbletea"
)

// aiBatch is set while one user action changes many files at once (AI:
// Undo Last Turn): the editor's buffers are listed with one nvim call,
// not one per file, and the IDE (explorer, open tabs, git) is refreshed
// once when the batch ends instead of after every file.
type aiBatch struct {
	have    bool
	bufs    []aiBuf
	sync    bool // a refresh was asked for
	disk    bool // ... of files on disk (else git only)
	reveals []string
}

// aiAllBuffersLua lists every file buffer (aiBuffersUnderLua without the
// path filter).
const aiAllBuffersLua = `
local out = {}
for _, b in ipairs(vim.api.nvim_list_bufs()) do
  if vim.bo[b].buftype == '' then
    out[#out + 1] = { id = b, path = vim.api.nvim_buf_get_name(b), modified = vim.bo[b].modified,
      listed = vim.bo[b].buflisted, loaded = vim.api.nvim_buf_is_loaded(b) }
  end
end
return vim.json.encode(out)
`

func (m *Model) aiBatchNow() *aiBatch {
	if m.ai == nil {
		return nil
	}
	return m.ai.batch
}

// aiBeginBatch starts a batch (nil end func when one is running: the
// outer one ends it).
func (m *Model) aiBeginBatch() (end func() tea.Cmd) {
	s := m.aiS()
	if s.batch != nil {
		return func() tea.Cmd { return nil }
	}
	b := &aiBatch{}
	s.batch = b
	return func() tea.Cmd {
		s.batch = nil
		if !b.sync {
			return nil
		}
		lvl := aiSyncGit
		if b.disk {
			lvl = aiSyncDisk
		}
		_, cmd := m.aiSyncIDE("", lvl)
		for _, r := range b.reveals {
			m.explorer.RevealPath(r)
		}
		return cmd
	}
}

// buffersUnder is aiBuffersUnder from the batch's one buffer list.
func (b *aiBatch) buffersUnder(m *Model, path string) ([]aiBuf, error) {
	if !b.have {
		var raw string
		if err := m.nvim.ExecLuaResult(aiAllBuffersLua, &raw); err != nil {
			return nil, err
		}
		b.bufs = nil
		if raw != "" && raw != "{}" && raw != "[]" {
			if err := json.Unmarshal([]byte(raw), &b.bufs); err != nil {
				return nil, err
			}
		}
		b.have = true
	}
	var out []aiBuf
	for _, x := range b.bufs {
		if x.Path == path || (len(x.Path) > len(path) && x.Path[:len(path)+1] == path+"/") {
			out = append(out, x)
		}
	}
	return out, nil
}
