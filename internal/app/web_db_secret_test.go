package app

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webSecretCall runs db.secret and fails the test on an err.
func (w *webTunnelDB) secret(t *testing.T, kind, value string) map[string]any {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"conn": "tun", "kind": kind, "value": value})
	return w.call(t, "db.secret", string(b))
}

// webNoSecret fails when value is in an answer the page got or in the
// audit.
func webNoSecret(t *testing.T, m *Model, value string, frames ...map[string]any) {
	t.Helper()
	for _, f := range frames {
		if b, _ := json.Marshal(f); strings.Contains(string(b), value) {
			t.Errorf("the secret is in an answer: %s", b)
		}
	}
	if a := webTestAudit(m); strings.Contains(a, value) {
		t.Errorf("the secret is in the audit:\n%s", a)
	}
}

// Spec: an SSH password the tunnel needs is typed on the page: the read
// says so (need ssh_password), db.secret keeps the value in the laptop's
// secret store only (db.<id>.ssh, as the laptop's own prompt does), a
// wrong one is said to be wrong, and the read then works. The value is in
// no answer and no audit line.
func TestWebDBSSHPasswordFromPage(t *testing.T) {
	srv := dbtest.NewSSHServer()
	srv.Password = "tunnel-pw-77"
	defer srv.Close()
	w := newWebTunnelDB(t, srv, srv, dbclient.SSHConfig{Host: "bastion.example.com", User: "deploy", Auth: "password"})
	if err := dbclient.TrustHostKey(filepath.Join(w.home, ".ssh", "known_hosts"), "bastion.example.com:22", srv.HostKey.PublicKey()); err != nil {
		t.Fatal(err)
	}
	f1 := w.call(t, webproto.OpDBTables, `{"conn":"tun"}`)
	if need := webNeed(t, f1); need["need"] != "ssh_password" || need["wrong"] == true {
		t.Fatalf("need %v", need)
	}
	f2 := w.secret(t, "ssh_password", "wrong-pw-1")
	if webTestErr(f2) != "" {
		t.Fatalf("db.secret: %v", f2)
	}
	f3 := w.call(t, webproto.OpDBTables, `{"conn":"tun"}`)
	if need := webNeed(t, f3); need["need"] != "ssh_password" || need["wrong"] != true {
		t.Fatalf("after a wrong password, need %v", need)
	}
	f4 := w.secret(t, "ssh_password", "tunnel-pw-77")
	var tb webproto.DBTables
	f5 := w.call(t, webproto.OpDBTables, `{"conn":"tun"}`)
	webTestData(t, f5, &tb)
	if len(tb.Tables) != 2 {
		t.Fatalf("tables %+v", tb)
	}
	if v, ok, _ := databaseSecretStore().Get("db.tun.ssh"); !ok || v != "tunnel-pw-77" {
		t.Fatalf("store has %q, %v", v, ok)
	}
	if a := webTestAudit(w.m); !strings.Contains(a, "db.secret  tun ssh_password") {
		t.Errorf("audit:\n%s", a)
	}
	webNoSecret(t, w.m, "tunnel-pw-77", f1, f2, f3, f4, f5)
	webNoSecret(t, w.m, "wrong-pw-1", f1, f2, f3, f4, f5)
}

// Spec: the passphrase of an SSH key file is asked with the key's name
// and kept as db.<id>.sshkey.
func TestWebDBSSHKeyPassphraseFromPage(t *testing.T) {
	srv := dbtest.NewSSHServer()
	defer srv.Close()
	w := newWebTunnelDB(t, srv, srv, dbclient.SSHConfig{Host: "bastion.example.com", User: "deploy", Auth: "key", KeyFile: "~/.ssh/id_ed25519"})
	srv.AuthorizedKeys = append(srv.AuthorizedKeys, writeTestKey(t, filepath.Join(w.home, ".ssh", "id_ed25519"), "key-pp-9"))
	if err := dbclient.TrustHostKey(filepath.Join(w.home, ".ssh", "known_hosts"), "bastion.example.com:22", srv.HostKey.PublicKey()); err != nil {
		t.Fatal(err)
	}
	if need := webNeed(t, w.call(t, webproto.OpDBTables, `{"conn":"tun"}`)); need["need"] != "ssh_passphrase" || need["key"] != "id_ed25519" {
		t.Fatalf("need %v", need)
	}
	if f := w.secret(t, "ssh_passphrase", "key-pp-9"); webTestErr(f) != "" {
		t.Fatalf("db.secret: %v", f)
	}
	var tb webproto.DBTables
	webTestData(t, w.call(t, webproto.OpDBTables, `{"conn":"tun"}`), &tb)
	if v, ok, _ := databaseSecretStore().Get("db.tun.sshkey"); !ok || v != "key-pp-9" {
		t.Fatalf("store has %q, %v", v, ok)
	}
	if f := w.secret(t, "ssh_password", "x"); webTestErr(f) != "bad_request" {
		t.Fatalf("a password for a key login: %v", f)
	}
}

// Spec: the database's own password is typed on the page when the laptop
// has none: the read says the database needs it (need password), and
// says when it was not accepted; db.secret keeps it as db.<id> in the
// secret store, as the laptop's Set Password does.
func TestWebDBPasswordFromPage(t *testing.T) {
	m, c, _ := webTestDB(t, nil)
	delete(m.databases.passwords, "local")
	var tried []string
	old := dbSQLSessionFactory
	dbSQLSessionFactory = func(_ context.Context, _ dbclient.Conn, pw string) (dbclient.Session, error) {
		tried = append(tried, pw)
		if pw != "db-pw-5" {
			return nil, errors.New("failed to connect to `user=app database=app`: 127.0.0.1:5432 (localhost): failed SASL auth: FATAL: password authentication failed for user \"app\" (SQLSTATE 28P01)")
		}
		return &aiDBSession{name: "web-local"}, nil
	}
	t.Cleanup(func() { dbSQLSessionFactory = old })
	call := func(op, args string) map[string]any { return webTestCallBg(t, m, c, op, args) }
	f1 := call(webproto.OpDBTables, `{"conn":"local"}`)
	if need := webNeed(t, f1); need["need"] != "password" || need["wrong"] == true {
		t.Fatalf("need %v (%v)", need, f1)
	}
	f2 := call("db.secret", `{"conn":"local","kind":"password","value":"nope-3"}`)
	f3 := call(webproto.OpDBTables, `{"conn":"local"}`)
	if need := webNeed(t, f3); need["need"] != "password" || need["wrong"] != true {
		t.Fatalf("after a wrong password, need %v", need)
	}
	f4 := call("db.secret", `{"conn":"local","kind":"password","value":"db-pw-5"}`)
	var tb webproto.DBTables
	f5 := call(webproto.OpDBTables, `{"conn":"local"}`)
	webTestData(t, f5, &tb)
	if len(tb.Tables) != 2 || tried[len(tried)-1] != "db-pw-5" {
		t.Fatalf("tables %+v, tried %q", tb, tried)
	}
	if v, ok, _ := databaseSecretStore().Get("db.local"); !ok || v != "db-pw-5" {
		t.Fatalf("store has %q, %v", v, ok)
	}
	if f := call("db.secret", `{"conn":"local","kind":"ssh_password","value":"x"}`); webTestErr(f) != "bad_request" {
		t.Fatalf("an SSH password for a connection with no tunnel: %v", f)
	}
	webNoSecret(t, m, "db-pw-5", f1, f2, f3, f4, f5)
	webNoSecret(t, m, "nope-3", f1, f2, f3, f4, f5)
}
