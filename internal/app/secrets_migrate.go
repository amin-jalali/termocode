package app

import (
	"errors"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/cfgfile"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Moving the old plain credentials.json to a safer store (U19 SS-4) ──
//
// At start (secretsMigrateStartCmd, off the UI goroutine) the app looks
// at the store it opened:
//
//   - plain, kept only because ai/credentials.json exists (SS-3), with
//     secret_store unset or auto, no marker and secrets in the file:
//     the card "Move your secrets to a safer store?" asks once per start.
//     Esc is "later"; after secretsMigrateMaxLater laters the card stops
//     and the Welcome page shows one note line instead. "Keep plain"
//     sets secret_store: plain and the card never returns.
//   - keyring or age while the plain file still exists: a move that was
//     cut off, or one where the user kept the plain copy. Once the store
//     is open (the keyring at once, the encrypted file after the user's
//     own unlock) a card asks: Keep for now (first, focused) / Delete the
//     plain file. The file is never deleted without that answer; the
//     values are checked first. AI: Move Secrets… asks again any time.
//
// The move itself is secrets.Migrate, run as a tea.Cmd (scrypt, writes,
// fsync): write, read back, compare. The old file stays; the delete (zero,
// sync, remove, credentials.json.migrated) is secrets.Finish after the card. The card counts kinds,
// never values; toasts and errors name secrets, never values. AI: Move
// Secrets to a Safer Store… opens the card any time the plain backend
// is in use. None of it is for the AI (palette id aiNever).

const (
	confirmKindSecretsMigrate     confirmKindEnum = 1802
	confirmKindSecretsDeletePlain confirmKindEnum = 1806
)

// secretsMigrateMaxLater "later" answers stop the card at start.
const secretsMigrateMaxLater = 3

// welcomeSecretsNote shows the Welcome page's line about the move after
// the card was put off secretsMigrateMaxLater times.
var welcomeSecretsNote bool

const welcomeSecretsNoteText = "F1 → AI: Move Secrets to a Safer Store…"

// secretsMigratePlan is what the check found.
type secretsMigratePlan struct {
	ask        bool // show the card
	note       bool // put off too often: the Welcome note
	finish     bool // a plain copy is left: ask to delete it once the store is open
	plainLeft  bool // the plain file still exists beside a safer store
	counts     secrets.Counts
	keyring    bool   // the OS keyring answered
	keyringWhy string // why not ("no D-Bus session")
	backend    string // the store in use when it is not plain
	err        error  // the plain file could not be read
}

// secretsMigrateCheckMsg carries the check's answer. palette: the user
// asked (AI: Move Secrets…), so the start rules do not apply.
type secretsMigrateCheckMsg struct {
	plan    secretsMigratePlan
	palette bool
}

// secretsMigrateDoneMsg ends a move or a finish. It never carries a
// value or the passphrase.
type secretsMigrateDoneMsg struct {
	to       string // "age" | "keyring"; "" for a finish
	err      error
	moved    bool // the new store is in use (even if the delete failed)
	finished bool // a finish removed the plain file
}

// secretsMigrateStartCmd is the start-up check (Init).
func (m *Model) secretsMigrateStartCmd() tea.Cmd {
	return func() tea.Msg { return secretsMigrateCheckMsg{plan: secretsMigrateCheck(false)} }
}

// secretsMigrateCheck decides what to do; it reads files and may probe
// the keyring, so it runs off the UI goroutine.
func secretsMigrateCheck(palette bool) secretsMigratePlan {
	var p secretsMigratePlan
	s, err := configureSecretStore()
	if err != nil || s == nil {
		return p
	}
	path, err := ai.CredentialsPath()
	if err != nil {
		return p
	}
	if s.Backend() != "plain" {
		p.backend = secretsBackendLabel(s)
		if _, err := os.Stat(path); err == nil && (s.Backend() == "age" || s.Backend() == "keyring") {
			p.plainLeft, p.finish = true, !palette
		}
		return p
	}
	pl, err := secrets.FromPlain(path)
	if err != nil {
		p.err = err
		return p
	}
	p.counts = pl.Counts()
	if p.counts.Secrets() == 0 {
		return p
	}
	if !palette {
		dir, err := ai.ConfigDir()
		if err != nil || secretStoreSetting() != "auto" || secrets.Migrated(path) {
			return p
		}
		if secrets.LaterCount(dir) >= secretsMigrateMaxLater {
			p.note = true
			return p
		}
	}
	p.keyring, p.keyringWhy = secretsKeyringAvailable()
	p.ask = true
	return p
}

// secretsKeyringAvailable probes the OS keyring (the test seam first)
// and says why it can not be used.
func secretsKeyringAvailable() (bool, string) {
	if secretKeyringProbe != nil {
		if secretKeyringProbe() {
			return true, ""
		}
	} else {
		if why := secrets.KeyringSkipReason(nil, false); why != "" {
			return false, why
		}
		if secrets.ProbeKeyring(nil, false) {
			return true, ""
		}
	}
	if why := secrets.KeyringSkipReason(nil, false); why != "" {
		return false, why
	}
	return false, "it did not answer"
}

// onSecretsMigrateCheck acts on the check: the card, the note or a
// finish.
func (m *Model) onSecretsMigrateCheck(msg secretsMigrateCheckMsg) tea.Cmd {
	p := msg.plan
	if !msg.palette && welcomeSecretsNote != p.note {
		welcomeSecretsNote = p.note
		invalidateWelcome()
	}
	switch {
	case p.finish:
		return m.secretsFinishMove()
	case msg.palette && p.err != nil:
		return m.secretsToast(toast.Errr, "Can not read ai/credentials.json", secretsRedact(p.err))
	case msg.palette && p.backend != "" && p.plainLeft:
		return m.secretsFinishMove()
	case msg.palette && p.backend != "":
		return m.secretsToast(toast.Info, "Secrets are already in the "+p.backend, "Nothing to move · F1 → AI: Secret Store…")
	case msg.palette && !p.ask:
		return m.secretsToast(toast.Info, "Nothing to move", "ai/credentials.json holds no secrets.")
	case !p.ask:
		return nil
	}
	if !msg.palette && (m.confirmOpen || m.promptOpen || m.pickerOpen) {
		return nil // something else is on screen: ask on the next start
	}
	m.openSecretsMigrateCard(p, msg.palette)
	return nil
}

// openSecretsMigrateCard shows the card (plan F1): what was found, by
// kind, and the recommended store.
func (m *Model) openSecretsMigrateCard(p secretsMigratePlan, palette bool) {
	var b strings.Builder
	b.WriteString("Found in ai/credentials.json (plain text, mode 0600):\n")
	b.WriteString("  " + p.counts.String() + "\n\n")
	var buttons []confirm.Button
	if p.keyring {
		b.WriteString("Recommended here: OS keyring. No passphrase to type.\n\n")
		buttons = append(buttons, confirm.Button{ID: "keyring", Title: "OS keyring", Style: confirm.StylePrimary},
			confirm.Button{ID: "age", Title: "Encrypted file"})
	} else {
		b.WriteString("Recommended here: encrypted file (no OS keyring: " + p.keyringWhy + ").\n")
		b.WriteString("You choose a passphrase once; this session keeps it.\n\n")
		buttons = append(buttons, confirm.Button{ID: "age", Title: "Encrypted file", Style: confirm.StylePrimary})
	}
	b.WriteString("The old file stays until you choose to delete it. Esc: ask later.")
	buttons = append(buttons, confirm.Button{ID: "plain", Title: "Keep plain"})
	m.secretsS().migrateAsked = !palette
	m.confirm = confirm.New("✦ Move your secrets to a safer store?", b.String(), buttons).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindSecretsMigrate
}

// answerSecretsMigrate handles a button of the card.
func (m *Model) answerSecretsMigrate(id string) tea.Cmd {
	st := m.secretsS()
	st.migrateAsked = false
	switch id {
	case "keyring":
		return m.secretsMigrateCmd("keyring", nil)
	case "age":
		st.migrating, st.tries, st.after = true, 0, nil
		st.forgetFirst()
		st.creating = !secretsAgeFileExists()
		m.openSecretsPrompt("")
		return nil
	case "plain":
		if err := saveSecretStoreSetting("plain"); err != nil {
			return m.secretsToast(toast.Errr, "Could not save secret_store", secretsRedact(err))
		}
		if welcomeSecretsNote {
			welcomeSecretsNote = false
			invalidateWelcome()
		}
		return m.secretsToast(toast.Info, "Secrets stay in the plain file", "secret_store: plain · F1 → AI: Move Secrets to a Safer Store… any time")
	}
	return nil
}

// secretsMigrateClosed is Esc on the card: "later". Only the card shown
// at start counts.
func (m *Model) secretsMigrateClosed() tea.Cmd {
	st := m.secretsS()
	if !st.migrateAsked {
		return nil
	}
	st.migrateAsked = false
	dir, err := ai.ConfigDir()
	if err != nil {
		return nil
	}
	n, err := secrets.AddLater(dir)
	if err != nil {
		return m.secretsToast(toast.Warn, "Could not save the answer", secretsRedact(err))
	}
	if n >= secretsMigrateMaxLater {
		welcomeSecretsNote = true
		invalidateWelcome()
	}
	return nil
}

// secretsMigratePalette is AI: Move Secrets to a Safer Store….
func (m *Model) secretsMigratePalette() tea.Cmd {
	return func() tea.Msg { return secretsMigrateCheckMsg{plan: secretsMigrateCheck(true), palette: true} }
}

// secretsPassEntered gets the passphrase once it is complete (typed
// twice for a new file): an unlock, or the move to the encrypted file.
func (m *Model) secretsPassEntered(pass []byte) tea.Cmd {
	if m.secretsS().migrating {
		return m.secretsMigrateCmd("age", pass)
	}
	if m.secretsS().switching { // secrets_switch.go
		return m.secretsSwitchCmd("age", pass)
	}
	return m.secretsUnlockCmd(pass)
}

// secretsMigrateCmd moves the secrets off the UI goroutine. pass (age)
// is zeroed after use.
func (m *Model) secretsMigrateCmd(to string, pass []byte) tea.Cmd {
	dir, err := ai.ConfigDir()
	path, err2 := ai.CredentialsPath()
	if err != nil || err2 != nil {
		clear(pass)
		return m.secretsToast(toast.Errr, "Secrets not moved", "The config folder is not known.")
	}
	m.secretsS().busy = true
	o := secrets.MigrateOptions{
		Options: secrets.Options{Dir: dir, PlainPath: path, ScryptWorkFactor: secretScryptWork,
			Workspace: aiWorkspaceRoot(), Register: ai.RegisterSecret, Redact: ai.Redact, KeyringProbe: secretKeyringProbe,
			OnUse: secretsTouch},
		To: to, Pass: pass,
		Commit: func(s secrets.Store) { adoptSecretStore(path, s) },
	}
	return func() tea.Msg {
		s, err := secrets.Migrate(o)
		clear(pass)
		return secretsMigrateDoneMsg{to: to, err: err, moved: s != nil}
	}
}

// adoptSecretStore makes s the app's one store after a move: the
// unlocked store stays open (no second passphrase), and a "plain"
// setting from an earlier Keep plain is cleared so auto finds the
// recorded backend on the next start.
func adoptSecretStore(path string, s secrets.Store) {
	if strings.EqualFold(strings.TrimSpace(loadSettings().SecretStore), "plain") {
		_ = saveSecretStoreSetting("")
	}
	secretStoreConfig.Lock()
	defer secretStoreConfig.Unlock()
	secretStoreConfig.key = path + "\x00" + secretStoreSetting()
	secretStoreConfig.store = s
	secretStoreConfig.err = nil
	ai.SetSecretStoreForPath(path, s)
}

// saveSecretStoreSetting sets secret_store in config.json ("" removes
// it), keeping every other key.
func saveSecretStoreSetting(v string) error {
	path, err := settingsConfigPath()
	if err != nil {
		return err
	}
	return cfgfile.Update(path, func(merged map[string]any) error {
		if v == "" {
			delete(merged, "secret_store")
		} else {
			merged["secret_store"] = v
		}
		return nil
	})
}

// secretsFinishMove asks about the plain file that is left beside an open
// safer store; a locked encrypted file waits for the user's unlock
// (onSecretsUnlockDone). It never deletes by itself.
func (m *Model) secretsFinishMove() tea.Cmd {
	s, err := m.secretStore()
	if err != nil || s == nil {
		return nil
	}
	if s.Locked() {
		m.secretsS().finishPending = true
		return nil
	}
	m.secretsS().finishPending = false
	if m.confirmOpen || m.promptOpen || m.pickerOpen {
		return nil // something else is on screen: ask on the next start
	}
	m.openSecretsDeletePlainCard(secretsBackendLabel(s))
	return nil
}

// openSecretsDeletePlainCard asks before the plain credentials file is
// deleted. Keep for now is first and focused; Enter and Esc keep.
func (m *Model) openSecretsDeletePlainCard(where string) {
	m.confirm = confirm.Risky("Delete the old plain file?",
		"Your secrets are in the "+where+".\n"+
			"ai/credentials.json still holds them as plain text, readable on disk.\n"+
			"termocode checks the copy first, then deletes the file.",
		confirm.Button{ID: "keep", Title: "Keep for now"},
		confirm.Button{ID: "delete", Title: "Delete the plain file", Style: confirm.StyleDestructive}).WithWideText()
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindSecretsDeletePlain
}

// showWaitingSecretsDeletePlain opens the Delete the old plain file? card
// a finished move left waiting, once no other dialog is open: opening it
// over another card (Restore unsaved changes? at start) would throw that
// question away. Update calls it after every message.
func (m *Model) showWaitingSecretsDeletePlain() {
	if m.secrets == nil || m.secrets.deletePlainWaiting == "" || m.confirmOpen || m.promptOpen || m.pickerOpen {
		return
	}
	where := m.secrets.deletePlainWaiting
	m.secrets.deletePlainWaiting = ""
	m.openSecretsDeletePlainCard(where)
}

// answerSecretsDeletePlain handles the card.
func (m *Model) answerSecretsDeletePlain(id string) tea.Cmd {
	if id != "delete" {
		return m.secretsToast(toast.Info, "Plain file kept", "It is still readable on disk · F1 → AI: Move Secrets to a Safer Store… to delete it")
	}
	s, err := m.secretStore()
	path, err2 := ai.CredentialsPath()
	if err != nil || err2 != nil || s == nil {
		return nil
	}
	return func() tea.Msg {
		done, err := secrets.Finish(s, path)
		return secretsMigrateDoneMsg{err: err, finished: done}
	}
}

// onSecretsMigrateDone reports the move: the toast, the AI reload, or
// the error (the plain file stays then).
func (m *Model) onSecretsMigrateDone(msg secretsMigrateDoneMsg) tea.Cmd {
	st := m.secretsS()
	if msg.to != "" {
		st.busy = false
	}
	if msg.to == "" { // a finish
		switch {
		case msg.finished:
			m.secretsChanged()
			return m.secretsToast(toast.Info, "Secrets moved · credentials.json removed", "The earlier move is finished.")
		case msg.err != nil:
			return m.secretsToast(toast.Warn, "ai/credentials.json kept", "It differs from the secret store: "+secretsRedact(msg.err))
		}
		return nil
	}
	where := "Now in the encrypted file " + secrets.AgeFile + "."
	if msg.to == "keyring" {
		where = "Now in the OS keyring."
	}
	switch {
	case msg.err == nil:
		st.migrating, st.tries, st.creating = false, 0, false
		if welcomeSecretsNote {
			welcomeSecretsNote = false
			invalidateWelcome()
		}
		m.secretsChanged()
		m.secretsS().deletePlainWaiting = strings.TrimPrefix(strings.TrimSuffix(where, "."), "Now in the ")
		m.showWaitingSecretsDeletePlain()
		return tea.Batch(m.secretsToast(toast.Info, "Secrets moved · plain file kept", where), m.secretsArmLock())
	case msg.moved:
		st.migrating, st.tries, st.creating = false, 0, false
		m.secretsChanged()
		return tea.Batch(m.secretsToast(toast.Warn, "Secrets moved · credentials.json not removed", where+" "+secretsRedact(msg.err)), m.secretsArmLock())
	case errors.Is(msg.err, secrets.ErrWrongPassphrase) && msg.to == "age":
		st.tries++
		if st.tries < secretsMaxTries {
			left := secretsTriesLeft(secretsMaxTries - st.tries)
			m.openSecretsPrompt("Wrong passphrase for the existing " + secrets.AgeFile + " (" + left + ").")
			return m.secretsToast(toast.Warn, "Wrong passphrase", left)
		}
	}
	st.migrating, st.tries, st.creating = false, 0, false
	return m.secretsToast(toast.Errr, "Secrets not moved", "ai/credentials.json is unchanged. "+secretsRedact(msg.err))
}
