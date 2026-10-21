package app

import (
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── The store screen: AI: Secret Store… (U19 SS-3b) ─────────────────────
//
// One picker card: a header (backend, where it lives, locked or open),
// one row per saved name with "value" or its reference text, and the
// buttons "Lock now" / "Unlock…". Enter on a name asks to forget it.
// The screen reads the index (names, kinds, reference texts) and never
// calls Get, so no secret value can reach the screen. Changing the
// passphrase and switching the backend come with SS-8.

const (
	pickerKindSecretStore   pickerKindEnum  = 1800
	confirmKindSecretForget confirmKindEnum = 1800
)

const (
	secretRowPrefix  = "name:"
	secretActionLock = "act:lock"
	secretActionOpen = "act:unlock"
	secretStoreWidth = 80
)

// secretsStoreHeader is the lines above the list: where the secrets are
// and whether they can be read now.
func secretsStoreHeader(s secrets.Store, dir string, inner int) []string {
	// where keeps the end of a long path (the file name) and cuts its front.
	where := func(label, path string) string {
		room := inner - ansi.StringWidth(label) - 1
		if room < 8 {
			room = 8
		}
		if ansi.StringWidth(path) > room {
			path = "…" + ansi.TruncateLeft(path, ansi.StringWidth(path)-room+1, "")
		}
		return label + path
	}
	var lines []string
	switch s.Backend() {
	case "age":
		lines = append(lines, where("Backend: encrypted file · ", filepath.Join(dir, secrets.AgeFile)))
		if s.Locked() {
			lines = append(lines, "State: locked · Unlock… asks for the passphrase")
		} else {
			lines = append(lines, "State: unlocked · kept until you lock it or quit")
		}
	case "keyring":
		lines = append(lines, "Backend: OS keyring", "State: open · the keyring guards the values")
	case "plain":
		path, _ := ai.CredentialsPath()
		lines = append(lines, where("Backend: plain file · ", path), "Plain file: the values are not encrypted.")
	default:
		lines = append(lines, "Backend: "+secretsBackendLabel(s), "State: not available")
	}
	return lines
}

// openSecretsStore opens the store screen.
func (m *Model) openSecretsStore() tea.Cmd {
	s, err := m.secretStore()
	if err != nil {
		return m.secretsToast(toast.Errr, "Secret store not available", secretsRedact(err))
	}
	dir, _ := ai.ConfigDir()
	entries := secrets.Entries(dir)
	names := s.Names()
	items := make([]picker.Item, 0, len(names)+1)
	for _, n := range names {
		hint := "saved"
		if e, ok := entries[n]; ok {
			switch {
			case e.Kind == secrets.KindRef && e.Ref != "":
				hint = ai.Redact(sanitizeOutputLine(e.Ref))
			case e.Kind == secrets.KindValue:
				hint = "value"
			case e.Kind == secrets.KindPublic:
				hint = "not secret"
			}
		}
		items = append(items, picker.Item{ID: secretRowPrefix + n, Title: sanitizeOutputLine(n), Hint: hint})
	}
	width := m.w - 4
	if width > secretStoreWidth {
		width = secretStoreWidth
	}
	inner := width - 6
	if inner < 20 {
		inner = 20
	}
	note := secretsStoreHeader(s, dir, inner)
	if len(names) == 0 {
		note = append(note, "No secrets saved yet. Setting an AI key or connecting a",
			"database with a password saves one here.")
	} else {
		note = append(note, "Enter on a name forgets it.")
	}
	if s.Backend() == "age" {
		if s.Locked() {
			items = append(items, picker.Item{ID: secretActionOpen, Title: "Unlock…", Hint: "ask for the passphrase"})
		} else {
			items = append(items, picker.Item{ID: secretActionLock, Title: "Lock now", Hint: "forget the passphrase"})
		}
	}
	for i, l := range note {
		note[i] = ansi.Truncate(l, inner, "…")
	}
	m.picker = picker.NewItems(" Secret Store ", items)
	m.picker.SetWidth(secretStoreWidth)
	m.picker.SetNote(note)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen, m.pickerKind = true, pickerKindSecretStore
	return nil
}

// onSecretsStoreSelected is Enter on a row of the store screen.
func (m *Model) onSecretsStoreSelected(id string) tea.Cmd {
	switch {
	case id == secretActionLock:
		return tea.Batch(m.secretsLock(), m.openSecretsStore())
	case id == secretActionOpen:
		return m.openSecretsUnlock(func(m *Model) tea.Cmd { return m.openSecretsStore() })
	case strings.HasPrefix(id, secretRowPrefix):
		name := strings.TrimPrefix(id, secretRowPrefix)
		return m.withSecretsUnlocked(func(m *Model) tea.Cmd { return m.askSecretForget(name) })
	}
	return nil
}

// askSecretForget is the confirm card before a secret is deleted.
func (m *Model) askSecretForget(name string) tea.Cmd {
	s, err := m.secretStore()
	if err != nil {
		return m.secretsToast(toast.Errr, "Secret store not available", secretsRedact(err))
	}
	m.secretsS().forgetName = name
	m.confirm = confirm.New("Forget "+sanitizeOutputLine(name)+"?",
		"Removes it from the "+secretsBackendLabel(s)+". Whatever uses it will ask for it again. This can not be undone.",
		[]confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "forget", Title: "Forget", Style: confirm.StyleDestructive}})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindSecretForget
	return nil
}

// answerSecretForget handles the card: Forget deletes, anything else
// goes back to the screen.
func (m *Model) answerSecretForget(id string) tea.Cmd {
	st := m.secretsS()
	name := st.forgetName
	st.forgetName = ""
	if id != "forget" || name == "" {
		return m.openSecretsStore()
	}
	s, err := m.secretStore()
	if err == nil {
		err = s.Delete(name)
	}
	if err != nil {
		return tea.Batch(m.secretsToast(toast.Errr, "Could not forget the secret", secretsRedact(err)), m.openSecretsStore())
	}
	m.secretsChanged()
	return tea.Batch(m.secretsToast(toast.Info, "Forgot "+sanitizeOutputLine(name), ""), m.openSecretsStore())
}
