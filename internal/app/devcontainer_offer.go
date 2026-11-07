package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/devcontainer"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// RC-7: when a local folder with devcontainer.json opens, one quiet
// toast offers to reopen it in a dev container. A click opens a card:
// Not now (first, the default), Never for this folder, or Reopen in Dev
// Container, which goes on to the trust card. Nothing is made or run
// from the toast or this card.

const confirmKindDevContainerOffer confirmKindEnum = 5420

// devcontainerOfferMsg: the folder has devcontainer.json and no Never.
type devcontainerOfferMsg struct{ root string }

// devcontainerOfferClickMsg is a click on the offer toast.
type devcontainerOfferClickMsg struct{ root string }

// devcontainerOfferCmd checks the folder off the UI loop (two stats and
// one small state file). nil when the offer showed already this session
// or the workspace is a container.
func (m Model) devcontainerOfferCmd() tea.Cmd {
	if m.devcontainerOffered || m.workspace().Kind == workspace.KindContainer {
		return nil
	}
	root, err := m.rootDir()
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		if devcontainer.Find(root) == "" || devcontainer.OfferMuted(root) {
			return nil
		}
		return devcontainerOfferMsg{root: root}
	}
}

// onDevContainerOffer shows the offer toast once a session.
func (m *Model) onDevContainerOffer(msg devcontainerOfferMsg) tea.Cmd {
	if m.devcontainerOffered || m.workspace().Kind == workspace.KindContainer {
		return nil
	}
	m.devcontainerOffered = true
	var cmd tea.Cmd
	m.toast, cmd = m.toast.PushClick(toast.Info, "This project has a dev container",
		"Click to choose: Reopen in Dev Container, Not now or Never.", devcontainerOfferClickMsg{root: msg.root})
	return cmd
}

// openDevContainerOfferCard is the Not now / Never / Reopen card.
func (m *Model) openDevContainerOfferCard(root string) tea.Cmd {
	if m.workspace().Kind == workspace.KindContainer {
		return nil
	}
	m.dcOfferRoot = root
	m.confirm = confirm.New("Dev Container",
		"This project has a devcontainer.json. Reopen it in a dev container?\n\n"+
			"termocode shows every command it will run before it makes anything. "+
			"Workspace: Reopen in Dev Container... does the same any time.",
		[]confirm.Button{
			{ID: "cancel", Title: "Not now"},
			{ID: "never", Title: "Never for this project"},
			{ID: "open", Title: "Reopen in Dev Container"},
		})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindDevContainerOffer
	return nil
}

// onDevContainerOfferConfirm handles the offer card.
func (m *Model) onDevContainerOfferConfirm(id string) tea.Cmd {
	root := m.dcOfferRoot
	m.dcOfferRoot = ""
	switch id {
	case "open":
		return m.openDevContainerReopen()
	case "never":
		if root == "" {
			return nil
		}
		if err := devcontainer.MuteOffer(root); err != nil {
			return m.pushToastDetail(toast.Errr, "Cannot save Never", err.Error())
		}
		return m.pushToastDetail(toast.Info, "termocode will not ask again for this project",
			"Workspace: Reopen in Dev Container... still works.")
	}
	return nil
}
