package app

import (
	"crypto/subtle"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Secret store: one store per UI process, unlock and lock (U19 SS-3) ──
//
// The app opens ONE secrets.Store (configureSecretStore) and hands it to
// internal/ai (LoadCredentials / SaveCredentials / LookupMCPSecret), so
// the AI, MCP, HTTP {{$secret}} and the Databases view read the same
// store. The secret_store setting picks the backend (auto, keyring, age,
// plain); auto is the OS keyring when one answers, else the age file,
// never plain. A user who already has a plain ai/credentials.json keeps
// it, with no prompt, until the migration card (SS-4) moves it.
//
// The age file is unlocked with a passphrase asked once per UI process
// (ADR 0007: the UI process is the persistent session): on the first
// need (an AI entry point, or saving a secret) or with AI: Unlock
// Secrets. The passphrase lives in the prompt's value until Enter, then
// only in the age backend's byte slice (zeroed by AI: Lock Secrets);
// the copies this file holds are zeroed after use. It is never logged,
// registered, toasted or put in argv. scrypt (~1 s) runs off the UI
// goroutine (secretsUnlockCmd).
//
// None of this is offered to the AI: both palette ids are aiNever and
// settings_set refuses secret_store and secret_lock_minutes.

const promptKindSecretsPass promptKindEnum = 1800

// secretsMaxTries wrong passphrases end an unlock round; the store stays
// locked until AI: Unlock Secrets.
const secretsMaxTries = 3

// secretStoreConfig is the app's one secret store. It is package level
// because goroutines (MCP expander, DB sessions, OAuth refresh) reach it
// without the Model; the Model reaches it with m.secretStore().
var secretStoreConfig struct {
	sync.Mutex
	key   string // credentials path + backend setting it was opened for
	store secrets.Store
	err   error // Open failed: forced keyring without one, bad setting
}

// Test seams. secretStoreSettingOverride wins over the secret_store
// setting (this package's TestMain sets "plain": its tests were written
// for the plain file and share one config dir). secretKeyringProbe
// replaces the OS keyring probe and secretScryptWork the age work factor
// (0: the default).
var (
	secretStoreSettingOverride string
	secretKeyringProbe         func() bool
	secretScryptWork           int
)

// secretStoreSetting is secret_store from config.json ("" = auto).
func secretStoreSetting() string {
	if secretStoreSettingOverride != "" {
		return secretStoreSettingOverride
	}
	v := strings.ToLower(strings.TrimSpace(loadSettings().SecretStore))
	if v == "" {
		return "auto"
	}
	return v
}

// configureSecretStore opens the store for the current config dir and
// setting (again only when one of them changed) and returns it. When it
// can not be opened, ai gets a store that refuses everything: there is
// no silent fallback to the plain file.
func configureSecretStore() (secrets.Store, error) {
	dir, err := ai.ConfigDir()
	if err != nil {
		return nil, err
	}
	path, err := ai.CredentialsPath()
	if err != nil {
		return nil, err
	}
	setting := secretStoreSetting()
	key := path + "\x00" + setting
	secretStoreConfig.Lock()
	defer secretStoreConfig.Unlock()
	if secretStoreConfig.key == key {
		// Set again: a test may have cleared ai's store.
		ai.SetSecretStoreForPath(path, secretStoreConfig.store)
		return secretStoreConfig.store, secretStoreConfig.err
	}
	if old := secretStoreConfig.store; old != nil {
		old.Lock() // the values of a store we leave are dropped
	}
	store, err := secrets.Open(secrets.Options{
		Dir: dir, PlainPath: path, Backend: setting,
		// Until SS-4 migrates it, an existing plain credentials file
		// stays the backend under auto (no prompt, no data loss).
		KeepPlainFile:    true,
		ScryptWorkFactor: secretScryptWork,
		Workspace:        aiWorkspaceRoot(),
		Register:         ai.RegisterSecret,
		Redact:           ai.Redact,
		KeyringProbe:     secretKeyringProbe,
		OnUse:            secretsTouch, // the idle lock (secrets_lock_timer.go)
	})
	if err != nil {
		store = brokenSecretStore{err: err}
	}
	ai.SetSecretStoreForPath(path, store)
	secretStoreConfig.key = key
	secretStoreConfig.store = store
	secretStoreConfig.err = err
	return store, err
}

// databaseSecretStore is the store for the Databases view (passwords,
// SSH keys); nil only when the config dir is unknown.
func databaseSecretStore() secrets.Store {
	s, _ := configureSecretStore()
	return s
}

// brokenSecretStore stands in for a store that could not be opened: it
// reads as locked (features say "not configured") and refuses writes
// with the open error.
type brokenSecretStore struct{ err error }

func (b brokenSecretStore) Backend() string                  { return "none" }
func (b brokenSecretStore) Locked() bool                     { return true }
func (b brokenSecretStore) Unlock(string) error              { return b.err }
func (b brokenSecretStore) Lock()                            {}
func (b brokenSecretStore) Get(string) (string, bool, error) { return "", false, b.err }
func (b brokenSecretStore) Set(string, string) error         { return b.err }
func (b brokenSecretStore) Delete(string) error              { return b.err }
func (b brokenSecretStore) Names() []string                  { return nil }

// secretStore is the app's one store (see configureSecretStore).
func (m *Model) secretStore() (secrets.Store, error) { return configureSecretStore() }

// secretsBackendLabel names a backend for the user.
func secretsBackendLabel(s secrets.Store) string {
	switch s.Backend() {
	case "age":
		return "encrypted file"
	case "keyring":
		return "OS keyring"
	case "plain":
		return "plain file"
	}
	return "secret store"
}

// secretsAgeFileExists reports a secrets.age to unlock (without one an
// unlock creates it).
func secretsAgeFileExists() bool {
	d, err := ai.ConfigDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(d, secrets.AgeFile))
	return err == nil
}

