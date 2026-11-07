package app

import (
	"context"
	"path"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

// ── AI tools in a container workspace (RC-5 4e) ──────────────────────────
//
// The agent (chat, runs, and `termocode mcp` clients, which share the
// same tool list) works on the container's files: ai.Workspace gets the
// Host FS and Exec, so read/write/search/glob/git/run_command go there
// and never to the local disk at the same path. Approval classes and
// confirm cards are the same as for a local folder.

// aiWorkspaceAt is the ai.Workspace for root: the container's Host when
// root is the root of the Model's container workspace (or under it),
// else the local folder (a worktree, a test temp dir).
func (m *Model) aiWorkspaceAt(root string) ai.Workspace {
	ws := m.workspace()
	if isLocal(ws) || ws.Host == nil || !pathUnder(ws.Root, root) {
		return ai.Workspace{Root: root}
	}
	return ai.Workspace{Root: root, FS: ws.Host.FS(), Exec: ws.Host.Exec(), HostLabel: ws.Host.Label()}
}

// pathUnder reports whether p is root or below it (slash paths).
func pathUnder(root, p string) bool {
	if root == "" || p == "" {
		return false
	}
	root, p = path.Clean(root), path.Clean(p)
	return p == root || root == "/" || len(p) > len(root) && p[:len(root)] == root && p[len(root)] == '/'
}

// aiSyncHostTools is aiSyncTools for a container workspace: the file
// tools keep their Host Run, but first refuse a file whose editor tab has
// unsaved edits (the user's work is never overwritten), and then the IDE
// refreshes (explorer, open buffers, git).
func aiSyncHostTools(tools []ai.Tool, ws ai.Workspace, call aiIDECaller) []ai.Tool {
	rel := aiRel(ws.Root)
	out := make([]ai.Tool, 0, len(tools))
	for _, t := range tools {
		inner := t.Run
		switch t.Name {
		case "write_file", "edit_file", "delete", "rename", "create_file", "create_dir":
			key := "path"
			if t.Name == "rename" {
				key = "from"
			}
			name := t.Name
			t.Run = func(ctx context.Context, a map[string]any) (string, error) {
				abs, err := ws.Resolve(ai.ArgString(a, key))
				if err == nil && name != "create_dir" {
					if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) { return "", nil, m.aiRefuseUnsaved(abs, name, rel) }); err != nil {
						return "", err
					}
				}
				res, err := inner(ctx, a)
				if err != nil {
					return res, err
				}
				reveal := abs
				if name == "rename" {
					reveal, _ = ws.Resolve(ai.ArgString(a, "to"))
				}
				if name == "delete" {
					reveal = ""
				}
				return res + aiSyncAfter(ctx, call, reveal, aiSyncDisk), nil
			}
		case "git_stage", "git_commit", "git_push":
			t.Run = aiAfterRun(inner, call, aiSyncGit)
		case "git_checkout", "git_pull", "run_command", "format":
			t.Run = aiAfterRun(inner, call, aiSyncDisk)
		}
		out = append(out, t)
	}
	return out
}
