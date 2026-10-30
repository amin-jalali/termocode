package app

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// covSvcNoSSH makes every SSH dial fail at once (no network) and keeps
// ~/.ssh in the test home.
func covSvcNoSSH(t *testing.T) {
	t.Helper()
	oldDial, oldHome := dbTunnelDial, dbSSHHome
	dbTunnelDial = func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("no route to host") }
	home := os.Getenv("HOME")
	dbSSHHome = func() string { return home }
	t.Cleanup(func() { dbTunnelDial, dbSSHHome = oldDial, oldHome })
}

func covSvcSSHPrompt(t *testing.T, m *Model, kind promptKindEnum, value string) {
	t.Helper()
	if _, ok := m.databaseSSHPrompt(kind, value); !ok {
		t.Fatalf("prompt %d not handled", kind)
	}
}

// SSH tunnel steps on a saved connection: each empty or bad answer is a
// note; the tunnel is saved in the project file with the passphrase in
// the store, then tested.
func TestCovSvcDBSSHTunnelSteps(t *testing.T) {
	m, _ := covSvcDB(t, `{"id":"pg","driver":"postgres","host":"db.internal","port":5432,"user":"app","database":"app"}`)
	covSvcNoSSH(t)
	s := m.databases
	m.databaseSSHStart("")
	if !strings.Contains(covSvcToast(m), "Select a connection") {
		t.Errorf("no target toast = %q", covSvcToast(m))
	}
	m.databaseSSHStart("app")
	if !strings.Contains(covSvcToast(m), "No tunnel for SQLite") {
		t.Errorf("sqlite toast = %q", covSvcToast(m))
	}
	m.databaseSSLStart("app")
	if !strings.Contains(covSvcToast(m), "No SSL for SQLite") {
		t.Errorf("sqlite ssl toast = %q", covSvcToast(m))
	}
	m.databaseSSHStart("pg")
	if m.promptKind != promptKindDBSSHHost {
		t.Fatal("no host prompt")
	}
	covSvcSSHPrompt(t, m, promptKindDBSSHHost, " ")
	if !strings.Contains(covSvcToast(m), "SSH server is needed") {
		t.Errorf("host toast = %q", covSvcToast(m))
	}
	covSvcSSHPrompt(t, m, promptKindDBSSHHost, "bastion.example")
	covSvcSSHPrompt(t, m, promptKindDBSSHPort, "abc")
	if !strings.Contains(covSvcToast(m), "That port is not valid") {
		t.Errorf("port toast = %q", covSvcToast(m))
	}
	covSvcSSHPrompt(t, m, promptKindDBSSHPort, "2222")
	covSvcSSHPrompt(t, m, promptKindDBSSHUser, "")
	if !strings.Contains(covSvcToast(m), "SSH user is needed") {
		t.Errorf("user toast = %q", covSvcToast(m))
	}
	covSvcSSHPrompt(t, m, promptKindDBSSHUser, "deploy")
	if !m.pickerOpen || m.pickerKind != pickerKindDBSSHAuth {
		t.Fatal("no auth picker")
	}
	m.handleHTTPPickerSelect(dbclient.SSHAuthKey)
	if m.promptKind != promptKindDBSSHKeyFile || m.prompt.Value() != "~/.ssh/id_ed25519" {
		t.Errorf("key prompt %d %q", m.promptKind, m.prompt.Value())
	}
	covSvcSSHPrompt(t, m, promptKindDBSSHKeyFile, "")
	if !strings.Contains(covSvcToast(m), "Key file is needed") {
		t.Errorf("key toast = %q", covSvcToast(m))
	}
	covSvcSSHPrompt(t, m, promptKindDBSSHKeyFile, "~/.ssh/deploy_key")
	c, _ := m.databaseSSHPrompt(promptKindDBSSHSecret, "key-pass-123")
	covSvcRun(t, m, c)
	b, _ := os.ReadFile(filepath.Join(s.root, ".termocode", "databases.json"))
	if !strings.Contains(string(b), "bastion.example") || !strings.Contains(string(b), "deploy_key") || strings.Contains(string(b), "key-pass-123") {
		t.Errorf("project file = %s", b)
	}
	if v, ok, _ := databaseSecretStore().Get(dbSSHSecretName("pg", "passphrase")); !ok || v != "key-pass-123" {
		t.Errorf("stored passphrase = %q %v", v, ok)
	}
	// The agent: no secret asked, saved at once.
	m.databaseSSHStart("pg")
	covSvcSSHPrompt(t, m, promptKindDBSSHHost, "bastion.example")
	covSvcSSHPrompt(t, m, promptKindDBSSHPort, "22")
	covSvcSSHPrompt(t, m, promptKindDBSSHUser, "deploy")
	covSvcRun(t, m, func() tea.Cmd { c, _ := m.handleHTTPPickerSelect(dbclient.SSHAuthAgent); return c }())
	if c, _ := s.connection("pg"); c.SSH == nil || c.SSH.Auth != dbclient.SSHAuthAgent || c.SSH.KeyFile != "" {
		t.Errorf("agent ssh = %+v", c.SSH)
	}
	// A transaction open: saved, but the old connection stays.
	s.txs = map[string]*dbTxState{"pg": {open: true}}
	m.databaseSSHStart("pg")
	covSvcSSHPrompt(t, m, promptKindDBSSHHost, "bastion.example")
	covSvcSSHPrompt(t, m, promptKindDBSSHPort, "22")
	covSvcSSHPrompt(t, m, promptKindDBSSHUser, "deploy")
	m.handleHTTPPickerSelect(dbclient.SSHAuthPassword)
	if m.promptKind != promptKindDBSSHSecret {
		t.Fatal("no password prompt")
	}
	m.databaseSSHPrompt(promptKindDBSSHSecret, "pw")
	if !strings.Contains(covSvcToast(m), "Saved · reconnect to use it") {
		t.Errorf("tx toast = %q", covSvcToast(m))
	}
}