// secretsPaletteItems are the palette's secret store commands.
func secretsPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: "secrets-unlock", Title: "AI: Unlock Secrets"},
		{ID: "secrets-lock", Title: "AI: Lock Secrets"},
		{ID: "secrets-store", Title: "AI: Secret Store…"},
		{ID: "secrets-migrate", Title: "AI: Move Secrets to a Safer Store…"},
		{ID: "secrets-passphrase", Title: "AI: Change Secrets Passphrase…"},
	}
}

// secretsState is the unlock flow's state, shared by Model copies.
type secretsState struct {
	tries      int    // wrong passphrases in this round
	asked      bool   // the prompt opened on a need once (until Lock)
	creating   bool   // no secrets.age yet: the prompt sets a new passphrase
	first      []byte // creating: the first entry until it is typed again
	busy       bool   // an Unlock runs off the UI goroutine
	forgetName string // the name the forget card asks about
	// The move from the plain file (secrets_migrate.go): migrating sends
	// the passphrase to the move instead of an unlock; migrateAsked marks
	// the card shown at start (its Esc counts as "later");
	// finishPending finishes a cut-off move after the next unlock.
	migrating     bool
	migrateAsked  bool
	finishPending bool
	// deletePlainWaiting is the store the Delete the old plain file? card
	// names when the move ended under another card; it opens once no
	// dialog is on screen (showWaitingSecretsDeletePlain).
	deletePlainWaiting string
	// switching sends the passphrase to Switch backend… → Encrypted
	// file (secrets_switch.go) instead of an unlock.
	switching bool
	// after runs once the store is unlocked (the step that needed it).
	after func(*Model) tea.Cmd
	// chg is AI: Change Secrets Passphrase… while it asks
	// (secrets_passphrase.go); the passphrase prompt is routed there.
	chg *secretsChange
	// The idle lock (secrets_lock_timer.go): lockGen is the generation
	// of the one timer, lockArmed reports it runs.
	lockGen   int
	lockArmed bool
}

