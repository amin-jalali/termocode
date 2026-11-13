package dbclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteCopyCSVAndSniffCopyCSV(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	path := filepath.Join(dir, "copy.csv")
	n, err := WriteCopyCSV(ctx, path, func(_ context.Context, sink RowSink) error {
		if err := sink.Columns([]Column{{Name: "a"}, {Name: "b"}}); err != nil {
			return err
		}
		if err := sink.Row([]Cell{{Text: "1"}, {Null: true}}); err != nil {
			return err
		}
		return sink.Row([]Cell{{Text: "x;y"}})
	}, nil)
	if err != nil || n != 2 {
		t.Fatalf("copy = %d, %v", n, err)
	}
	sn, err := SniffCopyCSV(path)
	if err != nil || sn.Delimiter != ',' || sn.Fields != 2 || !sn.Header {
		t.Errorf("sniff = %+v, %v", sn, err)
	}
	if _, err := WriteCopyCSV(ctx, path, func(context.Context, RowSink) error { return nil }, nil); err == nil {
		t.Error("an existing file is never replaced")
	}
	// Failures remove the file again.
	for name, src := range map[string]func(context.Context, RowSink) error{
		"source error": func(context.Context, RowSink) error { return errors.New("source down") },
		"no columns":   func(context.Context, RowSink) error { return nil },
		"row first":    func(_ context.Context, s RowSink) error { return s.Row([]Cell{{Text: "x"}}) },
	} {
		p := filepath.Join(dir, strings.ReplaceAll(name, " ", "-")+".csv")
		if _, err := WriteCopyCSV(ctx, p, src, nil); err == nil {
			t.Errorf("%s: want error", name)
		}
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s: a failed copy leaves nothing behind", name)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	p := filepath.Join(dir, "cancel.csv")
	if _, err := WriteCopyCSV(cancelled, p, func(context.Context, RowSink) error { return nil }, nil); err == nil {
		t.Error("a cancelled copy: want error")
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("a cancelled copy leaves nothing behind")
	}
	for name, content := range map[string]string{"empty": "  \n", "no rows": ""} {
		f := filepath.Join(dir, name+".csv")
		if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := SniffCopyCSV(f); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if _, err := SniffCopyCSV(filepath.Join(dir, "none.csv")); err == nil || !strings.Contains(err.Error(), "cannot open") {
		t.Errorf("a missing file = %v", err)
	}
}

func TestTunnelHostMappingAndLocalUser(t *testing.T) {
	tun := &Tunnel{}
	if tun.HostMapped("db") != "" || tun.mapped("db:5432") != "db:5432" || tun.mapped("no-port") != "no-port" {
		t.Error("nothing is mapped yet")
	}
	tun.MapHost("db", "10.0.0.7")
	tun.MapHost("other", "10.0.0.8")
	if tun.HostMapped("db") != "10.0.0.7" || tun.mapped("db:5432") != "10.0.0.7:5432" || tun.mapped("elsewhere:1") != "elsewhere:1" {
		t.Errorf("mapped = %q", tun.mapped("db:5432"))
	}
	t.Setenv("USER", "envuser")
	if localUserName() == "" {
		t.Error("the local user name is found")
	}
	var o = TunnelOptions{}
	if _, _, err := sshAuth(context.Background(), SSHTarget{Auth: SSHAuthPassword}, o, nil); err == nil {
		t.Error("a password login without a password: want a secret error")
	} else if _, ok := err.(*SSHSecretError); !ok {
		t.Errorf("error = %T", err)
	}
	methods, stop, err := sshAuth(context.Background(), SSHTarget{Auth: SSHAuthPassword}, TunnelOptions{Password: "pw"}, nil)
	if err != nil || len(methods) != 2 {
		t.Fatalf("password auth = %v %v", methods, err)
	}
	stop()
}
