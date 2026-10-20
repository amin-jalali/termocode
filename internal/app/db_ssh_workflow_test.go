package app_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
	"github.com/amin-jalali/termocode/internal/testkit"
)

// The SSH tunnel through the real UI: palette → prompts → auth picker →
// masked password → test through the tunnel → connect and browse.
func TestSSHTunnelAddedFromPaletteTestsAndConnects(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	f := redisSeed()
	t.Cleanup(f.Close)
	srv := dbtest.NewSSHServer()
	srv.Password = "ui-tunnel-pw-5"
	srv.Forward = func(ctx context.Context, addr string) (net.Conn, error) { return f.Dial(ctx, "tcp", addr) }
	t.Cleanup(srv.Close)
	sshHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sshHome, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := dbclient.TrustHostKey(filepath.Join(sshHome, ".ssh", "known_hosts"), "bastion.test:22", srv.HostKey.PublicKey()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.SetSSHForTest(sshHome, srv.Dial))

	conns := `{"connections":[{"id":"cache","driver":"redis","host":"cache.internal","port":6379}]}`
	d := testkit.New(t, testkit.Options{W: 100, H: 30, Nvim: true, Files: map[string]string{"README.md": "ssh fixture\n", ".termocode/databases.json": conns}})
	d.Key("f1")
	d.Type("Databases: Focus")
	d.Key("enter")
	waitFrame(t, d, "DATABASES", "cache")

	d.Key("f1")
	d.Type("Databases: Add SSH Tunnel")
	d.Key("enter")
	waitFrame(t, d, "SSH tunnel · cache · 1/4")
	d.Type("bastion.test")
	d.Key("enter")
	waitFrame(t, d, "SSH port")
	d.Key("enter")
	waitFrame(t, d, "SSH user")
	d.Key("ctrl+u")
	d.Type("deploy")
	d.Key("enter")
	waitFrame(t, d, "log in with")
	d.Type("Password")
	d.Key("enter")
	waitFrame(t, d, "SSH password")
	d.Type("ui-tunnel-pw-5")
	if strings.Contains(ansi.Strip(d.Frame()), "ui-tunnel-pw-5") {
		t.Fatal("the SSH password is shown while typed")
	}
	d.Key("enter")
	waitFrame(t, d, "Connection works")
	if strings.Contains(ansi.Strip(d.Frame()), "ui-tunnel-pw-5") {
		t.Fatal("the SSH password is on screen")
	}
	conf, err := os.ReadFile(filepath.Join(d.Dir, ".termocode", "databases.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(conf), `"host": "bastion.test"`) || strings.Contains(string(conf), "ui-tunnel-pw-5") {
		t.Fatalf("databases.json:\n%s", conf)
	}
	waitFrame(t, d, "redis ssh")

	d.Key("enter") // connect through the tunnel
	waitFrame(t, d, "db0", "tunnel up")
	if srv.Logins() < 2 || !f.Sent("INFO") {
		t.Fatalf("logins=%d commands=%q", srv.Logins(), f.Commands())
	}
}
