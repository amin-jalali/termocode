package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/addai"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/secrets"
)

// ── Add AI (AIP-3) ──────────────────────────────────────────────────────
//
// AI: Add AI… opens one card (internal/addai) that connects an AI: an
// API key, a local server, a server URL, or a CLI's sign-in that is
// already there. The card only asks; this file runs its requests off the
// Update goroutine (ai_add_run.go) and saves only on success:
//
//	the key        → the secret store (ai.SaveCredentials), as typed
//	the provider   → ai/providers.json (one entry per kind)
//	its models     → the models cache (ai.SaveModelsCache)
//
// config.json is never written. Esc at any step closes the card, stops
// the request and drops its late answer, so nothing is left behind.

// addAINoAI is the AI policy reason of the card's command.
const addAINoAI = "a card for the user; keys and connected AIs stay with the user (the AI shows them with ai_view_open)"

// openAddAI opens the card. A key is saved in the secret store, so a
// locked store asks for its passphrase first.
func (m *Model) openAddAI() tea.Cmd {
	if c, wait := m.secretsOpenFirst((*Model).openAddAI); wait {
		return c
	}
	m.addAI = addai.New(addai.Options{Connected: m.aiS().providers, KeyNote: secretPromptNote("")})
	m.addAI.SetSize(m.w, m.h)
	m.addAIOpen = true
	return nil
}

// closeAddAI closes the card and stops its request.
func (m *Model) closeAddAI() {
	m.addAIOpen = false
	m.addAIStop()
}

func (m *Model) addAIStop() {
	if s := m.aiS(); s.addAICancel != nil {
		s.addAICancel()
		s.addAICancel = nil
	}
}

// addAIContext is a fresh context for one request (the last one stops).
func (m *Model) addAIContext() context.Context {
	m.addAIStop()
	ctx, cancel := context.WithCancel(context.Background())
	m.aiS().addAICancel = cancel
	return ctx
}

// routeToAddAI sends a key, a click or a resize to the open card.
func (m Model) routeToAddAI(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.applyLayout()
		m.addAI.SetSize(m.w, m.h)
		return m, nil
	case tea.MouseMsg:
		if m.mouseProbe != nil {
			for _, t := range m.addAITargets() {
				if msg.Y == t.Y && msg.X >= t.X && msg.X < t.X+t.W {
					m.probed(t.ID)
					return m, nil
				}
			}
			m.probed("addai")
			return m, nil
		}
		var cmd tea.Cmd
		m.addAI, cmd = m.addAI.HandleMouse(msg.X, msg.Y, msg.Action, msg.Button)
		return m, cmd
	}
	var cmd tea.Cmd
	m.addAI, cmd = m.addAI.Update(msg)
	return m, cmd
}

// addAITargets are the card's buttons and choices for the hit map.
func (m Model) addAITargets() []hitTarget {
	var out []hitTarget
	for _, r := range m.addAI.OptionRects() {
		out = append(out, hitTarget{ID: "addai:pick:" + r.ID, Label: r.Title, X: r.X, Y: r.Y, W: r.W, H: 1})
	}
	for _, b := range m.addAI.ButtonRects() {
		out = append(out, hitTarget{ID: "addai:" + b.ID, Label: b.Title, X: b.X, Y: b.Y, W: b.W, H: 1})
	}
	return out
}

