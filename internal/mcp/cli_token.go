package mcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/lazyre"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// The GitHub CLI as a token source (CX-2): when gh is signed in, the
// GitHub MCP server takes the token `gh auth token` prints. termocode
// asks gh at each connect and keeps the token in memory only: gh rotates
// and revokes it, a copy on disk would go stale and leak scope.
//
// gh is found on PATH at call time and always run with a fixed argument
// list; its stdout is the token (registered as a secret), its stderr
// only ever reaches the MCP log through ai.Redact.

// ErrNoGH: the gh command is not installed (not on PATH).
var ErrNoGH = errors.New("the gh command is not installed")

// ErrGHNotLoggedIn: gh is installed but has no GitHub login.
var ErrGHNotLoggedIn = errors.New("gh is installed but not signed in: run gh auth login, or sign in on github.com instead")

// ghTimeout caps one gh run (tests shorten it).
var ghTimeout = 5 * time.Second

// GHAccount is one github.com account gh is signed in with.
type GHAccount struct {
	Login  string
	Active bool
	Scopes []string
}

// ghLoginRe is a GitHub login (it goes into gh's argument list).
var ghLoginRe = lazyre.New(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$`)

// ValidGHLogin reports a well-formed GitHub login.
func ValidGHLogin(s string) bool { return ghLoginRe.MatchString(s) }

// runGH runs gh with args and returns stdout, stderr.
func runGH(ctx context.Context, args ...string) (string, string, error) {
	path, err := exec.LookPath("gh")
	if err != nil {
		return "", "", ErrNoGH
	}
	ctx, cancel := context.WithTimeout(ctx, ghTimeout)
	defer cancel()
	cmd := spawn.CommandContext(ctx, path, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	cmd.Env = append(cmd.Environ(), "GH_PROMPT_DISABLED=1", "NO_COLOR=1", "GH_NO_UPDATE_NOTIFIER=1")
	cmd.WaitDelay = 200 * time.Millisecond // a child that keeps the pipes open
	err = cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", "", fmt.Errorf("gh did not answer in %v", ghTimeout)
	}
	return out.String(), errb.String(), err
}

// notLoggedIn reports gh's "not logged in" answer.
func notLoggedIn(stderr string) bool {
	s := strings.ToLower(stderr)
	return strings.Contains(s, "not logged in") || strings.Contains(s, "not logged into") || strings.Contains(s, "gh auth login")
}

// GHAccounts lists the github.com accounts of `gh auth status`, the
// active one first.
func GHAccounts(ctx context.Context) ([]GHAccount, error) {
	out, errOut, err := runGH(ctx, "auth", "status", "--hostname", "github.com")
	if errors.Is(err, ErrNoGH) {
		return nil, err
	}
	// Old gh versions print the status on stderr.
	acc := ParseGHStatus(out + "\n" + errOut)
	if len(acc) == 0 {
		if err != nil && !notLoggedIn(errOut) {
			return nil, fmt.Errorf("gh auth status: %s", ai.Redact(strings.TrimSpace(errOut)))
		}
		return nil, ErrGHNotLoggedIn
	}
	return acc, nil
}

var (
	ghAccountRe = lazyre.New(`Logged in to (\S+) (?:account|as) ([A-Za-z0-9-]+)`)
	ghActiveRe  = lazyre.New(`Active account:\s*(true|false)`)
	ghScopesRe  = lazyre.New(`Token scopes:\s*(.*)`)
)

// ParseGHStatus reads the text of `gh auth status`: the github.com
// accounts, the active one first (then in gh's order).
func ParseGHStatus(text string) []GHAccount {
	var out []GHAccount
	cur := -1
	for _, line := range strings.Split(text, "\n") {
		if m := ghAccountRe.FindStringSubmatch(line); m != nil {
			cur = -1
			if m[1] != "github.com" {
				continue
			}
			out = append(out, GHAccount{Login: m[2]})
			cur = len(out) - 1
			continue
		}
		if cur < 0 {
			continue
		}
		if m := ghActiveRe.FindStringSubmatch(line); m != nil {
			out[cur].Active = m[1] == "true"
		}
		if m := ghScopesRe.FindStringSubmatch(line); m != nil {
			for _, s := range strings.Split(m[1], ",") {
				if s = strings.Trim(strings.TrimSpace(s), "'\""); s != "" {
					out[cur].Scopes = append(out[cur].Scopes, s)
				}
			}
		}
	}
	if len(out) == 1 {
		out[0].Active = true // gh before multi-account support has no "Active" line
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Active && !out[j].Active })
	return out
}

// GHTokenFor asks gh for the token of a github.com account ("" = the
// active one). The token is registered as a secret; nothing is saved.
func GHTokenFor(ctx context.Context, user string) (string, error) {
	args := []string{"auth", "token", "--hostname", "github.com"}
	if user != "" {
		if !ValidGHLogin(user) {
			return "", fmt.Errorf("%q is not a GitHub login", user)
		}
		args = append(args, "--user", user)
	}
	out, errOut, err := runGH(ctx, args...)
	if err != nil {
		if errors.Is(err, ErrNoGH) {
			return "", err
		}
		if notLoggedIn(errOut) {
			return "", ErrGHNotLoggedIn
		}
		msg := strings.TrimSpace(errOut)
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("gh auth token: %s", ai.Redact(msg))
	}
	tok := strings.TrimSpace(out)
	if tok == "" || strings.ContainsAny(tok, " \n\t") {
		return "", errors.New("gh auth token printed no token")
	}
	ai.RegisterSecret(tok)
	return tok, nil
}