// SSL settings: Off saves at once; a client certificate needs its key.
func TestCovSvcDBSSLSteps(t *testing.T) {
	m, _ := covSvcDB(t, `{"id":"pg","driver":"postgres","host":"db.internal","port":5432,"user":"app","database":"app"}`)
	covSvcNoSSH(t)
	s := m.databases
	m.databaseSSLStart("pg")
	if !m.pickerOpen || m.pickerKind != pickerKindDBSSLMode {
		t.Fatal("no SSL picker")
	}
	m.handleHTTPPickerSelect(dbclient.SSLVerifyFull)
	covSvcSSHPrompt(t, m, promptKindDBSSLCA, "ca.pem")
	covSvcSSHPrompt(t, m, promptKindDBSSLCert, "client.pem")
	covSvcSSHPrompt(t, m, promptKindDBSSLKey, "")
	if !strings.Contains(covSvcToast(m), "Client key is needed") {
		t.Errorf("key toast = %q", covSvcToast(m))
	}
	c, _ := m.databaseSSHPrompt(promptKindDBSSLKey, "client.key")
	covSvcRun(t, m, c)
	if c, _ := s.connection("pg"); c.SSL == nil || c.SSL.Mode != dbclient.SSLVerifyFull || c.SSL.Key != "client.key" {
		t.Errorf("ssl = %+v", c.SSL)
	}
	m.databaseSSLStart("pg")
	m.handleHTTPPickerSelect(dbclient.SSLRequire)
	covSvcSSHPrompt(t, m, promptKindDBSSLCA, "")
	covSvcRun(t, m, func() tea.Cmd { c, _ := m.databaseSSHPrompt(promptKindDBSSLCert, ""); return c }())
	if c, _ := s.connection("pg"); c.SSL.Mode != dbclient.SSLRequire || c.SSL.Key != "" {
		t.Errorf("require ssl = %+v", c.SSL)
	}
	m.databaseSSLStart("pg")
	covSvcRun(t, m, func() tea.Cmd { c, _ := m.handleHTTPPickerSelect(dbclient.SSLDisable); return c }())
	if c, _ := s.connection("pg"); c.SSL.Mode != dbclient.SSLDisable {
		t.Errorf("off ssl = %+v", c.SSL)
	}
	// Prompts with no edit running do nothing.
	if c, ok := m.databaseSSHPrompt(promptKindDBSSHHost, "x"); !ok || c != nil {
		t.Error("prompt without an edit did something")
	}
	if _, ok := m.databaseSSHPrompt(promptKindDBHost, "x"); ok {
		t.Error("a foreign prompt was taken")
	}
}

// A connection being added gets its tunnel before the test; a secret
// reference that does not resolve is a toast.
func TestCovSvcDBSSHOnPending(t *testing.T) {
	m, _ := covSvcDB(t, "")
	covSvcNoSSH(t)
	s := m.databases
	s.pendingConn = dbclient.Conn{ID: "new", Name: "new", Driver: "postgres", Host: "db.internal", Port: 5432, Database: "d", Source: dbclient.SourceUser}
	m.databaseSSHStart("")
	covSvcSSHPrompt(t, m, promptKindDBSSHHost, "bastion.example")
	covSvcSSHPrompt(t, m, promptKindDBSSHPort, "22")
	covSvcSSHPrompt(t, m, promptKindDBSSHUser, "deploy")
	m.handleHTTPPickerSelect(dbclient.SSHAuthPassword)
	m.databaseSSHPrompt(promptKindDBSSHSecret, "${env:COVSVC_NEVER_SET_X}")
	if !strings.Contains(covSvcToast(m), "Could not resolve the SSH secret") {
		t.Errorf("toast = %q", covSvcToast(m))
	}
	m.databaseSSHStart("")
	covSvcSSHPrompt(t, m, promptKindDBSSHHost, "bastion.example")
	covSvcSSHPrompt(t, m, promptKindDBSSHPort, "22")
	covSvcSSHPrompt(t, m, promptKindDBSSHUser, "deploy")
	m.handleHTTPPickerSelect(dbclient.SSHAuthPassword)
	c, _ := m.databaseSSHPrompt(promptKindDBSSHSecret, "tunnel-pw")
	covSvcRun(t, m, c)
	if s.pendingConn.SSH == nil || s.pendingSSH.password != "tunnel-pw" {
		t.Errorf("pending ssh = %+v %+v", s.pendingConn.SSH, s.pendingSSH)
	}
	// The failed test offers the next steps; Cancel forgets the pending one.
	if !m.pickerOpen || m.pickerKind != pickerKindDBTestResult {
		t.Fatalf("no test result picker: %q", covSvcToast(m))
	}
	for _, id := range []string{"db-test-ssl", "db-test-password", "db-test-ssh"} {
		m.pickerKind = pickerKindDBTestResult
		m.handleHTTPPickerSelect(id)
	}
	m.pickerKind = pickerKindDBTestResult
	m.handleHTTPPickerSelect("db-test-cancel")
	if s.pendingConn.ID != "" {
		t.Error("cancel kept the pending connection")
	}
	m.pickerKind = pickerKindDBTestResult
	if c, _ := m.handleHTTPPickerSelect("db-test-retry"); c != nil {
		t.Error("retry without a pending connection did something")
	}
}
