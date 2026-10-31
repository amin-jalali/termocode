package app

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Switch backend… on the store screen (U19 SS-8) ─────────────────────
//
// The store is opened first (withSecretsUnlocked), the OS keyring is
// probed off the UI goroutine, then a card offers the other backends:
// the OS keyring (only when it answers; else the card says why), the
// encrypted file (a passphrase: a new one twice, or the one of an
// existing file) and the plain file (behind a second card that says it
// is not encrypted). secrets.Switch copies every name, checks it,
// records the backend in the index and, in Commit, the secret_store
// setting; only then are the old copies removed. On a failure the old
// store stays in use and nothing is deleted. Only the user reaches it:
// the store screen is never offered to the AI.

const (
	confirmKindSecretsSwitch      confirmKindEnum = 1803
	confirmKindSecretsSwitchPlain confirmKindEnum = 1804
)

// secretActionSwitch is the store screen's Switch backend… button.
const secretActionSwitch = "act:switch"

// secretsSwitchCheckMsg carries the keyring probe for the card.
type secretsSwitchCheckMsg struct {
	keyring bool
	why     string
}

// secretsSwitchDoneMsg ends a switch. It never carries a value or the
// passphrase.
type secretsSwitchDoneMsg struct {
	to       string
	from     string // the label of the old store
	err      error
	switched bool // the new store is in use (even if a removal failed)
}

// secretsBackendName names a backend id for the user.
func secretsBackendName(id string) string {
	switch id {
	case "age":
		return "encrypted file"
	case "keyring":
		return "OS keyring"
	case "plain":
		return "plain file"
	}
	return id
}

// secretsSwitchStart probes the keyring and then shows the card. The
// store is open (withSecretsUnlocked).
func (m *Model) secretsSwitchStart() tea.Cmd {
	if m.secretsS().busy {
		return nil
	}
	s, err := m.secretStore()
	if err != nil || s == nil {
		return m.secretsToast(toast.Errr, "Secret store not available", "The store could not be opened.")
	}
	if s.Backend() == "keyring" {
		return func() tea.Msg { return secretsSwitchCheckMsg{keyring: true} }
	}
	return func() tea.Msg {
		ok, why := secretsKeyringAvailable()
		return secretsSwitchCheckMsg{keyring: ok, why: why}
	}
}

// onSecretsSwitchCheck opens the card with the other backends.
func (m *Model) onSecretsSwitchCheck(msg secretsSwitchCheckMsg) tea.Cmd {
	s, err := m.secretStore()
	if err != nil || s == nil {
		return nil
	}
	cur := s.Backend()
	var b strings.Builder
	b.WriteString("Now: " + secretsBackendLabel(s) + ".\n")
	b.WriteString("Every secret is copied to the new place and checked;\n")
	b.WriteString("only then is the old copy removed. References stay references.\n\n")
	var buttons []confirm.Button
	if cur != "keyring" {
		if msg.keyring {
			buttons = append(buttons, confirm.Button{ID: "keyring", Title: "OS keyring", Style: confirm.StylePrimary})
		} else {
			b.WriteString("OS keyring: not available here (" + msg.why + ").\n")
		}
	}
	if cur != "age" {
		style := confirm.StyleDefault
		if len(buttons) == 0 {
			style = confirm.StylePrimary
		}
		buttons = append(buttons, confirm.Button{ID: "age", Title: "Encrypted file", Style: style})
	}
	if cur != "plain" {
		buttons = append(buttons, confirm.Button{ID: "plain", Title: "Plain file"})
	}
	buttons = append(buttons, confirm.Button{ID: "cancel", Title: "Cancel"})
	m.confirm = confirm.New("Switch backend", strings.TrimRight(b.String(), "\n"), buttons).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindSecretsSwitch
	return nil
}

// answerSecretsSwitch handles the card.
func (m *Model) answerSecretsSwitch(id string) tea.Cmd {
	st := m.secretsS()
	switch id {
	case "keyring":
		return m.secretsSwitchCmd("keyring", nil)
	case "age":
		st.switching, st.tries, st.after, st.migrating = true, 0, nil, false
		st.endSecretsChange()
		st.forgetFirst()
		st.creating = !secretsAgeFileExists()
		note := ""
		if !st.creating {
			note = "An encrypted file " + secrets.AgeFile + " already exists: type its passphrase, your secrets are added to it."
		}
		m.openSecretsPrompt(note)
		return nil
	case "plain":
		path, _ := ai.CredentialsPath()
		text := "The plain file is not encrypted (mode 0600 only):\n  " + path + "\n" +
			"Anyone who can read your home folder, or a backup of it,\n" +
			"can read every key in it. The current copy is removed after the check."
		m.confirm = confirm.New("Use the plain file?", text, []confirm.Button{
			{ID: "cancel", Title: "Cancel", Style: confirm.StylePrimary},
			{ID: "use-plain", Title: "Use plain file", Style: confirm.StyleDestructive},
		}).WithWideText()
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen, m.confirmKind = true, confirmKindSecretsSwitchPlain
		return nil
	}
	return m.openSecretsStore()
}