func (m *Model) secretsS() *secretsState {
	if m.secrets == nil {
		m.secrets = &secretsState{}
	}
	return m.secrets
}

// forgetFirst zeroes and drops the first entry of a new passphrase.
func (s *secretsState) forgetFirst() {
	clear(s.first)
	s.first = nil
}

func (m *Model) secretsPromptOpen() bool {
	return m.promptOpen && m.promptKind == promptKindSecretsPass
}

func (m *Model) secretsToast(level toast.Severity, title, detail string) tea.Cmd {
	return m.pushToastDetail(level, title, detail)
}

// secretsTriesLeft is "2 tries left" / "1 try left".
func secretsTriesLeft(n int) string {
	if n == 1 {
		return "1 try left"
	}
	return strconv.Itoa(n) + " tries left"
}

func secretsRedact(err error) string { return ai.Redact(err.Error()) }

// secretsUnlockDoneMsg ends secretsUnlockCmd. It never carries the
// passphrase.
type secretsUnlockDoneMsg struct{ err error }

// dispatchSecretsPalette runs a "secrets-…" palette id. ok=false: not ours.
func (m *Model) dispatchSecretsPalette(id string) (tea.Cmd, bool) {
	switch id {
	case "secrets-unlock":
		return m.openSecretsUnlock(nil), true
	case "secrets-lock":
		return m.secretsLock(), true
	case "secrets-store":
		return m.openSecretsStore(), true
	case "secrets-migrate":
		return m.secretsMigratePalette(), true
	case "secrets-passphrase":
		return m.openSecretsChangePassphrase(), true
	}
	return nil, false
}

// secretsLocked reports a store whose secrets exist but are locked: the
// setup card says "locked" and AI entry points ask for the passphrase.
func secretsLocked() bool {
	s, err := configureSecretStore()
	if err != nil || s == nil {
		return false
	}
	return s.Backend() == "age" && s.Locked() && secretsAgeFileExists()
}

// encryptedSecretStore is the store when it may hold a secret that must
// not go to a plain file: an unlocked age file or the OS keyring. SS-3b's
// registry "Remember in termocode" option is offered only when ok.
func (m *Model) encryptedSecretStore() (secrets.Store, bool) {
	s, err := m.secretStore()
	if err != nil || s == nil || s.Locked() {
		return nil, false
	}
	switch s.Backend() {
	case "age", "keyring":
		return s, true
	}
	return nil, false
}

// secretsAskOnNeed is called by an entry point that found no secret: a
// locked store asks for its passphrase once per process (and again after
// AI: Lock Secrets). asked=true: the caller stops quietly.
func (m *Model) secretsAskOnNeed() (tea.Cmd, bool) {
	st := m.secretsS()
	if m.secretsPromptOpen() || st.busy {
		return nil, true
	}
	if st.asked || !secretsLocked() {
		return nil, false
	}
	return m.openSecretsUnlock(nil), true
}

// withSecretsUnlocked runs then now, or after the passphrase when the
// store is locked (saving a secret needs an open store; a new age file
// is created with the passphrase typed twice).
func (m *Model) withSecretsUnlocked(then func(*Model) tea.Cmd) tea.Cmd {
	s, err := m.secretStore()
	if err != nil {
		return m.secretsToast(toast.Errr, "Secret store not available", secretsRedact(err))
	}
	if s == nil || !s.Locked() {
		return then(m)
	}
	return m.openSecretsUnlock(then)
}

// secretsOpenFirst runs before a step that saves a secret: with the
// store locked (or not yet created) it opens the passphrase prompt and
// then becomes the next step; wait=false: the store is open, go on.
func (m *Model) secretsOpenFirst(then func(*Model) tea.Cmd) (cmd tea.Cmd, wait bool) {
	if m.secretsS().busy {
		return nil, true // an unlock runs; the store answers after it
	}
	if s, err := m.secretStore(); err == nil && s != nil && !s.Locked() {
		return nil, false
	}
	return m.withSecretsUnlocked(then), true
}

