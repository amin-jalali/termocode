package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/prreview"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// `termocode review` (CMP-36): a short AI review of the branch, high-
// priority findings only. It prints them, or posts them on the pull
// request as one comment that later runs update. The GitHub workflow of
// Git: Set Up PR Review Bot runs it on every pull request.

const reviewUsage = `termocode review: a short AI review of this branch (P0/P1 findings only)

  termocode review [--base REF] [--post auto|N] [--fail-on p0|p1|never] [--json]

  --base REF      the branch the pull request goes into (default: origin/$GITHUB_BASE_REF
                  in GitHub Actions, else origin's default branch or main)
  --post auto|N   post the review on pull request N as one comment (auto: the pull
                  request of the GitHub Actions event); needs gh and GH_TOKEN
  --fail-on LEVEL exit 1 when a finding of this level or higher is found (default never)
  --json          print the findings as JSON

The review rules come from the sections of AGENTS.md whose heading has
"review" in it. The AI provider comes from your termocode AI settings or
the environment (ANTHROPIC_API_KEY, OPENAI_API_KEY, TERMOCODE_AI_*).
`

const (
	reviewDiffMax   = 64 << 10
	reviewTimeout   = 3 * time.Minute
	reviewMaxTokens = 2000
)

// reviewProvider builds the AI provider from the settings, the stored
// credentials and the environment. Tests replace it.
var reviewProvider = func(getenv func(string) string) (ai.Provider, ai.Config) {
	var s ai.Settings
	if p, err := ai.ConfigPath(); err == nil {
		s = ai.LoadSettings(p)
	}
	var c ai.Credentials
	if p, err := ai.CredentialsPath(); err == nil {
		c, _ = ai.LoadCredentials(p) // a missing file is fine: CI uses the environment
	}
	cfg := ai.Resolve(s, c, getenv)
	return ai.NewProvider(cfg), cfg
}

// reviewGH runs gh in dir. Tests replace it.
var reviewGH = func(dir string, args ...string) ([]byte, error) {
	cmd := spawn.Command("gh", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("gh %s: %w: %s", args[0], err, strings.TrimSpace(ai.Redact(string(out))))
	}
	return out, nil
}

func runReview(args []string) int {
	return runReviewIn(args, os.Stdout, os.Stderr, os.Getenv, os.Getwd)
}

func runReviewIn(args []string, out, errw io.Writer, getenv func(string) string, getwd func() (string, error)) int {
	fail := func(f string, a ...any) int {
		fmt.Fprintf(errw, "termocode review: "+f+"\n", a...)
		return 1
	}
	usage := func() int { fmt.Fprint(errw, reviewUsage); return 2 }
	var base, post string
	failOn := "never"
	asJSON := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		val := func() (string, bool) {
			if i+1 >= len(args) {
				return "", false
			}
			i++
			return args[i], true
		}
		var ok bool
		switch a {
		case "-h", "--help", "help":
			fmt.Fprint(out, reviewUsage)
			return 0
		case "--base":
			if base, ok = val(); !ok {
				return usage()
			}
		case "--post":
			if post, ok = val(); !ok {
				return usage()
			}
		case "--fail-on":
			if failOn, ok = val(); !ok {
				return usage()
			}
		case "--json":
			asJSON = true
		default:
			return usage()
		}
	}
	if !prreview.ValidFailOn(failOn) {
		return fail("--fail-on must be p0, p1 or never")
	}
	if strings.HasPrefix(base, "-") {
		return fail("--base %q is not a branch", base)
	}
	pr := 0
	if post != "" && post != "auto" {
		n, err := strconv.Atoi(strings.TrimPrefix(post, "#"))
		if err != nil || n <= 0 {
			return fail("--post must be auto or a pull request number")
		}
		pr = n
	}
	if post == "auto" {
		ev := getenv("GITHUB_EVENT_PATH")
		if ev == "" {
			return fail("--post auto works in GitHub Actions only (GITHUB_EVENT_PATH is not set)")
		}
		n, err := prreview.EventPR(ev)
		if err != nil {
			return fail("%v", err)
		}
		pr = n
	}

	cwd, err := getwd()
	if err != nil {
		return fail("no current folder: %v", err)
	}
	root, err := reviewRoot(cwd)
	if err != nil {
		return fail("not in a git repository")
	}
	if base == "" {
		if b := getenv("GITHUB_BASE_REF"); b != "" && !strings.HasPrefix(b, "-") {
			base = "origin/" + b
		} else if base, err = git.DefaultBase(root); err != nil {
			return fail("no base branch: %v (use --base)", err)
		}
	}
	diff, cut, err := git.BranchDiff(root, base, reviewDiffMax)
	if err != nil {
		return fail("could not read the branch against %s: %v", base, err)
	}

	var fs []prreview.Finding
	if strings.TrimSpace(diff) != "" {
		prov, cfg := reviewProvider(getenv)
		if prov == nil || !prov.Configured() {
			return fail("no AI provider: set ANTHROPIC_API_KEY or OPENAI_API_KEY, or run AI: Configure Provider… in termocode")
		}
		msgs := prreview.Prompt(base, diff, cut, prreview.LoadRules(root))
		ctx, cancel := context.WithTimeout(context.Background(), reviewTimeout)
		text, last := ai.Collect(prov.Stream(ctx, ai.Request{Messages: msgs, Model: cfg.Model, MaxTokens: reviewMaxTokens}))
		cancel()
		if last.Err != nil {
			return fail("the AI did not answer: %s", ai.Redact(last.Err.Error()))
		}
		fs = prreview.Parse(text)
	}

	body := prreview.Comment(fs, base, cut)
	if asJSON {
		if fs == nil {
			fs = []prreview.Finding{}
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(struct {
			Base     string             `json:"base"`
			Cut      bool               `json:"cut"`
			Findings []prreview.Finding `json:"findings"`
		}{base, cut, fs})
	} else {
		fmt.Fprint(out, body)
	}
	if pr > 0 {
		if err := postReview(root, pr, body); err != nil {
			return fail("could not post on pull request #%d: %v", pr, err)
		}
		fmt.Fprintf(errw, "Posted the review on pull request #%d\n", pr)
	}
	if prreview.Fails(fs, failOn) {
		return 1
	}
	return 0
}

// postReview updates the bot's earlier comment on the pull request (it
// starts with prreview.Marker), or adds a new one.
func postReview(dir string, pr int, body string) error {
	tmp, err := os.CreateTemp("", "termocode-review-*.md")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	file := "body=@" + filepath.ToSlash(tmp.Name())
	ids, err := reviewGH(dir, "api", "--paginate",
		"repos/{owner}/{repo}/issues/"+strconv.Itoa(pr)+"/comments",
		"--jq", `.[] | select(.body | startswith("`+prreview.Marker+`")) | .id`)
	if err != nil {
		return err
	}
	if id := lastLine(string(ids)); id != "" {
		if _, err := strconv.ParseInt(id, 10, 64); err != nil {
			return errors.New("gh gave a comment id that is not a number")
		}
		_, err = reviewGH(dir, "api", "-X", "PATCH", "repos/{owner}/{repo}/issues/comments/"+id, "-F", file)
		return err
	}
	_, err = reviewGH(dir, "api", "-X", "POST", "repos/{owner}/{repo}/issues/"+strconv.Itoa(pr)+"/comments", "-F", file)
	return err
}

// reviewRoot is the top folder of the git repository dir is in.
func reviewRoot(dir string) (string, error) {
	cmd := spawn.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func lastLine(s string) string {
	lines := strings.Fields(s)
	if len(lines) == 0 {
		return ""
	}
	return lines[len(lines)-1]
}