// answerSecretsSwitchPlain handles the warning card.
func (m *Model) answerSecretsSwitchPlain(id string) tea.Cmd {
	if id == "use-plain" {
		return m.secretsSwitchCmd("plain", nil)
	}
	return m.openSecretsStore()
}

// secretsSwitchCmd switches off the UI goroutine. pass (age) is zeroed
// after use.
func (m *Model) secretsSwitchCmd(to string, pass []byte) tea.Cmd {
	st := m.secretsS()
	from, err := m.secretStore()
	dir, err2 := ai.ConfigDir()
	path, err3 := ai.CredentialsPath()
	if err != nil || err2 != nil || err3 != nil || from == nil {
		clear(pass)
		st.switching = false
		return m.secretsToast(toast.Errr, "Backend not switched", "The secret store or the config folder is not available.")
	}
	st.busy = true
	label := secretsBackendLabel(from)
	o := secrets.SwitchOptions{
		Options: secrets.Options{Dir: dir, PlainPath: path, ScryptWorkFactor: secretScryptWork,
			Workspace: aiWorkspaceRoot(), Register: ai.RegisterSecret, Redact: ai.Redact,
			KeyringProbe: secretKeyringProbe, OnUse: secretsTouch},
		To: to, Pass: pass,
		Commit: func(s secrets.Store) error { return adoptSwitchedStore(path, to, s) },
	}
	return func() tea.Msg {
		s, err := secrets.Switch(from, o)
		clear(pass)
		return secretsSwitchDoneMsg{to: to, from: label, err: err, switched: s != nil}
	}
}

// adoptSwitchedStore saves secret_store and makes s the app's one store
// (Switch's Commit). Under the store lock, so no one opens the old
// store again in between.
func adoptSwitchedStore(path, to string, s secrets.Store) error {
	secretStoreConfig.Lock()
	defer secretStoreConfig.Unlock()
	if err := saveSecretStoreSetting(to); err != nil {
		return errors.New("could not save secret_store in config.json: " + err.Error())
	}
	secretStoreConfig.key = path + "\x00" + secretStoreSetting()
	secretStoreConfig.store = s
	secretStoreConfig.err = nil
	ai.SetSecretStoreForPath(path, s)
	return nil
}

// onSecretsSwitchDone reports the switch and shows the store screen.
func (m *Model) onSecretsSwitchDone(msg secretsSwitchDoneMsg) tea.Cmd {
	st := m.secretsS()
	st.busy = false
	where := secretsBackendName(msg.to)
	switch {
	case msg.switched:
		st.switching, st.tries, st.creating = false, 0, false
		m.secretsChanged()
		title, level, detail := "Secrets now in the "+where, toast.Info, "Copied and checked · the "+msg.from+" copy is removed · secret_store: "+msg.to
		fromPlain := msg.from == "plain file"
		if fromPlain {
			detail = "Copied and checked · the plain file is kept · secret_store: " + msg.to
		}
		if msg.err != nil {
			title, level, detail = "Secrets now in the "+where+" · old copy not removed", toast.Warn, secretsRedact(msg.err)
		}
		cmds := []tea.Cmd{m.secretsToast(level, title, detail), m.secretsArmLock(), m.openSecretsStore()}
		if fromPlain && msg.to != "plain" {
			m.openSecretsDeletePlainCard(where) // after the store screen: the card is on top
		}
		return tea.Batch(cmds...)
	case errors.Is(msg.err, secrets.ErrWrongPassphrase) && msg.to == "age":
		st.tries++
		if st.tries < secretsMaxTries {
			left := secretsTriesLeft(secretsMaxTries - st.tries)
			m.openSecretsPrompt("Wrong passphrase for the existing " + secrets.AgeFile + " (" + left + ").")
			return m.secretsToast(toast.Warn, "Wrong passphrase", left)
		}
	}
	st.switching, st.tries, st.creating = false, 0, false
	return m.secretsToast(toast.Errr, "Backend not switched", "The "+msg.from+" stays in use; nothing was removed. "+secretsRedact(msg.err))
}
