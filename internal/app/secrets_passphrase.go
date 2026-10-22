package app

import (
	"crypto/subtle"
	"errors"
	"strconv"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── AI: Change Secrets Passphrase… (U19 SS-8) ──────────────────────────
//
// Three masked prompts in the passphrase prompt (promptKindSecretsPass,
// routed here while secretsState.chg is set): the current passphrase,
// always, even when the store is open (checked off the UI goroutine
// before anything else is asked), then the new one twice (8 at least, a
// warning under 12, not the current one). secrets.ChangePassphrase
// re-encrypts in a temp file, checks it with the new passphrase and only
// then renames it over the old file; any failure keeps the old file.
// The typed passphrases live in chg as byte slices until the change
// runs, which zeroes them; Esc zeroes them too. For the encrypted file
// only (the keyring and the plain file have no passphrase), never for
// the AI (palette id aiNever).

// secretsChange is the change flow's state.
type secretsChange struct {
	stage int    // 0 current, 1 new, 2 new again
	old   []byte // the checked current passphrase
	first []byte // the first entry of the new one
	tries int    // wrong current passphrases
}

// forget zeroes what was typed.
func (c *secretsChange) forget() {
	if c == nil {
		return
	}
	clear(c.old)
	clear(c.first)
	c.old, c.first = nil, nil
}

// secretsChangeCheckMsg ends the check of the current passphrase.
type secretsChangeCheckMsg struct{ err error }

// secretsChangeDoneMsg ends the re-encryption.
type secretsChangeDoneMsg struct{ err error }

// endSecretsChange drops the flow and zeroes what it held.
func (st *secretsState) endSecretsChange() {
	st.chg.forget()
	st.chg = nil
}

// openSecretsChangePassphrase is AI: Change Secrets Passphrase… and the
// store screen's Change passphrase….
func (m *Model) openSecretsChangePassphrase() tea.Cmd {
	s, err := m.secretStore()
	if err != nil {
		return m.secretsToast(toast.Errr, "Secret store not available", secretsRedact(err))
	}
	if s == nil || s.Backend() != "age" {
		label := "this store"
		if s != nil {
			label = "the " + secretsBackendLabel(s)
		}
		return m.secretsToast(toast.Info, "No passphrase to change", "Only the encrypted file has a passphrase; "+label+" has none.")
	}
	if !secretsAgeFileExists() {
		return m.secretsToast(toast.Info, "No encrypted file yet", "It is made, with its passphrase, when the first secret is saved.")
	}
	st := m.secretsS()
	if st.busy {
		return nil
	}
	st.forgetFirst()
	st.after, st.migrating = nil, false
	st.endSecretsChange()
	st.chg = &secretsChange{}
	m.openSecretsChangePrompt("")
	return nil
}

// openSecretsChangePrompt opens the masked prompt of the current stage.
func (m *Model) openSecretsChangePrompt(note string) {
	c := m.secretsS().chg
	label, text := "Current passphrase:", "Step 1 of 3. The encrypted file stays as it is until the new one is checked."
	switch c.stage {
	case 1:
		label = "New passphrase:"
		text = "Step 2 of 3. At least " + strconv.Itoa(secrets.MinPassphrase) + " characters (" +
			strconv.Itoa(secrets.GoodPassphrase) + " or more is better). It can not be recovered."
	case 2:
		label, text = "Type it again:", "Step 3 of 3. Type the new passphrase again."
	}
	if note != "" {
		text = note + " " + text
	}
	m.prompt = prompt.New("Change secrets passphrase", label, "")
	m.prompt.SetMasked(true)
	m.prompt.SetNote(text)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindSecretsPass
}

// handleSecretsChangeSubmit is Enter in one of the three prompts. The
// prompt was already cleared by the caller.
func (m *Model) handleSecretsChangeSubmit(value string) tea.Cmd {
	st := m.secretsS()
	c := st.chg
	switch c.stage {
	case 0:
		if value == "" {
			m.openSecretsChangePrompt("")
			return nil
		}
		s, err := m.secretStore()
		if err != nil || s == nil {
			st.endSecretsChange()
			return m.secretsToast(toast.Errr, "Secret store not available", "The store could not be opened.")
		}
		c.forget()
		c.old = []byte(value)
		pass := append([]byte(nil), c.old...) // the command's own copy
		st.busy = true
		return func() tea.Msg {
			err := secrets.CheckCurrentPassphrase(s, pass)
			clear(pass)
			return secretsChangeCheckMsg{err: err}
		}
	case 1:
		if utf8.RuneCountInString(value) < secrets.MinPassphrase {
			m.openSecretsChangePrompt("Too short: at least " + strconv.Itoa(secrets.MinPassphrase) + " characters.")
			return nil
		}
		next := []byte(value)
		if subtle.ConstantTimeCompare(next, c.old) == 1 {
			clear(next)
			m.openSecretsChangePrompt("That is the current passphrase. Choose a new one.")
			return nil
		}
		clear(c.first)
		c.first, c.stage = next, 2
		note := ""
		if weak, _ := secrets.CheckPassphrase(value); weak {
			note = "Weak: under " + strconv.Itoa(secrets.GoodPassphrase) + " characters is easy to guess. Type it again to use it anyway, or Esc."
		}
		m.openSecretsChangePrompt(note)
		return nil
	}
	again := []byte(value)
	same := subtle.ConstantTimeCompare(c.first, again) == 1
	clear(c.first)
	c.first = nil
	if !same {
		clear(again)
		c.stage = 1
		m.openSecretsChangePrompt("The two passphrases differ. Choose one again.")
		return nil
	}
	s, err := m.secretStore()
	if err != nil || s == nil {
		clear(again)
		st.endSecretsChange()
		return m.secretsToast(toast.Errr, "Secret store not available", "The store could not be opened.")
	}
	old := c.old
	c.old = nil // the command owns and zeroes it now
	st.busy = true
	return func() tea.Msg {
		err := secrets.ChangePassphrase(s, old, again)
		clear(old)
		clear(again)
		return secretsChangeDoneMsg{err: err}
	}
}

// onSecretsChangeCheck goes on to the new passphrase, or asks the
// current one again (three tries).
func (m *Model) onSecretsChangeCheck(msg secretsChangeCheckMsg) tea.Cmd {
	st := m.secretsS()
	st.busy = false
	c := st.chg
	if c == nil {
		return nil // Esc while it ran: forgotten already
	}
	switch {
	case msg.err == nil:
		c.stage = 1
		m.openSecretsChangePrompt("")
		return nil
	case errors.Is(msg.err, secrets.ErrWrongPassphrase):
		c.forget()
		c.tries++
		if c.tries >= secretsMaxTries {
			st.endSecretsChange()
			return m.secretsToast(toast.Warn, "Passphrase not changed", "Wrong current passphrase three times. Nothing changed.")
		}
		left := secretsTriesLeft(secretsMaxTries - c.tries)
		m.openSecretsChangePrompt("Wrong passphrase (" + left + ").")
		return m.secretsToast(toast.Warn, "Wrong passphrase", left)
	}
	st.endSecretsChange()
	return m.secretsToast(toast.Errr, "Passphrase not changed", secretsRedact(msg.err))
}

// onSecretsChangeDone reports the change.
func (m *Model) onSecretsChangeDone(msg secretsChangeDoneMsg) tea.Cmd {
	st := m.secretsS()
	st.busy = false
	st.endSecretsChange()
	if msg.err != nil {
		return m.secretsToast(toast.Errr, "Passphrase not changed", "The encrypted file is unchanged. "+secretsRedact(msg.err))
	}
	st.tries = 0
	m.secretsChanged()
	return tea.Batch(m.secretsToast(toast.Info, "Secrets passphrase changed", "The encrypted file now opens with the new passphrase."), m.secretsArmLock())
}
