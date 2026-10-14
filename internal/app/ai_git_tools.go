package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
)

// Group K4: git tools on top of internal/git — log, blame, unstage,
// fetch, hunks and stash. Read tools run at once; stage / unstage /
// fetch / stash push|apply|pop ask with class git; hunk discard and
// stash drop lose data, so they ask with class "" (no "Always allow").
// Ops that rewrite files in the working tree refuse while an affected
// file has unsaved edits in the editor, before the user is asked.
// After each change the IDE refreshes (git panel, explorer markers,
// status bar; open buffers re-read the disk when files changed).

const (
	aiGitLogDefault = 20
	aiGitLogMax     = 100
	aiGitBlameMax   = 200 // lines per git_blame call
)

var (
	aiHunkOps  = []string{"stage", "unstage", "discard"}
	aiStashOps = []string{"list", "push", "pop", "apply", "drop"}
)

func aiOneOf(what, v string, ok []string) error {
	for _, o := range ok {
		if v == o {
			return nil
		}
	}
	return fmt.Errorf("%s must be one of %s, not %q", what, strings.Join(ok, ", "), v)
}

// aiArgPaths reads "paths" as a list (a single string is accepted too).
func aiArgPaths(a map[string]any) []string {
	switch v := a["paths"].(type) {
	case []any:
		var out []string
		for _, x := range v {
			if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	case string:
		if strings.TrimSpace(v) != "" {
			return []string{v}
		}
	}
	return nil
}

// aiFormatLog renders commits one per line: sha date author subject (refs).
func aiFormatLog(es []git.LogEntry) string {
	if len(es) == 0 {
		return "no commits"
	}
	var b strings.Builder
	for i, e := range es {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%s %s | %s | %s", e.Hash, e.When, e.Author, e.Subject)
		if e.Refs != "" {
			b.WriteString(" " + e.Refs)
		}
	}
	return b.String()
}

// aiFormatBlame groups runs of lines from the same commit:
// "1-3 a1b2c3d Test 2026-03-01 subject".
func aiFormatBlame(ls []git.BlameRangeLine) string {
	var b strings.Builder
	for i := 0; i < len(ls); {
		j := i
		for j+1 < len(ls) && ls[j+1].Hash == ls[i].Hash && ls[j+1].Line == ls[j].Line+1 {
			j++
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		if j > i {
			fmt.Fprintf(&b, "%d-%d ", ls[i].Line, ls[j].Line)
		} else {
			fmt.Fprintf(&b, "%d ", ls[i].Line)
		}
		l := ls[i]
		if l.Hash == "uncommitted" {
			b.WriteString("uncommitted (working tree)")
		} else {
			fmt.Fprintf(&b, "%s %s %s %s", l.Hash, l.Author, l.Date, l.Subject)
		}
		i = j + 1
	}
	return b.String()
}

// aiRefuseUnsaved errors when a buffer of abs (a file or a folder) has
// unsaved edits: a tool that rewrites those files would fight them.
func (m *Model) aiRefuseUnsaved(abs, what string, rel func(string) string) error {
	bufs, err := m.aiBuffersUnder(abs)
	if err != nil {
		return err
	}
	if dirty := aiUnsaved(bufs, rel); len(dirty) > 0 {
		return fmt.Errorf("not done: %s open in the editor with unsaved changes; %s would rewrite it. "+
			"Ask the user to save or discard them first", strings.Join(dirty, ", "), what)
	}
	return nil
}

// aiPrecheck runs check on Update before the confirm dialog, so the user
// is never asked about a call that will be refused.
func aiPrecheck(call aiIDECaller, check func(m *Model) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := call(ctx, func(m *Model) (string, tea.Cmd, error) { return "", nil, check(m) })
	if errors.Is(err, errAIChatStopped) || errors.Is(err, context.DeadlineExceeded) {
		return nil // Run checks again
	}
	return err
}

func aiStashRef(a map[string]any) string { return fmt.Sprintf("stash@{%d}", ai.ArgInt(a, "index")) }

// aiGitTools are the K4 git tools; the IDE part runs on Update via call.
func aiGitTools(root string, call aiIDECaller) []ai.Tool {
	ws := ai.Workspace{Root: root}
	rel := aiRel(root)
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	num := func(d string) map[string]any { return map[string]any{"type": "integer", "description": d} }
	enum := func(d string, vs []string) map[string]any {
		return map[string]any{"type": "string", "enum": vs, "description": d}
	}
	// relPath resolves a tool path inside the workspace to (abs, rel).
	relPath := func(p string) (string, string, error) {
		abs, err := ws.Resolve(p)
		if err != nil {
			return "", "", err
		}
		return abs, rel(abs), nil
	}
	sync := func(ctx context.Context, lvl aiSyncLevel) string { return aiSyncAfter(ctx, call, "", lvl) }

	return []ai.Tool{
		{
			Name:        "git_log",
			Description: fmt.Sprintf("Recent commits, newest first: sha, age, author, subject, refs. Optional path limits it to that file's history; n (default %d, max %d).", aiGitLogDefault, aiGitLogMax),
			Params:      map[string]any{"path": str("file path (optional)"), "n": num("number of commits")},
			Describe: func(a map[string]any) string {
				if p := ai.ArgString(a, "path"); p != "" {
					return "Git log " + p
				}
				return "Git log"
			},
			Run: func(_ context.Context, a map[string]any) (string, error) {
				n := ai.ArgInt(a, "n")
				if n <= 0 {
					n = aiGitLogDefault
				}
				n = min(n, aiGitLogMax)
				if p := ai.ArgString(a, "path"); p != "" {
					_, r, err := relPath(p)
					if err != nil {
						return "", err
					}
					es, err := git.FileHistory(root, r, n)
					return aiFormatLog(es), err
				}
				es, err := git.Log(root, n)
				return aiFormatLog(es), err
			},
		},
		{
			Name: "git_blame",
			Description: fmt.Sprintf("Who last changed each line of a file: \"lines sha author date subject\", runs of lines "+
				"from one commit grouped. Optional start/end lines (1-based); at most %d lines per call.", aiGitBlameMax),
			Params:   map[string]any{"path": str("file path"), "start": num("first line (default 1)"), "end": num("last line")},
			Required: []string{"path"},
			Describe: func(a map[string]any) string { return "Git blame " + ai.ArgString(a, "path") },
			Run: func(_ context.Context, a map[string]any) (string, error) {
				_, r, err := relPath(ai.ArgString(a, "path"))
				if err != nil {
					return "", err
				}
				start := max(ai.ArgInt(a, "start"), 1)
				end := ai.ArgInt(a, "end")
				capped := end <= 0 || end-start+1 > aiGitBlameMax
				if capped {
					end = start + aiGitBlameMax - 1
				}
				ls, err := git.BlameRange(root, r, start, end)
				if err != nil {
					return "", err
				}
				out := aiFormatBlame(ls)
				if capped && len(ls) == aiGitBlameMax {
					out += fmt.Sprintf("\n[stopped at line %d; call again with start=%d for more]", end, end+1)
				}
				return out, nil
			},
		},
		{
			Name:        "git_unstage",
			Description: "Unstage files (keep their changes in the working tree).",
			Params: map[string]any{"paths": map[string]any{"type": "array", "items": map[string]any{"type": "string"},
				"description": "file paths"}},
			Required: []string{"paths"},
			Mutating: true, Class: ai.ClassGit,
			Describe: func(a map[string]any) string { return "Unstage " + strings.Join(aiArgPaths(a), ", ") },
			Validate: func(a map[string]any) error {
				if len(aiArgPaths(a)) == 0 {
					return errors.New("paths is empty")
				}
				return nil
			},
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				var done []string
				for _, p := range aiArgPaths(a) {
					_, r, err := relPath(p)
					if err != nil {
						return "", err
					}
					if err := git.Unstage(root, r); err != nil {
						return "", fmt.Errorf("unstage %s: %v", r, err)
					}
					done = append(done, r)
				}
				return "unstaged " + strings.Join(done, ", ") + "; Source Control lists them under Changes" + sync(ctx, aiSyncGit), nil
			},
		},
		{
			Name:        "git_fetch",
			Description: "Fetch from the remotes (updates remote branches; the working tree is not touched).",
			Mutating:    true, Class: ai.ClassGit,
			Describe: func(map[string]any) string { return "Git fetch" },
			Run: func(ctx context.Context, _ map[string]any) (string, error) {
				if err := git.Fetch(root); err != nil {
					return "", err
				}
				out := "fetched"
				if b, err := git.GetBranch(root); err == nil && b.Name != "" && git.HasUpstream(root) {
					out += fmt.Sprintf("; %s is %d ahead, %d behind its upstream", b.Name, b.Ahead, b.Behind)
				}
				return out + sync(ctx, aiSyncGit), nil
			},
		},
		{
			Name: "git_hunk",
			Description: "Stage, unstage or discard the one diff hunk that contains line (1-based; for unstage, the " +
				"line in the staged version). discard reverts it in the working tree and can not be undone; it is " +
				"refused while the file has unsaved edits in the editor.",
			Params: map[string]any{"op": enum("what to do", aiHunkOps), "path": str("file path"),
				"line": num("a line inside the hunk")},
			Required: []string{"op", "path", "line"},
			Mutating: true, Class: ai.ClassGit,
			Gate: func(a map[string]any) (bool, string) {
				if ai.ArgString(a, "op") == "discard" {
					return true, "" // data loss: always ask, no "Always allow"
				}
				return true, ai.ClassGit
			},
			Describe: func(a map[string]any) string {
				op := ai.ArgString(a, "op")
				return fmt.Sprintf("%s%s hunk at %s:%d", strings.ToUpper(op[:min(1, len(op))]), op[min(1, len(op)):],
					ai.ArgString(a, "path"), ai.ArgInt(a, "line"))
			},
			Validate: func(a map[string]any) error {
				if err := aiOneOf("op", ai.ArgString(a, "op"), aiHunkOps); err != nil {
					return err
				}
				if ai.ArgInt(a, "line") < 1 {
					return errors.New("line must be 1 or more")
				}
				abs, _, err := relPath(ai.ArgString(a, "path"))
				if err != nil || ai.ArgString(a, "op") != "discard" {
					return err
				}
				return aiPrecheck(call, func(m *Model) error { return m.aiRefuseUnsaved(abs, "discard", rel) })
			},
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				abs, r, err := relPath(ai.ArgString(a, "path"))
				if err != nil {
					return "", err
				}
				op, line := ai.ArgString(a, "op"), ai.ArgInt(a, "line")
				return call(ctx, func(m *Model) (string, tea.Cmd, error) {
					lvl, out := aiSyncGit, ""
					switch op {
					case "stage":
						err, out = git.StageHunk(root, r, line), fmt.Sprintf("staged the hunk at %s:%d", r, line)
					case "unstage":
						err, out = git.UnstageHunk(root, r, line), fmt.Sprintf("unstaged the hunk at %s:%d", r, line)
					case "discard":
						if err := m.aiRefuseUnsaved(abs, "discard", rel); err != nil {
							return "", nil, err
						}
						lvl = aiSyncDisk
						err, out = git.DiscardHunk(root, r, line), fmt.Sprintf("discarded the hunk at %s:%d; the file on disk is reverted", r, line)
					}
					if err != nil {
						return "", nil, fmt.Errorf("%s hunk at %s:%d: %v", op, r, line, err)
					}
					note, cmd := m.aiSyncIDE("", lvl)
					return out + note, cmd, nil
				})
			},
		},
		{
			Name: "git_stash",
			Description: "Stash: list; push (save tracked changes, optional message, and clean the working tree); " +
				"apply / pop / drop the entry at index (default 0). push/apply/pop are refused while a file has " +
				"unsaved edits in the editor; drop can not be undone.",
			Params: map[string]any{"op": enum("what to do", aiStashOps), "message": str("push: stash message"),
				"index": num("apply/pop/drop: stash index (stash@{index})")},
			Required: []string{"op"},
			Mutating: true, Class: ai.ClassGit,
			Gate: func(a map[string]any) (bool, string) {
				switch ai.ArgString(a, "op") {
				case "list":
					return false, ""
				case "drop":
					return true, "" // data loss
				}
				return true, ai.ClassGit
			},
			Describe: func(a map[string]any) string {
				switch op := ai.ArgString(a, "op"); op {
				case "list":
					return "List stashes"
				case "push":
					if msg := ai.ArgString(a, "message"); msg != "" {
						return fmt.Sprintf("Stash changes: %q", msg)
					}
					return "Stash changes"
				default:
					return "Stash " + op + " " + aiStashRef(a)
				}
			},
			Validate: func(a map[string]any) error {
				op := ai.ArgString(a, "op")
				if err := aiOneOf("op", op, aiStashOps); err != nil {
					return err
				}
				if ai.ArgInt(a, "index") < 0 {
					return errors.New("index must be 0 or more")
				}
				if op == "list" || op == "drop" {
					return nil
				}
				return aiPrecheck(call, func(m *Model) error { return m.aiRefuseUnsaved(root, "stash "+op, rel) })
			},
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				op, ref := ai.ArgString(a, "op"), aiStashRef(a)
				if op == "list" {
					es, err := git.StashList(root)
					if err != nil {
						return "", err
					}
					if len(es) == 0 {
						return "no stashes", nil
					}
					lines := make([]string, len(es))
					for i, e := range es {
						lines[i] = e.Ref + ": " + e.Message
					}
					return strings.Join(lines, "\n"), nil
				}
				return call(ctx, func(m *Model) (string, tea.Cmd, error) {
					if op != "drop" {
						if err := m.aiRefuseUnsaved(root, "stash "+op, rel); err != nil {
							return "", nil, err
						}
					}
					var err error
					var out string
					lvl := aiSyncDisk
					switch op {
					case "push":
						err, out = git.Stash(root, ai.ArgString(a, "message")), "stashed the changes as stash@{0}; the working tree is clean"
					case "apply":
						err, out = git.StashApply(root, ref), "applied "+ref+" (it is kept in the stash list)"
					case "pop":
						if err = git.StashApply(root, ref); err == nil {
							err = git.StashDrop(root, ref)
						}
						out = "popped " + ref + ": its changes are back and it is removed from the stash list"
					case "drop":
						err, out, lvl = git.StashDrop(root, ref), "dropped "+ref, aiSyncGit
					}
					note, cmd := m.aiSyncIDE("", lvl)
					if err != nil {
						// a failed apply / pop may still have changed files
						return "", cmd, fmt.Errorf("stash %s: %v%s", op, err, note)
					}
					return out + note, cmd, nil
				})
			},
		},
	}
}