// openSecretsUnlock opens the masked passphrase prompt (AI: Unlock
// Secrets, or a need). after runs once unlocked.
func (m *Model) openSecretsUnlock(after func(*Model) tea.Cmd) tea.Cmd {
	st := m.secretsS()
	s, err := m.secretStore()
	if err != nil {
		return m.secretsToast(toast.Errr, "Secret store not available", secretsRedact(err))
	}
	if s == nil || !s.Locked() {
		if after != nil {
			return after(m)
		}
		label := "the store"
		if s != nil {
			label = "the " + secretsBackendLabel(s)
		}
		return m.secretsToast(toast.Info, "Secrets are not locked", "Nothing to unlock: "+label+" has no passphrase or is open.")
	}
	if st.busy {
		return nil
	}
	st.asked = true
	st.after = after
	st.endSecretsChange()
	st.forgetFirst()
	st.creating = !secretsAgeFileExists()
	m.openSecretsPrompt("")
	return nil
}

// openSecretsPrompt opens the masked prompt for the current step; note
// is a line about the last try ("Wrong passphrase (2 tries left).").
func (m *Model) openSecretsPrompt(note string) {
	st := m.secretsS()
	title, label := "Unlock secrets", "Secrets passphrase:"
	text := "Asked once while termocode runs. AI: Lock Secrets forgets it."
	switch {
	case st.creating && st.first == nil:
		title, label = "New secrets passphrase", "Secrets passphrase:"
		text = "Your secrets go to an encrypted file (" + secrets.AgeFile + "). Choose a passphrase of at least " +
			strconv.Itoa(secrets.MinPassphrase) + " characters (" + strconv.Itoa(secrets.GoodPassphrase) +
			" or more is better). It can not be recovered."
	case st.creating:
		title, label = "New secrets passphrase", "Type it again:"
		text = "Type the same passphrase again."
	}
	if note != "" {
		text = note + " " + text
	}
	m.prompt = prompt.New(title, label, "")
	m.prompt.SetMasked(true)
	m.prompt.SetNote(text)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindSecretsPass
}

// handleSecretsPromptSubmit is Enter in the passphrase prompt.
func (m *Model) handleSecretsPromptSubmit(value string) (tea.Cmd, bool) {
	if m.promptKind != promptKindSecretsPass {
		return nil, false
	}
	m.clearPrompt() // the typed passphrase must not stay in the model
	st := m.secretsS()
	if st.busy {
		return nil, true
	}
	if st.chg != nil { // AI: Change Secrets Passphrase… (secrets_passphrase.go)
		return m.handleSecretsChangeSubmit(value), true
	}
	if utf8.RuneCountInString(value) < secrets.MinPassphrase {
		m.openSecretsPrompt("Too short: at least " + strconv.Itoa(secrets.MinPassphrase) + " characters.")
		return nil, true
	}
	pass := []byte(value)
	if !st.creating {
		return m.secretsPassEntered(pass), true
	}
	if st.first == nil {
		st.first = pass
		note := ""
		if weak, _ := secrets.CheckPassphrase(value); weak {
			note = "Weak: under " + strconv.Itoa(secrets.GoodPassphrase) + " characters is easy to guess. Type it again to use it anyway, or Esc."
		}
		m.openSecretsPrompt(note)
		return nil, true
	}
	same := subtle.ConstantTimeCompare(st.first, pass) == 1
	st.forgetFirst()
	if !same {
		clear(pass)
		m.openSecretsPrompt("The two passphrases differ. Choose one again.")
		return nil, true
	}
	return m.secretsPassEntered(pass), true
}

// secretsPromptClosed is Esc in the passphrase prompt: nothing is kept.
func (m *Model) secretsPromptClosed() tea.Cmd {
	if m.promptKind != promptKindSecretsPass {
		return nil
	}
	m.clearPrompt()
	st := m.secretsS()
	st.forgetFirst()
	st.endSecretsChange()
	st.after = nil
	st.migrating, st.switching = false, false
	return nil
}

