package testkit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Helpers for the security scenarios (SCN-*): a fake model that makes
// many tool calls in one reply, and snapshots of everything an action the
// AI may not run could change on disk.

// scnModel is a fake OpenAI-compatible model. Request n answers with the
// tool calls of turns[n], all in one reply; once the turns are used up it
// answers "fake-done".
type scnModel struct {
	mu     sync.Mutex
	turns  [][]toolCall
	n      int
	bodies []string
}

func newSCNModel(t *testing.T, turns ...[]toolCall) (*scnModel, *httptest.Server) {
	t.Helper()
	s := &scnModel{turns: turns}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.bodies = append(s.bodies, string(body))
		i := s.n
		s.n++
		s.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		var delta map[string]any
		if i < len(turns) {
			calls := make([]any, 0, len(turns[i]))
			for j, c := range turns[i] {
				args, _ := json.Marshal(c.args)
				calls = append(calls, map[string]any{"index": j, "id": fmt.Sprintf("call_%d_%d", i, j),
					"function": map[string]any{"name": c.name, "arguments": string(args)}})
			}
			delta = map[string]any{"tool_calls": calls}
		} else {
			delta = map[string]any{"content": "fake-done"}
		}
		b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": delta}}})
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", b)
	}))
	t.Cleanup(srv.Close)
	return s, srv
}

// results returns the tool results of the last request, by call id.
func (s *scnModel) results() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	if len(s.bodies) == 0 {
		return out
	}
	var req struct {
		Messages []struct {
			Role       string `json:"role"`
			Content    string `json:"content"`
			ToolCallID string `json:"tool_call_id"`
		} `json:"messages"`
	}
	_ = json.Unmarshal([]byte(s.bodies[len(s.bodies)-1]), &req)
	for _, m := range req.Messages {
		if m.Role == "tool" {
			out[m.ToolCallID] = m.Content
		}
	}
	return out
}

// result is the tool result of call j of turn i.
func (s *scnModel) result(i, j int) string { return s.results()[fmt.Sprintf("call_%d_%d", i, j)] }

// scnSnapshot is the state an action the AI may not run must leave alone:
// every file under some roots (content hashes; a symlink by its target)
// and the git state of the workspace.
type scnSnapshot struct {
	files map[string]string
	git   string
}

// takeSCNSnapshot hashes every file under roots (skip names are left
// out, by path relative to their root) and reads the git refs, HEAD and
// status of ws.
func takeSCNSnapshot(t *testing.T, ws string, roots map[string]string, skip ...string) scnSnapshot {
	t.Helper()
	s := scnSnapshot{files: map[string]string{}}
	for label, root := range roots {
		_ = filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			for _, k := range skip {
				if rel == k || strings.HasPrefix(rel, k+string(filepath.Separator)) {
					if e.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
			}
			key := label + ":" + rel
			switch {
			case e.Type()&fs.ModeSymlink != 0:
				target, _ := os.Readlink(p)
				s.files[key] = "link→" + target
			case e.IsDir():
				// Folders count through their files: the app makes its own
				// state folders (ai/ for the chat) as it runs.
			default:
				b, err := os.ReadFile(p)
				if err != nil {
					s.files[key] = "unreadable"
					return nil
				}
				sum := sha256.Sum256(b)
				s.files[key] = hex.EncodeToString(sum[:8])
			}
			return nil
		})
	}
	if _, err := os.Stat(filepath.Join(ws, ".git")); err == nil {
		var b strings.Builder
		for _, args := range [][]string{{"rev-parse", "HEAD"}, {"show-ref"}, {"status", "--porcelain=v1", "--untracked-files=all"}, {"stash", "list"}, {"remote", "-v"}} {
			out, _ := exec.Command("git", append([]string{"-C", ws}, args...)...).CombinedOutput()
			fmt.Fprintf(&b, "$ git %s\n%s", strings.Join(args, " "), out)
		}
		s.git = b.String()
	}
	return s
}

// diff lists what changed from s to after.
func (s scnSnapshot) diff(after scnSnapshot) []string {
	var out []string
	for k, v := range s.files {
		if w, ok := after.files[k]; !ok {
			out = append(out, "removed "+k)
		} else if w != v {
			out = append(out, "changed "+k)
		}
	}
	for k := range after.files {
		if _, ok := s.files[k]; !ok {
			out = append(out, "added "+k)
		}
	}
	if s.git != after.git {
		out = append(out, "git state:\nbefore:\n"+s.git+"after:\n"+after.git)
	}
	slices.Sort(out)
	return out
}
