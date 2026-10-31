package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/secrets"
)

const regRememberSecret = "ghp_rememberedToken_9876543210"

// regAgeSession is a registry session whose secret store is an age file
// (unlocked when unlock).
func regAgeSession(t *testing.T, unlock bool) (*Model, string, func() string, func()) {
	t.Helper()
	m, f, home := regSession(t, regConfig)
	withSecretSetting(t, "age", false)
	s, err := configureSecretStore()
	if err != nil {
		t.Fatal(err)
	}
	if unlock {
		if err := s.Unlock(secretsTestPass); err != nil {
			t.Fatal(err)
		}
	}
	stdin := func() string {
		b, _ := os.ReadFile(filepath.Join(f.Data, "login.stdin"))
		return string(b)
	}
	reset := func() { _ = os.WriteFile(filepath.Join(f.Data, "login.stdin"), nil, 0o600) }
	return m, home, stdin, reset
}

// pumpLogin pumps until the login answer arrives, however slow the
// store save after it is.
func pumpLogin(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	pumpHostUntil(t, m, cmd, func(msg tea.Msg) bool { _, ok := msg.(registryLoginDoneMsg); return ok })
}

func startConnect(t *testing.T, m *Model, host string) {
	t.Helper()
	m.startRegistryConnect(host)
	pumpHost(t, m, submitPrompt(t, m, "amin-jalali"))
}

func TestRegistryRememberNotOfferedForPlainStore(t *testing.T) {
	m, _, _ := regSession(t, regConfig) // TestMain: plain
	startConnect(t, m, "ghcr.io")
	if m.confirmOpen || !m.promptOpen || m.promptKind != promptKindRegistryToken {
		t.Fatalf("plain store: confirm=%v prompt=%v kind=%v", m.confirmOpen, m.promptOpen, m.promptKind)
	}
}

func TestRegistryRememberOfferedOnlyWhenUnlocked(t *testing.T) {
	m, _, _, _ := regAgeSession(t, false)
	startConnect(t, m, "ghcr.io")
	if m.confirmOpen || m.promptKind != promptKindRegistryToken {
		t.Fatal("remember offered while the store is locked")
	}
}

func TestRegistryRememberSavesAndReuses(t *testing.T) {
	m, home, stdin, reset := regAgeSession(t, true)
	startConnect(t, m, "ghcr.io")
	if !m.confirmOpen || m.confirmKind != confirmKindRegistryRemember {
		t.Fatal("no remember card")
	}
	if got := ansi.Strip(m.confirm.View()); !strings.Contains(got, "Remember") {
		t.Fatalf("card:\n%s", got)
	}
	answerStoreConfirm(t, m, "remember")
	if !m.promptOpen || m.promptKind != promptKindRegistryToken {
		t.Fatal("no token prompt after Remember")
	}
	pumpLogin(t, m, submitPrompt(t, m, regRememberSecret))
	if stdin() != regRememberSecret+"\n" {
		t.Fatalf("login stdin %q", stdin())
	}
	s, _ := configureSecretStore()
	if v, ok, err := s.Get("registry.ghcr.io"); err != nil || !ok || v != regRememberSecret {
		t.Fatalf("saved %q ok=%v err=%v", v, ok, err)
	}
	if got := regToast(m); strings.Contains(got, regRememberSecret) || !strings.Contains(got, "remembered") {
		t.Fatalf("toast %q", got)
	}
	noSecretOnDisk(t, regRememberSecret, home, m.services.root)

	// A later sign-in offers the saved token and uses it on stdin.
	reset()
	startConnect(t, m, "ghcr.io")
	if !m.confirmOpen || m.confirmKind != confirmKindRegistryRemember {
		t.Fatal("no saved-token card")
	}
	if got := ansi.Strip(m.confirm.View()); !strings.Contains(got, "saved") || strings.Contains(got, regRememberSecret) {
		t.Fatalf("card:\n%s", got)
	}
	pumpLogin(t, m, answerStoreConfirm(t, m, "use"))
	if stdin() != regRememberSecret+"\n" {
		t.Fatalf("second login stdin %q", stdin())
	}
}

func TestRegistryNotNowDoesNotSave(t *testing.T) {
	m, _, _, _ := regAgeSession(t, true)
	startConnect(t, m, "ghcr.io")
	answerStoreConfirm(t, m, "once")
	pumpLogin(t, m, submitPrompt(t, m, regRememberSecret))
	s, _ := configureSecretStore()
	if len(s.Names()) != 0 {
		t.Fatalf("saved without asking: %v", s.Names())
	}
}

func TestRegistrySavedTokenOnLockedStoreUnlocksFirst(t *testing.T) {
	m, _, stdin, _ := regAgeSession(t, true)
	s, _ := configureSecretStore()
	if err := s.Set("registry.ghcr.io", regRememberSecret); err != nil {
		t.Fatal(err)
	}
	s.Lock()
	startConnect(t, m, "ghcr.io")
	if !m.confirmOpen {
		t.Fatal("saved token not offered while locked")
	}
	pumpSecrets(t, m, answerStoreConfirm(t, m, "use"))
	if !m.secretsPromptOpen() {
		t.Fatal("no passphrase prompt")
	}
	submitUnlockThenLogin(t, m, secretsTestPass)
	if stdin() != regRememberSecret+"\n" {
		t.Fatalf("stdin %q", stdin())
	}
}

// submitUnlockThenLogin types the passphrase and runs what follows until
// the login the unlock lets go has answered. submitSecrets stops 100 ms
// after the unlock: a login slower than that (the macOS runner) had not
// written its stdin yet, and the test read "".
func submitUnlockThenLogin(t *testing.T, m *Model, pass string) {
	t.Helper()
	if !m.secretsPromptOpen() {
		t.Fatalf("no passphrase prompt open (kind %v)", m.promptKind)
	}
	next, cmd := m.update(prompt.SubmitMsg{Value: pass})
	*m = next
	results := make(chan tea.Msg, 64)
	start := func(c tea.Cmd) {
		if c != nil {
			go func() { results <- c() }()
		}
	}
	start(cmd)
	deadline := time.After(30 * time.Second)
	for {
		select {
		case got := <-results:
			switch msg := got.(type) {
			case tea.BatchMsg:
				for _, c := range msg {
					start(c)
				}
			case secretsUnlockDoneMsg:
				next, more := m.update(msg)
				*m = next
				start(more)
			case registryLoginDoneMsg:
				m.handleServicesMsg(msg)
				return
			}
		case <-deadline:
			t.Fatal("the login after the unlock never answered")
		}
	}
}

var _ = secrets.AgeFile
