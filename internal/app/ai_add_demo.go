package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/addai"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clilogin"
	"github.com/amin-jalali/termocode/internal/connectcard"
)

// addAISignInDemoLink is the demo's sign-in link (not a real one).
const addAISignInDemoLink = "https://auth.openai.com/oauth/authorize?client_id=demo&state=demo-not-real"

// addAISignInDemo opens the Add AI card on OpenAI's sign-in at one step,
// for the shots (dev action "addaidemo:<step>"):
//
//	signin-ask      Codex is missing: Install and sign in
//	signin-install  Installing Codex 0:12
//	signin-browser  Sign in with your browser · Copy the link
//	signin-code     the CLI asks for a pasted code
//	signin-device   over SSH: the link and the device code
//	signin-nonode   Codex needs Node.js
//	signin-ready    OpenAI is ready with the CLI's models
//
// The states are fixed: nothing installs, runs, signs in or is saved.
func (m *Model) addAISignInDemo(step string) tea.Cmd {
	m.addAI = addai.New(addai.Options{KeyNote: secretPromptNote("")})
	m.addAI.SetSize(m.w, m.h)
	m.addAIOpen = true
	send := func(msgs ...tea.Msg) {
		for _, k := range msgs {
			m.addAI, _ = m.addAI.Update(k) // the request it sends is not run
		}
	}
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	send(tea.KeyMsg{Type: tea.KeyDown}, enter, enter) // OpenAI → Sign in with ChatGPT Plus / Pro
	if step == "signin-ask" {
		return nil
	}
	m.addAI, _ = m.addAI.Update(connectcard.ButtonMsg{ID: "signin"})
	progress := func(st clilogin.State) { m.addAI.Progress(m.addAI.Gen(), st) }
	progress(clilogin.State{Phase: clilogin.PhaseInstalling, Elapsed: 12 * time.Second, Line: "added 1 package in 11s"})
	switch step {
	case "signin-install":
	case "signin-browser":
		progress(clilogin.State{Phase: clilogin.PhaseWaitingForBrowser, URL: addAISignInDemoLink})
	case "signin-code":
		progress(clilogin.State{Phase: clilogin.PhaseWaitingForBrowser, URL: addAISignInDemoLink, NeedsCode: true})
	case "signin-device":
		progress(clilogin.State{Phase: clilogin.PhaseWaitingForBrowser, URL: "https://auth.openai.com/codex/device", Code: "DEMO-1234"})
	case "signin-nonode":
		progress(clilogin.State{Phase: clilogin.PhaseFailed, Err: &clilogin.Error{Kind: clilogin.FailNoNode, Msg: "Codex needs Node.js."}})
	case "signin-ready":
		progress(clilogin.State{Phase: clilogin.PhaseLoadingModels})
		var ids []string
		for _, mi := range ai.CLIModels(ai.CLICodex) {
			ids = append(ids, mi.ID)
		}
		m.addAI.Finish(m.addAI.Gen(), ids)
	default:
		m.addAIOpen = false
	}
	return nil
}
