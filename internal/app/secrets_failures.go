package app

import (
	"path/filepath"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Secret store failure cases (U19 SS-8, plan §5) ─────────────────────
//
//   - A damaged secrets.age: the unlock error names the file and a card
//     offers "Start a new encrypted file". The old file is renamed to
//     secrets.age.broken-<YYYYMMDD-HHMMSS> (never deleted), the values
//     it held leave the index, and the new-passphrase prompt opens.
//   - The OS keyring answered when it was chosen but not now (an SSH
//     login to the same machine): a failed Get sets a flag from any
//     goroutine; the next Update shows one toast, once per process.

const confirmKindSecretsBroken confirmKindEnum = 1805

// keyringNotice is 0 (nothing), 1 (a failure to show) or 2 (shown).
var keyringNotice atomic.Int32

func init() {
	secrets.SetKeyringFailNotifier(func() { keyringNotice.CompareAndSwap(0, 1) })
}

// resetKeyringNotice forgets the shown toast (tests).
func resetKeyringNotice() { keyringNotice.Store(0) }

// drainKeyringNotice shows the keyring toast once; one atomic load per
// Update otherwise.
func (m *Model) drainKeyringNotice() tea.Cmd {
	if keyringNotice.Load() != 1 || !keyringNotice.CompareAndSwap(1, 2) {
		return nil
	}
	return m.secretsToast(toast.Warn, "Keyring not reachable here", "F1 → AI: Secret Store…")
}

// openSecretsBrokenCard is the card for a damaged secrets.age.
func (m *Model) openSecretsBrokenCard(err error) {
	dir, _ := ai.ConfigDir()
	path := filepath.Join(dir, secrets.AgeFile)
	text := "This file can not be read (damaged, or not an encrypted file):\n  " + path + "\n" +
		secretsRedact(err) + "\n\n" +
		"Start a new encrypted file: the old one is kept as\n  " + secrets.AgeFile + secrets.BrokenSuffix + "<date>\n" +
		"(never deleted) and you choose a new passphrase. The values in it\n" +
		"must be set again; references stay."
	m.confirm = confirm.New("Secret file damaged", text, []confirm.Button{
		{ID: "new", Title: "Start a new encrypted file", Style: confirm.StylePrimary},
		{ID: "cancel", Title: "Cancel"},
	}).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindSecretsBroken
}

// answerSecretsBroken handles the card: "new" keeps the old file aside
// and asks for a new passphrase.
func (m *Model) answerSecretsBroken(id string) tea.Cmd {
	if id != "new" {
		return nil
	}
	dir, err := ai.ConfigDir()
	if err != nil {
		return m.secretsToast(toast.Errr, "Secret store not available", secretsRedact(err))
	}
	kept, err := secrets.SetAsideAgeFile(dir, clock.Now())
	if err != nil {
		return m.secretsToast(toast.Errr, "Could not keep the damaged file aside", secretsRedact(err))
	}
	m.secretsChanged()
	return tea.Batch(
		m.secretsToast(toast.Info, "Damaged file kept as "+filepath.Base(kept), "Choose a passphrase for the new encrypted file."),
		m.openSecretsUnlock(nil),
	)
}
