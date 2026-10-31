package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/lspinstall"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// ast_grep (OMX-28): structural search and rewrite with ast-grep, when
// it is installed (the managed copy or one on PATH). A search runs at
// once; a rewrite is a file change: the review card shows it as a diff
// (or, with review off, the fileWrite confirm). Without ast-grep the
// tool opens its install card once per session.

const (
	aiAstGrepMaxMatches = 100
	aiAstGrepMaxFiles   = 50
	aiAstGrepTimeout    = time.Minute
)

// aiAstGrepBin is the ast-grep to run ("" when none). Linux has another
// program named sg, so only an sg that says it is ast-grep counts.
func aiAstGrepBin() string {
	if t, ok := lspinstall.Lookup("ast-grep"); ok {
		if st := lspinstall.StatusOf(t); st.State != lspinstall.Missing && st.Path != "" {
			return st.Path
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if out, err := spawn.CommandContext(ctx, "sg", "--version").Output(); err == nil && strings.Contains(string(out), "ast-grep") {
		return "sg"
	}
	return ""
}

// aiAstMatch is one line of `ast-grep run --json=stream`.
type aiAstMatch struct {
	File        string  `json:"file"`
	Lines       string  `json:"lines"`
	Replacement *string `json:"replacement"`
	Range       struct {
		ByteOffset struct {
			Start int `json:"start"`
			End   int `json:"end"`
		} `json:"byteOffset"`
		Start struct {
			Line int `json:"line"`
		} `json:"start"`
	} `json:"range"`
}

func (m *Model) aiAstGrepTool(root string, call aiIDECaller) ai.Tool {
	ws := ai.Workspace{Root: root}
	review := m.aiReviewEdits()
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	return ai.Tool{
		Name: "ast_grep", Description: "Structural code search with ast-grep: pattern is code with $VAR wildcards (\"fmt.Println($A)\"). " +
			"With rewrite, every match is replaced (\"log.Print($A)\") and the user reviews the change as a diff. " +
			"Use it for safe refactors across many files; for plain text use search.",
		Params: map[string]any{
			"pattern": str("ast-grep pattern, e.g. foo($A, $$$REST)"),
			"lang":    str("language (go, ts, python, rust …); optional when the files say it"),
			"path":    str("folder or file to search, relative to the workspace (default: root)"),
			"rewrite": str("replacement for each match, may use the pattern's $VARs (optional)"),
		},
		Required: []string{"pattern"},
		Validate: func(a map[string]any) error {
			if p := ai.ArgString(a, "path"); p != "" {
				_, err := ws.Resolve(p)
				return err
			}
			return nil
		},
		Gate: func(a map[string]any) (bool, string) {
			if ai.ArgString(a, "rewrite") == "" {
				return false, ""
			}
			if review {
				return true, ai.ClassReview
			}
			return true, ai.ClassFileWrite
		},
		Describe: func(a map[string]any) string {
			if r := ai.ArgString(a, "rewrite"); r != "" {
				return "AST rewrite: " + ai.ArgString(a, "pattern") + " → " + r
			}
			return "AST search: " + ai.ArgString(a, "pattern")
		},
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			bin := aiAstGrepBin()
			if bin == "" {
				_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) {
					if s := m.aiS(); !s.astGrepAsked && !m.confirmOpen {
						s.astGrepAsked = true
						return "", m.onToolRowSelected("ast-grep"), nil
					}
					return "", nil, nil
				})
				return "", errors.New("ast-grep is not installed. The user sees an install card; until it is installed, use search with regex=true")
			}
			matches, err := aiAstGrepRun(ctx, bin, ws, a)
			if err != nil {
				return "", err
			}
			if ai.ArgString(a, "rewrite") == "" {
				return aiAstGrepList(matches), nil
			}
			changes, err := aiAstGrepChanges(ws, matches)
			if err != nil || len(changes) == 0 {
				if err == nil {
					return "no matches; nothing to rewrite", nil
				}
				return "", err
			}
			if review {
				return aiReviewChanges(ctx, call, changes)
			}
			var out []string
			for _, c := range changes {
				s, err := call(ctx, func(m *Model) (string, tea.Cmd, error) { return m.aiWriteFile(c.abs, c.rel, c.text) })
				if err != nil {
					return strings.Join(append(out, "error: "+err.Error()), "\n"), nil
				}
				out = append(out, s)
			}
			return strings.Join(out, "\n"), nil
		},
	}
}

