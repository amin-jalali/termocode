package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentctl"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// ── Task dependency graph across agents (CMP-80) ─────────────────────────
//
// An agent records that the work in its worktree waits for the work in
// another one: `termocode worktree block <worktree>`, the control op
// worktree-block (the SDK) or the AI tool worktree_block. It is short
// metadata in worktrees.json (Meta.BlockedBy), so it needs no card, like
// a note. The WORKTREES card's line 4 says "waits for …" or "ready", and
// Worktree: Show Dependency Graph draws the waves: what can start now,
// what comes next.

const wtDepsPaletteID = "worktree-deps"

func init() {
	aiActionPolicies[wtDepsPaletteID] = aiAuto() // shows the graph card; read only
}

// wtDepsRows are the graph card's rows: a heading per wave, then one row
// per worktree with done, ready or waits.
func wtDepsRows(entries []worktrees.Entry) []confirm.KeyRow {
	waves := worktrees.Waves(entries)
	if len(waves) == 0 {
		return nil
	}
	var rows []confirm.KeyRow
	for i, w := range waves {
		rows = append(rows, confirm.KeyRow{What: "Wave " + strconv.Itoa(i+1)})
		for _, e := range w {
			mark := "ready"
			if worktrees.Settled(e) {
				mark = "done"
			}
			what := aiCleanText(e.Title())
			open := worktrees.OpenBlockers(entries, e)
			switch {
			case len(open) > 0 && mark != "done":
				mark = "waits"
				what += " · waits for " + aiCleanText(strings.Join(worktrees.Titles(open), ", "))
			default:
				if bs := worktrees.Blockers(entries, e); len(bs) > 0 {
					what += " · after " + aiCleanText(strings.Join(worktrees.Titles(bs), ", "))
				}
			}
			rows = append(rows, confirm.KeyRow{Key: mark, What: what})
		}
	}
	return rows
}

// worktreeDeps opens the graph card (Worktree: Show Dependency Graph).
func (m *Model) worktreeDeps() tea.Cmd {
	if m.wt == nil || !m.wt.loaded {
		return m.revealWorktrees()
	}
	rows := wtDepsRows(m.wt.entries)
	if len(rows) == 0 {
		return m.pushToastDetail(toast.Info, "No worktree waits for another",
			"Agents record it with termocode worktree block <worktree> or the worktree_block tool.")
	}
	return m.openKeyCard("Dependency graph · worktrees", rows, helpLocWorktrees(), nil)
}

// wtBlockedBy fills a control list row with what the worktree waits for.
func wtBlockedBy(entries []worktrees.Entry, e worktrees.Entry, w *agentctl.Worktree) {
	bs := worktrees.Blockers(entries, e)
	if len(bs) == 0 {
		return
	}
	w.BlockedBy = worktrees.Titles(bs)
	w.Waits = len(worktrees.OpenBlockers(entries, e)) > 0
}

// wtSetBlocker records (or drops, remove) that the worktree at path
// waits for the worktree arg names.
func wtSetBlocker(entries []worktrees.Entry, common, path, arg string, remove bool) (string, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "", errors.New("name the other worktree: a path, branch or name")
	}
	other, ok := wtFind(entries, arg)
	if !ok {
		return "", fmt.Errorf("%q is not a worktree of this repository", arg)
	}
	var err error
	changed := true
	uerr := worktrees.Update(common, func(mm map[string]worktrees.Meta) {
		if remove {
			changed = worktrees.RemoveBlocker(mm, path, other.Path)
			return
		}
		if err = worktrees.AddBlocker(mm, path, other.Path); err == nil {
			md := mm[path]
			md.LastSeen = clock.Now()
			mm[path] = md
		}
	})
	if err != nil {
		return "", err
	}
	if uerr != nil {
		return "", uerr
	}
	if remove && !changed {
		return "It did not wait for " + other.Title() + ".", nil
	}
	if remove {
		return "No longer waits for " + other.Title() + ".", nil
	}
	return "Waits for " + other.Title() + ".", nil
}

// aiWorktreeBlockTool is worktree_block for the chat and outside agents
// (short metadata only: no card).
func aiWorktreeBlockTool(root string) ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	return ai.Tool{
		Name: "worktree_block",
		Description: "Record that the work in one worktree waits for the work in another (a task dependency across agents), " +
			"or drop it with remove. The WORKTREES view draws the graph; a blocker is settled when its status is done, its PR merged or its issue closed.",
		Params: map[string]any{
			"worktree":  str("optional: the worktree that waits (a path or branch from worktrees_list); the workspace's worktree when left out"),
			"waits_for": str("the worktree it waits for: a path, branch or name from worktrees_list"),
			"remove":    map[string]any{"type": "boolean", "description": "optional: drop the dependency instead"},
		},
		Required: []string{"waits_for"},
		Describe: func(a map[string]any) string { return "Worktree waits for " + ai.ArgString(a, "waits_for") },
		Run: func(_ context.Context, a map[string]any) (string, error) {
			entries, common, err := wtRepoAt(root)
			if err != nil {
				return "", err
			}
			arg := strings.TrimSpace(ai.ArgString(a, "worktree"))
			if arg == "" {
				arg = wtRealDir(root)
			}
			e, ok := wtFind(entries, arg)
			if !ok {
				return "", fmt.Errorf("worktree %q is not a worktree of this repository (see worktrees_list)", arg)
			}
			return wtSetBlocker(entries, common, e.Path, ai.ArgString(a, "waits_for"), ai.ArgBool(a, "remove"))
		},
	}
}
