package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// ── workflow_dry_run and workflow_run (WFX-9, plan §5.3, §4.5) ──────────
//
// Both ask on a card every time, Deny first and focused. A dry run reads
// the sources for real (read-only) and runs plan steps (AI money: the
// card says so first); it never writes. A run may write to outside
// systems: each write asks again on its own gate card (Deny first)
// unless the user allowed that step for this file version. A repo file
// the user has not trusted is refused before the card: trust stays in
// the Workflows view, where the user reads the file.

// aiWfxDryWait is how long workflow_dry_run waits for the dry run to end
// before it answers "still running".
var aiWfxDryWait = 90 * time.Second

// checkRun finds a workflow that can run now (as far as the disk says).
func (d aiWfxDisk) checkRun(a map[string]any) (wire.Entry, error) {
	e, err := d.find(ai.ArgString(a, "name"))
	if err != nil {
		return e, err
	}
	switch {
	case e.Broken():
		return e, errors.New(e.Name + " has problems in its file; fix them first (workflow_read lists them)")
	case !e.Trusted:
		return e, errors.New(e.Name + " is a repo file the user has not trusted yet: ask the user to open it in the Workflows view " +
			"and press r or d (they read it on the trust card first)")
	case !workflowsEnabled():
		return e, errors.New("workflows are turned off (workflows_enabled); only the user turns them on")
	}
	return e, nil
}

// aiWfxPlanCost is the card's line about what the plan steps cost.
func aiWfxPlanCost(f *workflow.File) string {
	if f == nil {
		return ""
	}
	var models []string
	n := 0
	workflow.Walk(f.Steps, func(s *workflow.Step) bool {
		if s.Plan == nil {
			return true
		}
		n++
		model := s.Plan.Model
		if s.Plan.Profile != "" {
			model = "profile " + s.Plan.Profile
		}
		if model == "" {
			model = "quick"
		}
		models = append(models, s.ID+": "+model)
		return true
	})
	if n == 0 {
		return "Plan cost: none (no plan steps; this dry run costs no AI money)."
	}
	return fmt.Sprintf("Plan cost: %d plan step(s) call the AI once each (%s), with up to %d item(s) per call. This costs AI money; "+
		"the run page shows the real cost.", n, strings.Join(models, ", "), f.Limits.MaxItems)
}

func (d aiWfxDisk) describeDry(e wire.Entry) string {
	_, reads, writes, _ := aiWfxCounts(e.File)
	var b strings.Builder
	fmt.Fprintf(&b, "Dry run %s · %s\n", e.Name, wfxTitle(e))
	fmt.Fprintf(&b, "It reads %d source(s) for real (read-only).\n", reads)
	b.WriteString(aiWfxPlanCost(e.File) + "\n")
	fmt.Fprintf(&b, "Run steps (ai, agent, shell, tests) are only shown. The %d write(s) are listed with their full text and never sent.", writes)
	return b.String()
}

func (d aiWfxDisk) describeRun(e wire.Entry) string {
	_, reads, writes, plans := aiWfxCounts(e.File)
	var b strings.Builder
	fmt.Fprintf(&b, "Run %s now · %s\n", e.Name, wfxTitle(e))
	fmt.Fprintf(&b, "It reads %d source(s), runs its steps (%d plan step(s) cost AI money) and may write %d time(s) to outside systems:\n", reads, plans, writes)
	for _, w := range d.writes(e) {
		b.WriteString("  • " + w + "\n")
	}
	b.WriteString("Every write asks you on its own card (Deny first) unless you allowed it for this file version.")
	return b.String()
}

// aiWorkflowRunTools are workflow_dry_run and workflow_run of workspace ws.
func (m *Model) aiWorkflowRunTools(ws string, call aiIDECaller) []ai.Tool {
	disk := aiWfxDiskOf(ws)
	name := map[string]any{"name": map[string]any{"type": "string", "description": "workflow name from workflow_list"}}
	start := func(ctx context.Context, a map[string]any, dry bool) (string, *run.Handle, error) {
		e, err := disk.checkRun(a)
		if err != nil {
			return "", nil, err
		}
		if call == nil {
			return "", nil, errors.New("no IDE to run it in")
		}
		var h *run.Handle
		id, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
			m.wfxLoad(disk.ws)
			cur, ok := wire.Find(m.wfxS().list, e.Name)
			if !ok || cur.Hash != e.Hash {
				return "", nil, errors.New(e.Name + " changed since the card; read it again")
			}
			if m.wfxRunning(cur.Name) != nil {
				return "", nil, errors.New(cur.Name + " is running already (workflow_runs shows it)")
			}
			id, hh, cmd, err := m.wfxStartID(cur, dry)
			h = hh
			return id, cmd, err
		})
		return id, h, err
	}
	return []ai.Tool{
		{
			Name: "workflow_dry_run",
			Description: "Dry run a workflow: its sources are read for real (read-only) and its plan steps run (they cost AI " +
				"money), run steps are only shown and writes are listed with their full text, never sent. The user confirms " +
				"on a card. It waits for the dry run and answers with what it read and would write.",
			Params:   name,
			Required: []string{"name"},
			// No Always allow class: every call asks.
			Mutating: true,
			Validate: func(a map[string]any) error { _, err := disk.checkRun(a); return err },
			Describe: func(a map[string]any) string {
				e, err := disk.checkRun(a)
				if err != nil {
					return "Dry run a workflow (" + aiCardText(err.Error(), 80) + ")"
				}
				return disk.describeDry(e)
			},
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				id, h, err := start(ctx, a, true)
				if err != nil {
					return "", err
				}
				if h == nil {
					return "Dry run " + id + " started; workflow_runs with run=" + id + " shows it.", nil
				}
				select {
				case <-h.Done():
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(aiWfxDryWait):
					return "Dry run " + id + " still runs; workflow_runs with run=" + id + " shows it later.", nil
				}
				rf := h.Snapshot()
				var f *workflow.File
				if e, ok := wire.Find(disk.load(), rf.Workflow); ok {
					f = e.File
				}
				return "Dry run finished (nothing was written):\n" + aiWfxRunDetail(rf, f), nil
			},
		},
		{
			Name: "workflow_run",
			Description: "Run a workflow now, for real. The user confirms on a card first; then every write asks the user " +
				"again on its own card unless they allowed it for this file version. It answers at once with the run id; " +
				"workflow_runs with run=<id> shows how it went. Offer workflow_dry_run first.",
			Params:   name,
			Required: []string{"name"},
			// No Always allow class: every call asks.
			Mutating: true,
			Validate: func(a map[string]any) error { _, err := disk.checkRun(a); return err },
			Describe: func(a map[string]any) string {
				e, err := disk.checkRun(a)
				if err != nil {
					return "Run a workflow (" + aiCardText(err.Error(), 80) + ")"
				}
				return disk.describeRun(e)
			},
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				id, _, err := start(ctx, a, false)
				if err != nil {
					return "", err
				}
				return "Run " + id + " started. Each write asks the user on a card; workflow_runs with run=" + id + " shows how it went.", nil
			},
		},
	}
}
