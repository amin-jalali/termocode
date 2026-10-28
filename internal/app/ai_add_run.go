package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clilogin"
	"github.com/amin-jalali/termocode/internal/clistep"
	"github.com/amin-jalali/termocode/internal/lspinstall"
	"github.com/amin-jalali/termocode/internal/secrets"
)

// The Add AI card's background work (ai_add.go): list a provider's
// models with a typed key, a server URL or a local server, or run a
// CLI's sign-in path (clilogin). Nothing here saves anything; the
// answer goes back to the Update goroutine, which saves only when the
// card still waits for it.

// aiListModels lists a provider's models (a var for tests: they never
// reach the network).
var aiListModels = ai.ListModels

// addAICheckTimeout bounds one check (ListModels has its own 10 s).
const addAICheckTimeout = 15 * time.Second

// addAIResultMsg is the answer to one request of the card.
type addAIResultMsg struct {
	gen    int
	entry  ai.ProviderEntry // the provider to save
	key    string           // the key to save as typed ("" none)
	models []ai.ModelInfo
	fail   string // one sentence for the card; "" = connected
}

// addAIListCmd lists entry's models with key (typed: a reference is
// read first) and answers with the result. fail turns an error into the
// card's sentence.
func addAIListCmd(ctx context.Context, gen int, entry ai.ProviderEntry, typed string, fail func(error) string) tea.Cmd {
	root := aiWorkspaceRoot()
	return func() tea.Msg {
		key := typed
		if secrets.IsRef(typed) {
			ref, err := secrets.ParseRef(typed)
			v := ""
			if err == nil {
				v, err = newRefResolver(root).Resolve(ref)
			}
			if err != nil || v == "" {
				return addAIResultMsg{gen: gen, fail: "That reference gave no key."}
			}
			key = v
		}
		ai.RegisterSecret(key) // before the request: any error text is redacted
		cfg := ai.Config{Kind: entry.Kind, APIKey: key, BaseURL: entry.BaseURL}
		ctx, cancel := context.WithTimeout(ctx, addAICheckTimeout)
		defer cancel()
		ms, err := aiListModels(ctx, cfg)
		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return nil // the card was closed: nothing to say
			}
			recordError("Add AI " + entry.Kind + ": " + err.Error()) // the log gets the detail (redacted), the card a sentence
			return addAIResultMsg{gen: gen, fail: fail(err)}
		}
		return addAIResultMsg{gen: gen, entry: entry, key: typed, models: ms}
	}
}

// addAISignInFlow is one running sign-in path (a *clilogin.Flow; tests
// use a fake).
type addAISignInFlow interface {
	Updates() <-chan clilogin.State
	Write(code string) error
	Cancel()
}

// aiCLISignIn starts a CLI's sign-in path: install when missing, the
// CLI's own sign-in when signed out, then its models (a var for tests:
// they never run a real CLI, npm or the network).
var aiCLISignIn = func(ctx context.Context, cfg clilogin.Config) addAISignInFlow { return clilogin.Run(ctx, cfg) }

// aiCLIInstalled reports a sign-in CLI on the PATH or in termocode's
// tools folder (a var for tests). Only a file check: nothing runs.
var aiCLIInstalled = func(cli string) bool {
	env := lspinstall.EnvWithToolsBin()
	path := ""
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			path = v
		}
	}
	for _, d := range filepath.SplitList(path) {
		if d == "" {
			continue
		}
		if fi, err := os.Stat(filepath.Join(d, clistep.Bin(cli))); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return true
		}
	}
	return false
}

// addAIInstalled are the sign-in CLIs on this machine, for the card.
func addAIInstalled() map[string]bool {
	out := map[string]bool{}
	for _, cli := range []string{ai.CLIClaude, ai.CLICodex} {
		out[cli] = aiCLIInstalled(cli)
	}
	return out
}

// addAISignInStateMsg is one state of the card's sign-in flow.
type addAISignInStateMsg struct {
	gen       int
	kind, cli string
	flow      addAISignInFlow
	st        clilogin.State
}

// addAISignInNext waits for the flow's next state (nil once it closed:
// it was canceled).
func addAISignInNext(gen int, kind, cli string, f addAISignInFlow) tea.Cmd {
	return func() tea.Msg {
		st, ok := <-f.Updates()
		if !ok {
			return nil
		}
		return addAISignInStateMsg{gen: gen, kind: kind, cli: cli, flow: f, st: st}
	}
}

// addAIKeyFail is the sentence for a failed key check.
func addAIKeyFail(name string) func(error) string {
	return func(err error) string {
		if ai.KeyRejected(err) {
			return "That key was not accepted."
		}
		return "Could not reach " + name + ". Check your connection."
	}
}
