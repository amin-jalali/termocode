package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/containers"
)

func TestContainersVolumesPaletteIsClassified(t *testing.T) {
	ids := []string{"containers-volume-browse", "containers-volume-backup", "containers-volume-restore",
		"containers-volume-clone", "containers-volume-create-options", "containers-network-create-options"}
	have := map[string]bool{}
	for _, it := range containersExtraPaletteItems() {
		have[it.ID] = true
		if _, ok := aiActionPolicies[it.ID]; !ok {
			t.Fatalf("%s has no AI policy", it.ID)
		}
	}
	for _, id := range ids {
		if !have[id] {
			t.Fatalf("%s is not in the palette", id)
		}
	}
	for _, id := range []string{"containers-volume-restore", "containers-volume-clone"} {
		if aiActionPolicies[id].Gate != aiGateNever {
			t.Fatalf("%s must be closed to the AI", id)
		}
	}
}

func TestContainersVolumeCreateLine(t *testing.T) {
	o, err := volumeCreateLine("data driver=local opt.type=tmpfs opt.device=tmpfs label.team=web")
	if err != nil {
		t.Fatal(err)
	}
	if o.Name != "data" || o.Driver != "local" || o.Options["type"] != "tmpfs" || o.Options["device"] != "tmpfs" || o.Labels["team"] != "web" {
		t.Fatalf("got %+v", o)
	}
	for _, bad := range []string{"", "data size=1G", "data opt.=x"} {
		if _, err := volumeCreateLine(bad); err == nil {
			t.Fatalf("%q: want an error", bad)
		}
	}
}

func TestContainersNetworkCreateLine(t *testing.T) {
	o, err := networkCreateLine("backend driver=bridge subnet=10.5.0.0/16 gateway=10.5.0.1 ip-range=10.5.1.0/24 internal attachable ipv6 label.app=x")
	if err != nil {
		t.Fatal(err)
	}
	if o.Name != "backend" || o.Driver != "bridge" || o.Subnet != "10.5.0.0/16" || o.Gateway != "10.5.0.1" || o.IPRange != "10.5.1.0/24" ||
		!o.Internal || !o.Attachable || !o.IPv6 || o.Labels["app"] != "x" {
		t.Fatalf("got %+v", o)
	}
	if _, err := networkCreateLine("backend mtu=1400"); err == nil {
		t.Fatal("unknown option: want an error")
	}
}

func TestContainersVolumeBrowseText(t *testing.T) {
	text := volumeListingText("data", "/conf", containers.ParseVolumeListing("app/\nmain.cfg\n"))
	for _, want := range []string{"data:/conf", "app/", "main.cfg", "1 folder, 1 file"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q:\n%s", want, text)
		}
	}
	if got := volumeListingText("data", "/", nil); !strings.Contains(got, "Empty folder") {
		t.Fatalf("empty: %q", got)
	}
}

func TestContainersVolumeBackupName(t *testing.T) {
	at := time.Date(2026, 11, 7, 14, 5, 9, 0, time.UTC)
	if got := volumeBackupName("data", at); got != "data-20261107-140509.tar.gz" {
		t.Fatalf("got %q", got)
	}
}

func TestContainersVolumeRestorePath(t *testing.T) {
	dir, file, err := splitArchivePath("/work", "backups/data.tar.gz")
	if err != nil || dir != "/work/backups" || file != "data.tar.gz" {
		t.Fatalf("got %q %q %v", dir, file, err)
	}
	if _, _, err := splitArchivePath("/work", ""); err == nil {
		t.Fatal("empty path: want an error")
	}
}
