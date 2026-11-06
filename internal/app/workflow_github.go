package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/ledger"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

// ── Workflows: git writes on GitHub (WFX-13) ────────────────────────────
//
// A pr write pushes the item's branch (a plain push, never forced) and
// opens the pull request with gh; github.comment comments with gh. Both
// only run after the write's gate card said yes (wire.Writer). A resumed
// write is looked up by its marker before it is sent again.

// wfxGHTimeout caps one gh call of a workflow write.
const wfxGHTimeout = 2 * time.Minute

type wfxGitHub struct {
	dir  string // the workspace (github.comment runs here)
	gh   ghci.Runner
	push func(dir, branch string) error
}

func newWFXGitHub(ws string) *wfxGitHub {
	return &wfxGitHub{dir: ws, gh: ghci.DefaultRunner(wfxGHTimeout), push: pushCurrent}
}

// PR is wire.Config.PR: push the branch, then gh pr create.
func (g *wfxGitHub) PR(ctx context.Context, p wire.PRCall) (ledger.Result, error) {
	if err := g.push(p.Dir, p.Branch); err != nil {
		return ledger.Result{}, fmt.Errorf("push %s: %w", p.Branch, err)
	}
	url, err := ghci.Create(ctx, g.gh, p.Dir, ghci.NewPR{Title: p.Title, Body: p.Body, Base: p.Base, Branch: p.Branch, Draft: p.Draft})
	if err != nil {
		return ledger.Result{}, err
	}
	return ledger.Result{ExternalID: url}, nil
}

// prNum reads a PR number ("7" or "#7").
func prNum(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimPrefix(strings.TrimSpace(s), "#"))
	if err != nil || n <= 0 {
		return 0, run.Permanent(fmt.Errorf("github.comment: %q is not a pull request number", s))
	}
	return n, nil
}

// Comment is wire.Config.GitHubComment.
func (g *wfxGitHub) Comment(ctx context.Context, pr, body string) (ledger.Result, error) {
	n, err := prNum(pr)
	if err != nil {
		return ledger.Result{}, err
	}
	out, errOut, err := g.gh(ctx, g.dir, "pr", "comment", strconv.Itoa(n), "--body", body)
	if err != nil {
		return ledger.Result{}, ghErr(err, errOut)
	}
	return ledger.Result{ExternalID: lastHTTPLine(out)}, nil
}

// Find is wire.Config.Find: a PR whose body, or a comment of the PR,
// holds the marker.
func (g *wfxGitHub) Find(ctx context.Context, spec workflow.WriteSpec, marker string) (ledger.Result, bool, error) {
	type withBody struct {
		URL  string `json:"url"`
		Body string `json:"body"`
	}
	var list []withBody
	switch spec.To {
	case "pr":
		out, errOut, err := g.gh(ctx, g.dir, "pr", "list", "--state", "all", "--search", marker+" in:body", "--json", "url,body", "--limit", "10")
		if err != nil {
			return ledger.Result{}, false, ghErr(err, errOut)
		}
		if err := json.Unmarshal([]byte(out), &list); err != nil {
			return ledger.Result{}, false, fmt.Errorf("gh pr list: %w", err)
		}
	case "github.comment":
		n, err := prNum(spec.PR)
		if err != nil {
			return ledger.Result{}, false, err
		}
		out, errOut, err := g.gh(ctx, g.dir, "pr", "view", strconv.Itoa(n), "--json", "comments")
		if err != nil {
			return ledger.Result{}, false, ghErr(err, errOut)
		}
		var v struct {
			Comments []withBody `json:"comments"`
		}
		if err := json.Unmarshal([]byte(out), &v); err != nil {
			return ledger.Result{}, false, fmt.Errorf("gh pr view: %w", err)
		}
		list = v.Comments
	default:
		return ledger.Result{}, false, nil
	}
	for _, x := range list {
		if strings.Contains(x.Body, marker) {
			return ledger.Result{ExternalID: x.URL}, true, nil
		}
	}
	return ledger.Result{}, false, nil
}

func ghErr(err error, stderr string) error {
	if s := strings.TrimSpace(stderr); s != "" {
		return errors.New("gh: " + lastLine(s))
	}
	return err
}

// lastHTTPLine is the last link gh printed.
func lastHTTPLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); strings.HasPrefix(l, "https://") || strings.HasPrefix(l, "http://") {
			return l
		}
	}
	return ""
}

// wfxRunSettings are the settings a run reads when it starts:
// workflows_parallel_max and workflows_agent_sandbox.
func wfxRunSettings() (parallelMax int, sandbox string) {
	v, _ := workflowSettingValue("setting-workflows-parallel-max")
	parallelMax, err := strconv.Atoi(v)
	if err != nil {
		parallelMax = workflow.DefaultParallelMax
	}
	sandbox, _ = workflowSettingValue("setting-workflows-agent-sandbox")
	return parallelMax, sandbox
}
