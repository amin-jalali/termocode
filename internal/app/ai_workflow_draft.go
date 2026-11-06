package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/cfgfile"
	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// ── workflow_draft (WFX-9, plan §5.3) ────────────────────────────────────
//
// The AI writes a whole workflow file. The text is checked like the
// loader checks it (a file with a problem, or a key in it, never reaches
// the card), then the card shows the file (a diff when it changes one),
// the write steps it will do, and that Create trusts this exact version.
// Cancel is first and focused. Create writes the file atomically (temp
// file and rename) and stores the trust for its hash outside the repo: the
// user has just read it on the card.

// aiWorkflowAllow is the Allow button's title on the workflow cards.
var aiWorkflowAllow = map[string]string{"workflow_draft": "Create", "workflow_dry_run": "Dry run", "workflow_run": "Run"}

// aiWorkflowCancelFirst are the cards whose safe button says Cancel.
var aiWorkflowCancelFirst = map[string]bool{"workflow_draft": true}

var aiWfxNameRe = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,39}$`)

// aiWfxDiffCap caps the lines of the card's diff.
const aiWfxDiffCap = 400

type aiWfxDraft struct {
	name, path string
	data       []byte
	file       *workflow.File
	old        []byte // the file on disk now
	exists     bool
}

func (d aiWfxDisk) checkDraft(a map[string]any) (aiWfxDraft, error) {
	name := strings.TrimSpace(ai.ArgString(a, "name"))
	if !aiWfxNameRe.MatchString(name) {
		return aiWfxDraft{}, errors.New("name: a lower-case letter first, then letters, digits, - or _ (at most 40); it is the file name")
	}
	content, _ := a["content"].(string)
	if strings.TrimSpace(content) == "" {
		return aiWfxDraft{}, errors.New("content is required: the whole JSONC file")
	}
	if d.ws == "" {
		return aiWfxDraft{}, errors.New("no workspace is open")
	}
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	data := []byte(content)
	f, _, err := workflow.Parse(name+".jsonc", data)
	if err != nil || len(data) > workflow.MaxFileBytes {
		return aiWfxDraft{}, errors.New("the file is " + aiWfxValidate(name, data) + "\nFix it and send it again (workflow_validate checks text)")
	}
	dr := aiWfxDraft{name: name, data: data, file: f,
		path: filepath.Join(d.ws, filepath.FromSlash(wire.ProjectDir), name+".jsonc")}
	old, err := os.ReadFile(dr.path)
	switch {
	case err == nil:
		dr.old, dr.exists = old, true
		if bytes.Equal(old, data) {
			return aiWfxDraft{}, errors.New("the file has exactly this text already")
		}
	case !errors.Is(err, os.ErrNotExist):
		return aiWfxDraft{}, err
	}
	return dr, nil
}

func (dr aiWfxDraft) rel() string { return wire.ProjectDir + "/" + dr.name + ".jsonc" }

func aiWfxLines(b []byte) []string {
	s := strings.TrimRight(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// describeDraft is the card text.
func (d aiWfxDisk) describeDraft(dr aiWfxDraft) string {
	var b strings.Builder
	fresh := aiWfxLines(dr.data)
	var lines []string
	if dr.exists {
		old := aiWfxLines(dr.old)
		if len(old)*len(fresh) > 4_000_000 { // too big for the line diff: all out, all in
			for _, l := range old {
				lines = append(lines, "- "+l)
			}
			for _, l := range fresh {
				lines = append(lines, "+ "+l)
			}
		} else {
			lines = aiLineDiff(old, fresh)
		}
		fmt.Fprintf(&b, "Change %s (%d lines now)\n", dr.rel(), len(fresh))
	} else {
		for _, l := range fresh {
			lines = append(lines, "+ "+l)
		}
		fmt.Fprintf(&b, "%s (new, %d lines)\n", dr.rel(), len(fresh))
	}
	if _, err := os.Stat(filepath.Join(d.private, dr.name+".jsonc")); err == nil && d.private != "" {
		b.WriteString("Your own workflow " + dr.name + " has the same name: this repo file wins.\n")
	}
	b.WriteString("Create writes the file and trusts this exact version (kept outside the repo). Nothing runs yet.\n\n")
	ws := d.writes(wire.Entry{Name: dr.name, Hash: workflow.HashBytes(dr.data), File: dr.file})
	if len(ws) > 0 {
		b.WriteString("Writes it will do when run (each asks you on a card until you allow it):\n  • " + strings.Join(ws, "\n  • ") + "\n\n")
	} else {
		b.WriteString("Writes it will do when run: none (it only reads and shows results).\n\n")
	}
	more := 0
	if len(lines) > aiWfxDiffCap {
		more = len(lines) - aiWfxDiffCap
		lines = lines[:aiWfxDiffCap]
	}
	for _, l := range lines {
		b.WriteString(aiCleanText(l) + "\n")
	}
	if more > 0 {
		fmt.Fprintf(&b, "… %d more lines\n", more)
	}
	return strings.TrimRight(b.String(), "\n")
}

// aiWorkflowDraftTool is workflow_draft for workspace ws.
func (m *Model) aiWorkflowDraftTool(ws string, call aiIDECaller) ai.Tool {
	disk := aiWfxDiskOf(ws)
	// seen: the file on disk when the card was drawn (name + new hash →
	// old hash). Create refuses when it changed since.
	var mu sync.Mutex
	seen := map[string]string{}
	key := func(dr aiWfxDraft) string { return dr.name + "\x00" + workflow.HashBytes(dr.data) }
	str := func(s string) map[string]any { return map[string]any{"type": "string", "description": s} }
	return ai.Tool{
		Name: "workflow_draft",
		Description: "Save a workflow file " + wire.ProjectDir + "/<name>.jsonc (new, or a change of one: send the whole " +
			"file). It must be valid (workflow_validate checks text). The user sees the file (a diff for a change) and its " +
			"write steps on a card; Create writes it and trusts that version. Nothing runs. Ask the user before you add a " +
			"write step. Never put a key in it: ${secret:NAME} only in headers, env and via.",
		Params: map[string]any{
			"name":    str("workflow name: the file name, [a-z][a-z0-9_-]; the file's \"name\" must match"),
			"content": str("the whole JSONC file"),
		},
		Required: []string{"name", "content"},
		// No Always allow class: every call asks.
		Mutating: true,
		Validate: func(a map[string]any) error { _, err := disk.checkDraft(a); return err },
		Describe: func(a map[string]any) string {
			dr, err := disk.checkDraft(a)
			if err != nil {
				return "Save a workflow file (invalid: " + aiCardText(err.Error(), 80) + ")"
			}
			mu.Lock()
			seen[key(dr)] = workflow.HashBytes(dr.old)
			mu.Unlock()
			return disk.describeDraft(dr)
		},
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			dr, err := disk.checkDraft(a)
			if err != nil {
				return "", err
			}
			mu.Lock()
			was, ok := seen[key(dr)]
			delete(seen, key(dr))
			mu.Unlock()
			if ok && was != workflow.HashBytes(dr.old) {
				return "", errors.New(dr.rel() + " changed while the card was open; read it again (workflow_read) and send the draft again")
			}
			if err := cfgfile.WriteFile(dr.path, dr.data); err != nil {
				return "", err
			}
			trusted := disk.root != "" && wire.Trust(disk.root, wire.Entry{Name: dr.name, Hash: workflow.HashBytes(dr.data)}) == nil
			steps, _, writes, _ := aiWfxCounts(dr.file)
			verb := "Created " + dr.rel()
			if dr.exists {
				verb = "Saved the change to " + dr.rel()
			}
			out := fmt.Sprintf("%s (valid: %d step(s), %d write(s)", verb, steps, writes)
			if trusted {
				out += "; trusted for this version"
			}
			out += "). Nothing ran. workflow_dry_run shows what it would read and write."
			if call == nil {
				return out, nil
			}
			return call(ctx, func(m *Model) (string, tea.Cmd, error) {
				m.wfxLoad(disk.ws)
				m.wfxSyncActivity()
				return out, tea.Batch(m.wfxSendProblems(), m.wfxShowPage(dr.name, false)), nil
			})
		},
	}
}