// secretsUnlockCmd unlocks (or creates) the store off the UI goroutine:
// the age scrypt run takes about a second. pass is zeroed after use.
func (m *Model) secretsUnlockCmd(pass []byte) tea.Cmd {
	s, err := m.secretStore()
	if err != nil || s == nil {
		clear(pass)
		return m.secretsToast(toast.Errr, "Secret store not available", "The store could not be opened.")
	}
	m.secretsS().busy = true
	return func() tea.Msg {
		err := s.Unlock(string(pass))
		clear(pass)
		return secretsUnlockDoneMsg{err: err}
	}
}

// onSecretsUnlockDone counts a wrong passphrase (three end the round) or
// reloads what reads secrets and runs the step that waited.
func (m *Model) onSecretsUnlockDone(msg secretsUnlockDoneMsg) tea.Cmd {
	st := m.secretsS()
	st.busy = false
	switch {
	case msg.err == nil:
		st.tries = 0
		created := st.creating
		st.creating = false
		after := st.after
		st.after = nil
		m.secretsChanged()
		title := "Secrets unlocked"
		if created {
			title = "Secret store created"
		}
		cmds := []tea.Cmd{m.secretsToast(toast.Info, title, "Kept while termocode runs · F1 → AI: Lock Secrets"), m.secretsArmLock()}
		if after != nil {
			cmds = append(cmds, after(m))
		}
		if st.finishPending { // a cut-off move (secrets_migrate.go)
			cmds = append(cmds, m.secretsFinishMove())
		}
		return tea.Batch(cmds...)
	case errors.Is(msg.err, secrets.ErrWrongPassphrase):
		st.tries++
		if st.tries >= secretsMaxTries {
			st.tries = 0
			st.after = nil
			return m.secretsToast(toast.Warn, "Secrets stay locked", "F1 → AI: Unlock Secrets to try again.")
		}
		left := secretsTriesLeft(secretsMaxTries - st.tries)
		m.openSecretsPrompt("Wrong passphrase (" + left + ").")
		return m.secretsToast(toast.Warn, "Wrong passphrase", left)
	}
	st.after = nil
	st.creating = false
	if errors.Is(msg.err, secrets.ErrCorrupt) && secretsAgeFileExists() {
		m.openSecretsBrokenCard(msg.err) // secrets_failures.go
		return nil
	}
	return m.secretsToast(toast.Errr, "Could not unlock secrets", secretsRedact(msg.err))
}

// secretsLock is AI: Lock Secrets: the store forgets the passphrase and
// the values; the next need asks again.
func (m *Model) secretsLock() tea.Cmd {
	s, err := m.secretStore()
	if err != nil {
		return m.secretsToast(toast.Errr, "Secret store not available", secretsRedact(err))
	}
	if s == nil || s.Backend() != "age" {
		label := "this store"
		if s != nil {
			label = "the " + secretsBackendLabel(s)
		}
		return m.secretsToast(toast.Info, "Nothing to lock", "Only the encrypted file has a passphrase; "+label+" has none.")
	}
	s.Lock()
	m.secretsDisarmLock()
	st := m.secretsS()
	st.asked, st.tries = false, 0
	m.secretsChanged()
	return m.secretsToast(toast.Info, "Secrets locked", "F1 → AI: Unlock Secrets to use them again.")
}

// secretsChanged re-reads everything that holds secrets after an unlock
// or a lock: the AI provider and its inline completions.
func (m *Model) secretsChanged() {
	if m.ai != nil {
		m.ai.reload()
		m.syncAIInline()
	}
}

// aiKeyHome says where a saved AI key goes, for the key prompt.
func (m *Model) aiKeyHome() string {
	s, err := m.secretStore()
	if err != nil || s == nil {
		return "not saved: the secret store is not available"
	}
	switch s.Backend() {
	case "age":
		return "kept in the encrypted file " + secrets.AgeFile
	case "keyring":
		return "kept in the OS keyring"
	}
	return "plain file ai/credentials.json, not encrypted"
}