// handleAddAIMsg runs the card's requests and their answers. ok=false:
// not ours.
func (m *Model) handleAddAIMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case addai.CancelMsg:
		m.closeAddAI()
		return nil, true
	case addai.DoneMsg:
		m.closeAddAI()
		m.itermocRefreshNow()
		return nil, true
	case addai.ConnectKeyMsg:
		if !m.addAIOpen {
			return nil, true
		}
		if p := secrets.TypedProblem(msg.Key); p != "" {
			m.addAI.Fail(msg.Gen, p)
			return nil, true
		}
		e, _ := addai.EntryOf(msg.Kind)
		return addAIListCmd(m.addAIContext(), msg.Gen, ai.ProviderEntry{Kind: msg.Kind, Method: ai.MethodKey},
			msg.Key, addAIKeyFail(e.Name)), true
	case addai.ConnectURLMsg:
		if !m.addAIOpen {
			return nil, true
		}
		if p := secrets.TypedProblem(msg.Key); msg.Key != "" && p != "" {
			m.addAI.Fail(msg.Gen, p)
			return nil, true
		}
		return addAIListCmd(m.addAIContext(), msg.Gen, ai.ProviderEntry{Kind: ai.KindCompatible, Method: ai.MethodURL, BaseURL: msg.URL},
			msg.Key, func(err error) string {
				if ai.KeyRejected(err) {
					return "That key was not accepted."
				}
				return "Could not reach that server. Check the URL."
			}), true
	case addai.DetectLocalMsg:
		if !m.addAIOpen {
			return nil, true
		}
		e, _ := addai.EntryOf(msg.Kind)
		entry := ai.ProviderEntry{Kind: msg.Kind, Method: ai.MethodLocal}
		if old, ok := m.aiS().providers[msg.Kind]; ok && old.Method == ai.MethodLocal {
			entry.BaseURL = old.BaseURL // a server the user moved stays where it is
		}
		return addAIListCmd(m.addAIContext(), msg.Gen, entry, "", func(error) string {
			return e.Name + " is not running on this machine."
		}), true
	case addai.CheckSignInMsg:
		if !m.addAIOpen {
			return nil, true
		}
		return addAISignInCmd(m.addAIContext(), msg), true
	case addAIResultMsg:
		return m.onAddAIResult(msg), true
	}
	return nil, false
}

// onAddAIResult saves a connected AI, or shows why not. An answer the
// card no longer waits for (closed, Back, Try again) is dropped unsaved.
func (m *Model) onAddAIResult(msg addAIResultMsg) tea.Cmd {
	if !m.addAIOpen || msg.gen != m.addAI.Gen() || !m.addAI.Busy() {
		return nil
	}
	m.aiS().addAICancel = nil
	if msg.fail != "" {
		m.addAI.Fail(msg.gen, msg.fail)
		return nil
	}
	if err := m.saveAddedAI(msg); err != nil {
		recordError("Add AI " + msg.entry.Kind + ": " + err.Error()) // redacted there; the card says it plainly
		m.addAI.Fail(msg.gen, "Could not save it. Check the secret store, then try again.")
		return nil
	}
	ids := make([]string, 0, len(msg.models))
	for _, mi := range msg.models {
		ids = append(ids, mi.ID)
	}
	m.addAI.Finish(msg.gen, ids)
	return nil
}

// saveAddedAI writes the key (secret store), the providers.json entry and
// the model cache, then reloads the AI state.
func (m *Model) saveAddedAI(msg addAIResultMsg) error {
	s := m.aiS()
	if msg.key != "" {
		path, err := ai.CredentialsPath()
		if err != nil {
			return err
		}
		creds, err := ai.LoadCredentials(path)
		if err != nil {
			return err
		}
		if creds.Keys == nil {
			creds.Keys = map[string]string{}
		}
		creds.Keys[msg.entry.Kind] = strings.TrimSpace(msg.key)
		if err := ai.SaveCredentials(path, creds); err != nil {
			return err
		}
	}
	path, err := ai.ProvidersPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		// The first entry: keep the providers the setup already had.
		if err := ai.SaveProviders(path, s.providers); err != nil {
			return err
		}
	}
	now := clock.Now()
	entry := msg.entry
	entry.AddedAt = now.Unix()
	if err := ai.SaveProvider(path, entry); err != nil {
		return err
	}
	if cache, err := ai.LoadModelsCache(); err == nil { // only a cache: a failure is no error
		cache.Put(entry.Kind, msg.models, now)
		_ = ai.SaveModelsCache(cache)
	}
	s.reload()
	m.syncAIInline()
	return nil
}
