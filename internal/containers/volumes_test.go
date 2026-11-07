package containers

import (
	"reflect"
	"strings"
	"testing"
)

func TestVolumeCreateArgsWithDriverOptions(t *testing.T) {
	got, err := VolumeCreateArgs(VolumeCreateOptions{
		Name:    "data",
		Driver:  "local",
		Options: map[string]string{"type": "nfs", "device": ":/srv/share"},
		Labels:  map[string]string{"team": "web"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"volume", "create", "--driver", "local", "--label", "team=web", "--opt", "device=:/srv/share", "--opt", "type=nfs", "--", "data"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %q\nwant %q", got, want)
	}
}

func TestNetworkCreateArgsWithOptions(t *testing.T) {
	got, err := NetworkCreateArgs(NetworkCreateOptions{
		Name: "backend", Driver: "bridge", Subnet: "10.20.0.0/16", Gateway: "10.20.0.1",
		IPRange: "10.20.5.0/24", Internal: true, Attachable: true, IPv6: true,
		Labels: map[string]string{"env": "dev"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"network", "create", "--driver", "bridge", "--subnet", "10.20.0.0/16", "--gateway", "10.20.0.1",
		"--ip-range", "10.20.5.0/24", "--internal", "--attachable", "--ipv6", "--label", "env=dev", "--", "backend"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %q\nwant %q", got, want)
	}
}

func TestVolumeAndNetworkCreateRefuseBadInput(t *testing.T) {
	badVolumes := []VolumeCreateOptions{
		{Name: ""},
		{Name: "-rm"},
		{Name: "a b"},
		{Name: "ok", Driver: "--privileged"},
		{Name: "ok", Options: map[string]string{"": "x"}},
		{Name: "ok", Options: map[string]string{"a=b": "x"}},
		{Name: "ok", Labels: map[string]string{"bad key": "x"}},
	}
	for _, o := range badVolumes {
		if _, err := VolumeCreateArgs(o); err == nil {
			t.Errorf("VolumeCreateArgs accepted %+v", o)
		}
	}
	badNetworks := []NetworkCreateOptions{
		{Name: "-x"},
		{Name: "n", Subnet: "10.0.0.0"},
		{Name: "n", Gateway: "not-an-ip"},
		{Name: "n", IPRange: "300.0.0.0/8"},
		{Name: "n", Driver: "-d"},
	}
	for _, o := range badNetworks {
		if _, err := NetworkCreateArgs(o); err == nil {
			t.Errorf("NetworkCreateArgs accepted %+v", o)
		}
	}
}

func TestVolumeBrowseArgsStayInsideTheVolume(t *testing.T) {
	got, err := VolumeBrowseArgs("data", "../../etc")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"run", "--rm", "--network", "none", "-v", "data:/v:ro", VolumeHelperImage, "ls", "-1Ap", "--", "/v/etc"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args = %q\nwant %q", got, want)
	}
	root, _ := VolumeBrowseArgs("data", "")
	if root[len(root)-1] != "/v" {
		t.Fatalf("root browse path = %q, want /v", root[len(root)-1])
	}
	if _, err := VolumeBrowseArgs("bad:name", "/"); err == nil {
		t.Fatalf("a volume name with a colon must be refused (it would change the mount)")
	}
}

func TestParseVolumeListing(t *testing.T) {
	got := ParseVolumeListing("conf/\n.hidden\napp.log\n\nsub dir/\n")
	want := []VolumeEntry{{Name: "conf", Dir: true}, {Name: ".hidden"}, {Name: "app.log"}, {Name: "sub dir", Dir: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entries = %+v, want %+v", got, want)
	}
}

func TestVolumeBackupRestoreCloneArgs(t *testing.T) {
	backup, err := VolumeBackupArgs("data", "/home/me/backups", "data-2026.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	wantBackup := []string{"run", "--rm", "--network", "none", "-v", "data:/v:ro", "-v", "/home/me/backups:/backup",
		VolumeHelperImage, "tar", "-czf", "/backup/data-2026.tar.gz", "-C", "/v", "."}
	if !reflect.DeepEqual(backup, wantBackup) {
		t.Fatalf("backup = %q\nwant %q", backup, wantBackup)
	}
	restore, err := VolumeRestoreArgs("data", "/home/me/backups", "data-2026.tar.gz")
	if err != nil {
		t.Fatal(err)
	}
	wantRestore := []string{"run", "--rm", "--network", "none", "-v", "data:/v", "-v", "/home/me/backups:/backup:ro",
		VolumeHelperImage, "tar", "-xzf", "/backup/data-2026.tar.gz", "-C", "/v"}
	if !reflect.DeepEqual(restore, wantRestore) {
		t.Fatalf("restore = %q\nwant %q", restore, wantRestore)
	}
	clone, err := VolumeCloneArgs("data", "data-copy")
	if err != nil {
		t.Fatal(err)
	}
	wantClone := []string{"run", "--rm", "--network", "none", "-v", "data:/from:ro", "-v", "data-copy:/to",
		VolumeHelperImage, "cp", "-a", "/from/.", "/to/"}
	if !reflect.DeepEqual(clone, wantClone) {
		t.Fatalf("clone = %q\nwant %q", clone, wantClone)
	}
	for name, call := range map[string]func() error{
		"relative host dir": func() error { _, e := VolumeBackupArgs("data", "backups", "x.tgz"); return e },
		"host dir colon":    func() error { _, e := VolumeBackupArgs("data", "/a:b", "x.tgz"); return e },
		"file with slash":   func() error { _, e := VolumeRestoreArgs("data", "/b", "../x.tgz"); return e },
		"file dash":         func() error { _, e := VolumeRestoreArgs("data", "/b", "-x.tgz"); return e },
		"clone onto itself": func() error { _, e := VolumeCloneArgs("data", "data"); return e },
		"clone bad target":  func() error { _, e := VolumeCloneArgs("data", "a:b"); return e },
	} {
		if err := call(); err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.Contains(err.Error(), "/home") {
			t.Errorf("%s: error leaks a path: %v", name, err)
		}
	}
}
