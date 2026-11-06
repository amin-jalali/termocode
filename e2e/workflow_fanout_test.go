//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// WFX-10..13 end to end in the real binary: a 3-item workflow fans out
// to one git worktree per item (two at once), a fake Codex edits each
// worktree, the tests step runs there, and each item's pr write asks on
// its gate card. Enter (Deny, first and focused) on all three: nothing
// is committed, pushed or sent (the fake gh is never called). Never the
// real codex or gh: scripts on a temp PATH.

const wfxFanoutFile = `{
  "version": 1, "name": "fanout", "title": "Fan out",
  "steps": [
    { "id": "src", "type": "source", "from": "shell", "argv": ["printf", "a\nb\nc\n"], "parse": "lines" },
    { "id": "fix", "type": "each", "items": "steps.src.items", "parallel": 2, "worktree": true, "base": "main",
      "steps": [
        { "id": "code", "type": "agent", "agent": "codex", "prompt": "Fix {{item.value}}." },
        { "id": "test", "type": "tests", "argv": ["sh", "-c", "test -f fixed.txt"] },
        { "id": "pr", "type": "write", "to": "pr", "title": "fix {{item.value}}", "body": "auto", "base": "main", "draft": true }
      ] }
  ]
}
`

const wfxFakeCodex = `#!/bin/sh
cat > /dev/null
echo "fixed" > fixed.txt
pwd >> "$WFX_LOG/codex"
echo '{"type":"item.completed","item":{"id":"i1","type":"agent_message","text":"fixed it"}}'
echo '{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":2}}'
`

const wfxFakeGH = `#!/bin/sh
echo "$@" >> "$WFX_LOG/gh"
exit 1
`

func TestWorkflowFanOutDenyPR(t *testing.T) {
	r := newSessionRig(t)
	bin, logs := t.TempDir(), t.TempDir()
	for name, body := range map[string]string{"codex": wfxFakeCodex, "gh": wfxFakeGH} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A private workflow file is the user's own: no trust card.
	dir := filepath.Join(r.home, ".config", "termocode", "workflows")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fanout.jsonc"), []byte(wfxFanoutFile), 0o644); err != nil {
		t.Fatal(err)
	}
	r.env = append(r.env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "WFX_LOG="+logs)

	tm := Start(t, StartOpts{Home: r.home, Workspace: r.ws, Env: r.env, Cols: 200, Rows: 48})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	tm.Palette("Workflows: Open")
	tm.WaitText("fanout", uiTimeout)
	tm.settle()
	tm.Send("r")
	tm.WaitText("Run fanout now", uiTimeout)
	tm.settle()
	tm.Send("\t") // Cancel → Run
	tm.Send(KeyEnter)

	// Three gate cards, one at a time: each names the item's branch;
	// Enter is Deny.
	for i := 0; i < 3; i++ {
		tm.WaitText("fanout wants to write", 90*time.Second)
		tm.WaitText("pull request termocode/agent-wf-", uiTimeout)
		tm.settle()
		tm.Send(KeyEnter)
		tm.settle()
	}

	type itemState struct {
		Key   string `json:"key"`
		State string `json:"state"`
		Env   struct {
			Worktree string `json:"worktree"`
			Branch   string `json:"branch"`
		} `json:"env"`
		Steps map[string]struct {
			State string `json:"state"`
		} `json:"steps"`
	}
	type runFile struct {
		State string `json:"state"`
		Steps map[string]struct {
			Each *struct {
				Items []itemState `json:"items"`
			} `json:"each"`
		} `json:"steps"`
		Writes []struct {
			State string `json:"state"`
		} `json:"writes"`
	}
	var rf runFile
	tm.WaitFor("the run ends with 3 denied writes", 60*time.Second, func([]string) bool {
		paths, _ := filepath.Glob(filepath.Join(r.home, ".local", "state", "termocode", "workflows", "*", "runs", "*.json"))
		for _, p := range paths {
			b, err := os.ReadFile(p)
			if err != nil || json.Unmarshal(b, &rf) != nil {
				continue
			}
			denied := 0
			for _, w := range rf.Writes {
				if w.State == "denied" {
					denied++
				}
			}
			if rf.State != "running" && denied == 3 {
				return true
			}
		}
		return false
	})
	fix := rf.Steps["fix"].Each
	if fix == nil || len(fix.Items) != 3 {
		t.Fatalf("each: %+v", rf.Steps["fix"])
	}
	for _, it := range fix.Items {
		if it.Env.Worktree == "" || it.Env.Branch == "" {
			t.Errorf("item %s has no worktree: %+v", it.Key, it.Env)
			continue
		}
		if b, err := os.ReadFile(filepath.Join(it.Env.Worktree, "fixed.txt")); err != nil || string(b) != "fixed\n" {
			t.Errorf("item %s: the agent's edit is not in its worktree: %q %v", it.Key, b, err)
		}
		if it.Steps["code"].State != "done" || it.Steps["test"].State != "done" || it.Steps["pr"].State != "denied" {
			t.Errorf("item %s steps %+v", it.Key, it.Steps)
		}
		// Denied: nothing was committed on the item's branch.
		out, err := exec.Command("git", "-C", r.ws, "log", "--oneline", "main.."+it.Env.Branch).CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "" {
			t.Errorf("item %s: commits on %s after Deny: %q %v", it.Key, it.Env.Branch, out, err)
		}
	}
	if b, err := os.ReadFile(filepath.Join(logs, "codex")); err != nil || strings.Count(string(b), "\n") != 3 {
		t.Errorf("codex ran %q %v, want 3 times", b, err)
	}
	// (gh may be asked for CI runs by the status bar; never for a PR.)
	b, _ := os.ReadFile(filepath.Join(logs, "gh"))
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "pr ") || strings.HasPrefix(line, "api ") {
			t.Errorf("gh was asked to write after Deny: %q", line)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(r.ws, "fixed.txt")); len(b) > 0 {
		t.Error("the agent wrote in the user's tree")
	}
	tm.Quit()
}
