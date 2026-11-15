package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/containers"
)

func ctrVolumes(t *testing.T, m *Model, id string) {
	t.Helper()
	t.Cleanup(func() { m.services.stopHostWatch(); m.services.stopWatch() })
	if _, ok := m.dispatchContainersVolumes(id); !ok {
		t.Fatalf("%s not handled", id)
	}
}

// Each volume action needs a volume row and opens its prompt; browse,
// back up, restore and clone run on a scripted engine (restore and clone
// behind a card).
func TestCovContainerVolumeActions(t *testing.T) {
	m := ctrStorageModel(t)
	x := ctrScripted(t, m)
	x.on("run --rm", "data/\nnotes.txt\n")
	if _, ok := m.dispatchContainersVolumes("dance"); ok {
		t.Fatal("unknown id")
	}
	if len(containersVolumesPaletteItems()) != 6 {
		t.Fatal("palette rows")
	}
	m.services.cursor = 0
	ctrVolumes(t, m, "containers-volume-browse")
	if !strings.Contains(ctrToast(m), "Select a volume row first") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	ctrStorageRow(t, m, hostVolume, "termocode_db")
	for id, kind := range map[string]promptKindEnum{
		"containers-volume-browse": promptKindVolumeBrowse, "containers-volume-backup": promptKindVolumeBackup,
		"containers-volume-restore": promptKindVolumeRestore, "containers-volume-clone": promptKindVolumeClone,
		"containers-volume-create-options": promptKindVolumeCreate, "containers-network-create-options": promptKindNetworkCreate,
	} {
		m.promptOpen = false
		ctrVolumes(t, m, id)
		if !m.promptOpen || m.promptKind != kind {
			t.Fatalf("%s: no prompt", id)
		}
	}
	// Browse lists the folder.
	m.services.pendingArchive = "termocode_db"
	m.promptKind = promptKindVolumeBrowse
	cmd, _ := m.handleContainersExtraPrompt("/")
	page := ctrRun[containersPageMsg](t, cmd)
	if page.err != nil || !strings.Contains(page.text, "data/") || !strings.Contains(page.text, "1 folder, 1 file") {
		t.Fatalf("browse %+v", page)
	}
	if got := volumeListingText("v", "x", nil); !strings.Contains(got, "Empty folder.") {
		t.Fatal(got)
	}
	_ = m.volumeBrowsePage("-bad", "/")
	if !strings.Contains(ctrToast(m), "Not shown") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	// Back up runs; a relative folder is under the workspace.
	m.services.pendingArchive = "termocode_db"
	done := ctrRun[containersExtraDoneMsg](t, m.handleVolumePrompt(promptKindVolumeBackup, "backups"))
	if done.err != nil || !strings.Contains(done.label, "back up termocode_db") {
		t.Fatalf("%+v", done)
	}
	// Restore asks; an empty file name does not run.
	m.services.pendingArchive = "termocode_db"
	_ = m.handleVolumePrompt(promptKindVolumeRestore, "")
	if !strings.Contains(ctrToast(m), "a backup file is needed") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	m.services.pendingArchive = "termocode_db"
	_ = m.handleVolumePrompt(promptKindVolumeRestore, "db.tar.gz")
	if !m.confirmOpen || !strings.Contains(m.confirm.View(), "Restore volume?") {
		t.Fatal("no restore card")
	}
	// Clone: into a new volume, and into one that exists.
	m.services.pendingArchive = "termocode_db"
	_ = m.handleVolumePrompt(promptKindVolumeClone, "copy")
	if !strings.Contains(cardText(m), "copies every file") {
		t.Fatalf("clone card %s", cardText(m))
	}
	m.services.volumeRows = append(m.services.volumeRows, containers.Volume{Name: "copy"})
	m.services.pendingArchive = "termocode_db"
	_ = m.handleVolumePrompt(promptKindVolumeClone, "copy")
	if !strings.Contains(cardText(m), "Volume copy exists") {
		t.Fatalf("clone card %s", cardText(m))
	}
	// Create lines.
	done = ctrRun[containersExtraDoneMsg](t, m.handleVolumePrompt(promptKindVolumeCreate, "data driver=local opt.type=tmpfs label.app=x"))
	if done.err != nil || x.count("volume create") != 1 {
		t.Fatalf("%+v %v", done, x.calls)
	}
	done = ctrRun[containersExtraDoneMsg](t, m.handleVolumePrompt(promptKindNetworkCreate, "net driver=bridge subnet=10.9.0.0/24 gateway=10.9.0.1 ip-range=10.9.0.0/25 internal attachable ipv6 label.a=b"))
	if done.err != nil || x.count("network create") != 1 {
		t.Fatalf("%+v %v", done, x.calls)
	}
	_ = m.handleVolumePrompt(promptKindVolumeCreate, "driver=x")
	if !strings.Contains(ctrToast(m), "start with a name") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	x.fail("run --rm", "no such volume")
	m.services.pendingArchive = "termocode_db"
	page = ctrRun[containersPageMsg](t, m.handleVolumePrompt(promptKindVolumeBrowse, "/"))
	if page.err == nil {
		t.Fatal("no error")
	}
	m.services.containerEngine.Reachable = false
	_ = m.volumeBrowsePage("v", "/")
	if !strings.Contains(ctrToast(m), "No container engine") {
		t.Fatalf("toast %s", ctrToast(m))
	}
}

// The create lines: every option and each mistake.
func TestCovContainerVolumeCreateLines(t *testing.T) {
	o, err := volumeCreateLine("data driver=local opt.type=tmpfs label.app=x")
	if err != nil || o.Driver != "local" || o.Options["type"] != "tmpfs" || o.Labels["app"] != "x" {
		t.Fatalf("%+v %v", o, err)
	}
	for _, bad := range []string{"", "data plain", "data size=1", `data "open`} {
		if _, err := volumeCreateLine(bad); err == nil {
			t.Errorf("volume %q accepted", bad)
		}
	}
	n, err := networkCreateLine("net driver=bridge subnet=10.9.0.0/24 gateway=10.9.0.1 ip-range=10.9.0.0/25 internal attachable ipv6 label.a=b")
	if err != nil || !n.Internal || !n.Attachable || !n.IPv6 || n.Subnet == "" || n.Gateway == "" || n.IPRange == "" || n.Labels["a"] != "b" {
		t.Fatalf("%+v %v", n, err)
	}
	for _, bad := range []string{"", "net fast", "net mtu=1", `net "x`, "net subnet=notanip"} {
		if _, err := networkCreateLine(bad); err == nil {
			t.Errorf("network %q accepted", bad)
		}
	}
	if countWord(1, "file") != "1 file" || countWord(2, "file") != "2 files" {
		t.Fatal("countWord")
	}
	if got := volumeBackupName("db", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)); got != "db-20260102-030405.tar.gz" {
		t.Fatal(got)
	}
}
