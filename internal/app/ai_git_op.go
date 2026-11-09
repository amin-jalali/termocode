package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

// The git tools with an op argument (git parity): one tool per area
// (branches, tags, remotes, history, rebase, conflicts, lines, discard,
// repository, pull requests), each op asking where the GUI asks
// (ai_git_cards.go). aiGitOpSpec is one such tool; aiGitOpTool makes it
// an ai.Tool with its gate, the unsaved-buffer check of the ops that
// rewrite files, and the IDE refresh after each change.

// aiAsk is how an op asks.
type aiAsk int

const (
	aiAskNo    aiAsk = iota // a read: runs at once
	aiAskGit                // a card with "Always allow git" (the GUI does not ask)
	aiAskEvery              // a Cancel-first card every time (the GUI asks every time)
	aiAskCard               // opens the GUI's own card (a typed name): that card is the question
)

// aiGitOpSpec is a git tool with ops.
type aiGitOpSpec struct {
	name, desc string
	ops        map[string]aiAsk
	def        string // the op when none is given ("" = op is required)
	params     map[string]any
	// disk (optional): the op rewrites files of the working tree, so it
	// is refused while a file has unsaved edits in the editor.
	disk     func(op string, a map[string]any) bool
	check    func(op string, a map[string]any) error // per-op argument checks (no git)
	describe func(op string, a map[string]any) string
	run      func(ctx context.Context, op string, a map[string]any) (string, error)
}

// aiGitOpOf is the call's op (the default when none is given).
func (s aiGitOpSpec) opOf(a map[string]any) string {
	if op := strings.TrimSpace(ai.ArgString(a, "op")); op != "" {
		return op
	}
	return s.def
}

func (s aiGitOpSpec) opNames() []string {
	ops := make([]string, 0, len(s.ops))
	for op := range s.ops {
		ops = append(ops, op)
	}
	sort.Strings(ops)
	return ops
}

// aiGitOpTool makes s a tool of the workspace root.
func aiGitOpTool(root string, call aiIDECaller, s aiGitOpSpec) ai.Tool {
	rel := aiRel(root)
	ops := s.opNames()
	params := map[string]any{"op": map[string]any{"type": "string", "enum": ops, "description": "what to do"}}
	for k, v := range s.params {
		params[k] = v
	}
	var required []string
	if s.def == "" {
		required = []string{"op"}
	}
	disk := func(op string, a map[string]any) bool { return s.disk != nil && s.disk(op, a) }
	refuse := func(ctx context.Context, op string) error {
		if call == nil {
			return nil
		}
		_, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
			return "", nil, m.aiRefuseUnsaved(root, strings.ReplaceAll(s.name, "_", " ")+" "+op, rel)
		})
		if errors.Is(err, errAIChatStopped) {
			return nil
		}
		return err
	}
	return ai.Tool{
		Name: s.name, Description: s.desc, Params: params, Required: required,
		Mutating: true, Class: ai.ClassGit,
		Gate: func(a map[string]any) (bool, string) {
			ask, ok := s.ops[s.opOf(a)]
			switch {
			case !ok, ask == aiAskEvery:
				return true, ""
			case ask == aiAskGit:
				return true, ai.ClassGit
			}
			return false, ""
		},
		Describe: func(a map[string]any) string {
			op := s.opOf(a)
			if s.describe != nil {
				if d := s.describe(op, a); d != "" {
					return d
				}
			}
			return strings.ReplaceAll(s.name, "_", " ") + " " + op
		},
		Validate: func(a map[string]any) error {
			op := s.opOf(a)
			if _, ok := s.ops[op]; !ok {
				return fmt.Errorf("give op: %s", strings.Join(ops, ", "))
			}
			if s.check != nil {
				if err := s.check(op, a); err != nil {
					return err
				}
			}
			if disk(op, a) && call != nil {
				return aiPrecheck(call, func(m *Model) error {
					return m.aiRefuseUnsaved(root, strings.ReplaceAll(s.name, "_", " ")+" "+op, rel)
				})
			}
			return nil
		},
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			op := s.opOf(a)
			if _, ok := s.ops[op]; !ok {
				return "", fmt.Errorf("give op: %s", strings.Join(ops, ", "))
			}
			if s.check != nil {
				if err := s.check(op, a); err != nil {
					return "", err
				}
			}
			if disk(op, a) {
				if err := refuse(ctx, op); err != nil {
					return "", err
				}
			}
			out, err := s.run(ctx, op, a)
			if ask := s.ops[op]; ask == aiAskNo || ask == aiAskCard || call == nil {
				return out, err
			}
			lvl := aiSyncGit
			if disk(op, a) {
				lvl = aiSyncDisk
			}
			note := aiSyncAfter(ctx, call, "", lvl)
			if err != nil {
				return "", fmt.Errorf("%w%s", err, note) // a stopped op may still have changed files
			}
			return out + note, nil
		},
	}
}

// aiNeed is the error for a missing argument.
func aiNeed(op, what string) error { return fmt.Errorf("%s needs %s", op, what) }

// aiName reads a name argument; ok=false when it is empty. A name that
// starts with "-" is refused (git would read it as an option).
func aiName(a map[string]any, key string) (string, error) {
	v := strings.TrimSpace(ai.ArgString(a, key))
	if strings.HasPrefix(v, "-") {
		return "", fmt.Errorf("%s %q may not start with -", key, v)
	}
	return v, nil
}

// aiNeedName is aiName for a required argument.
func aiNeedName(op string, a map[string]any, key string) (string, error) {
	v, err := aiName(a, key)
	if err == nil && v == "" {
		err = aiNeed(op, key)
	}
	return v, err
}

// aiGUICard runs open on Update to show one of the GUI's own cards; it
// refuses while another card or dialog is open.
func aiGUICard(ctx context.Context, call aiIDECaller, open func(m *Model) (string, tea.Cmd, error)) (string, error) {
	if call == nil {
		return "", errors.New("the IDE is not available")
	}
	return call(ctx, func(m *Model) (string, tea.Cmd, error) {
		if m.confirmOpen || m.promptOpen || m.pickerOpen {
			return "", nil, errors.New("termocode is asking the user something else; try again after they answer")
		}
		return open(m)
	})
}

// aiStr and aiBool are JSON-Schema params.
func aiStr(d string) map[string]any  { return map[string]any{"type": "string", "description": d} }
func aiBool(d string) map[string]any { return map[string]any{"type": "boolean", "description": d} }
func aiInt(d string) map[string]any  { return map[string]any{"type": "integer", "description": d} }

// aiWithoutTool is tools without the one named name (a tool of the same
// name follows).
func aiWithoutTool(tools []ai.Tool, name string) []ai.Tool {
	out := tools[:0:0]
	for _, t := range tools {
		if t.Name != name {
			out = append(out, t)
		}
	}
	return out
}

// aiGitToolsFor is tools with the git tools added; the git_branch of
// aiGitTools (with ops) replaces the list-only one of ai.WorkspaceTools.
func aiGitToolsFor(tools []ai.Tool, root string, call aiIDECaller) []ai.Tool {
	return append(aiWithoutTool(tools, "git_branch"), aiGitTools(root, call)...)
}
