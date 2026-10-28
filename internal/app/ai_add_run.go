package app

import (
	"context"
	"errors"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/addai"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clistep"
	"github.com/amin-jalali/termocode/internal/secrets"
)

// The Add AI card's background work (ai_add.go): list a provider's
// models with a typed key, a server URL or a local server, or check a
// CLI's sign-in. Nothing here saves anything; the answer goes back to
// the Update goroutine, which saves only when the card still waits for
// it.

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

// addAISignInCmd checks a CLI's own sign-in; a signed-in CLI connects
// its kind with the CLI's built-in model list.
func addAISignInCmd(ctx context.Context, msg addai.CheckSignInMsg) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, addAICheckTimeout)
		defer cancel()
		title := ai.CLITitle(msg.CLI)
		switch aiCLICheck(ctx, msg.CLI, nil) {
		case clistep.StatusReady:
			return addAIResultMsg{gen: msg.Gen, entry: ai.ProviderEntry{Kind: msg.Kind, Method: ai.MethodCLI, CLI: msg.CLI},
				models: ai.CLIModels(msg.CLI)}
		case clistep.StatusMissing:
			return addAIResultMsg{gen: msg.Gen, fail: title + " is not on this machine."}
		case clistep.StatusLoggedOut:
			return addAIResultMsg{gen: msg.Gen, fail: title + " is not signed in on this machine."}
		}
		if ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled) {
			return nil
		}
		return addAIResultMsg{gen: msg.Gen, fail: "Could not check " + title + " on this machine."}
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