// aiAstGrepRun runs ast-grep in the workspace root.
func aiAstGrepRun(ctx context.Context, bin string, ws ai.Workspace, a map[string]any) ([]aiAstMatch, error) {
	args := []string{"run", "--pattern", ai.ArgString(a, "pattern"), "--json=stream"}
	if l := ai.ArgString(a, "lang"); l != "" {
		args = append(args, "--lang", l)
	}
	if r := ai.ArgString(a, "rewrite"); r != "" {
		args = append(args, "--rewrite", r)
	}
	target := "."
	if p := ai.ArgString(a, "path"); p != "" {
		abs, err := ws.Resolve(p)
		if err != nil {
			return nil, err
		}
		if rel, err := filepath.Rel(ws.Root, abs); err == nil {
			target = rel
		}
	}
	args = append(args, target)
	ctx, cancel := context.WithTimeout(ctx, aiAstGrepTimeout)
	defer cancel()
	cmd := spawn.CommandContext(ctx, bin, args...)
	cmd.Dir = ws.Root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("ast-grep stopped: %w", ctx.Err())
	}
	var ms []aiAstMatch
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	for sc.Scan() {
		var mt aiAstMatch
		if json.Unmarshal(sc.Bytes(), &mt) == nil && mt.File != "" {
			ms = append(ms, mt)
		}
	}
	if err != nil && len(ms) == 0 {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, errors.New("ast-grep: " + firstLineOf(msg))
		}
		if len(bytes.TrimSpace(out)) > 0 {
			return nil, err
		}
	}
	return ms, nil
}

func firstLineOf(s string) string { return strings.SplitN(s, "\n", 2)[0] }

// aiAstGrepList is the search result for the model.
func aiAstGrepList(ms []aiAstMatch) string {
	var b strings.Builder
	n := 0
	for _, mt := range ms {
		if ai.SecretFile(mt.File) {
			continue
		}
		if n == aiAstGrepMaxMatches {
			fmt.Fprintf(&b, "(showing %d of %d)\n", n, len(ms))
			break
		}
		n++
		fmt.Fprintf(&b, "%s:%d: %s\n", filepath.ToSlash(mt.File), mt.Range.Start.Line+1, strings.TrimSpace(firstLineOf(mt.Lines)))
	}
	if n == 0 {
		return "no matches"
	}
	return b.String()
}

// aiAstGrepChanges applies each match's replacement to its file.
func aiAstGrepChanges(ws ai.Workspace, ms []aiAstMatch) ([]aiFileChange, error) {
	byFile := map[string][]aiAstMatch{}
	var order []string
	for _, mt := range ms {
		if mt.Replacement == nil {
			continue
		}
		if _, ok := byFile[mt.File]; !ok {
			order = append(order, mt.File)
		}
		byFile[mt.File] = append(byFile[mt.File], mt)
	}
	if len(order) > aiAstGrepMaxFiles {
		return nil, fmt.Errorf("the rewrite changes %d files (at most %d at once); narrow it with path", len(order), aiAstGrepMaxFiles)
	}
	rel := aiRel(ws.Root)
	var changes []aiFileChange
	for _, f := range order {
		abs, err := ws.Resolve(f)
		if err != nil {
			return nil, err
		}
		if ai.SecretFile(abs) {
			continue
		}
		b, err := os.ReadFile(abs)
		if err != nil {
			return nil, err
		}
		text := string(b)
		fm := byFile[f]
		sort.Slice(fm, func(i, j int) bool { return fm[i].Range.ByteOffset.Start > fm[j].Range.ByteOffset.Start })
		limit := len(text) + 1
		for _, mt := range fm {
			s, e := mt.Range.ByteOffset.Start, mt.Range.ByteOffset.End
			if s < 0 || e > len(text) || s > e || e > limit {
				continue // out of range or overlapping: skip, never guess
			}
			text = text[:s] + *mt.Replacement + text[e:]
			limit = s
		}
		if text != string(b) {
			changes = append(changes, aiFileChange{abs: abs, rel: rel(abs), text: text})
		}
	}
	return changes, nil
}
