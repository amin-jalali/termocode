package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
)

// ctrStorageRow puts the cursor on the network or volume row name.
func ctrStorageRow(t *testing.T, m *Model, kind hostKind, name string) svcRow {
	t.Helper()
	return selectRow(t, m.services, func(r svcRow) bool {
		return r.kind == svcRowHostItem && r.host == kind && r.hostID == name
	})
}

func ctrSectionRow(t *testing.T, m *Model, sec string) {
	t.Helper()
	selectRow(t, m.services, func(r svcRow) bool { return r.kind == svcRowSection && r.section == hostSection(sec) })
}

func ctrStorageModel(t *testing.T) *Model {
	t.Helper()
	m, f := ctrModel(t)
	f.Set(t, "inspect.json", `[{"Name":"termocode_default","Containers":{"a":{"Name":"/termocode-api-1"},"b":{"Name":""}}}]`)
	f.Set(t, "ps.json", `{"ID":"c1","Names":"termocode-postgres-1","State":"running"}`)
	for _, s := range []string{"networks", "volumes"} {
		m.services.sections[s] = true
		m.services.collapsed[hostSection(s)] = false
	}
	m.services.touch()
	return m
}

// n on NETWORKS/VOLUMES asks a name; a bad one toasts, a good one runs
// network/volume create on the fake docker.
func TestCovContainerStorageCreate(t *testing.T) {
	m := ctrStorageModel(t)
	for _, sec := range []string{"networks", "volumes"} {
		ctrSectionRow(t, m, sec)
		_ = ctrKey(m, "n")
		if !m.promptOpen || m.promptKind != promptKindContainerStorageName || m.pendingHostTarget != sec {
			t.Fatalf("%s: no prompt", sec)
		}
		m.promptOpen = false
	}
	_ = m.storageCreate("-x", "network")
	if !strings.Contains(ctrToast(m), "cannot start with a dash") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.storageCreate("a b", "network")
	if !strings.Contains(ctrToast(m), "without spaces") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	done := ctrRun[hostActionDoneMsg](t, m.storageCreate(" web ", "network"))
	if done.operation != "create-network" || done.section != "networks" || done.target != "web" || done.err != nil {
		t.Fatalf("%+v", done)
	}
	done = ctrRun[hostActionDoneMsg](t, m.storageCreate("data", "volume"))
	if done.operation != "create-volume" || done.section != "volumes" {
		t.Fatalf("%+v", done)
	}
}

// Space on a network row unfolds it and lists the containers on it
// (from inspect); a volume lists its users from ps; errors show inline.
func TestCovContainerStorageUsersUnfold(t *testing.T) {
	m := ctrStorageModel(t)
	r := ctrStorageRow(t, m, hostNetwork, "termocode_default")
	if g := storageFoldGlyph(m.services, r); g == "" {
		t.Fatal("no glyph")
	}
	cmd := ctrKey(m, " ")
	if m.services.storageInspectState[storageInspectKey(r)] != "loading" || !strings.Contains(svcHintText(m.services), "Checking containers") {
		t.Fatalf("not loading: %s", svcHintText(m.services))
	}
	msg := ctrRun[storageInspectMsg](t, cmd)
	_ = m.onStorageInspect(msg)
	if got := svcHintText(m.services); !strings.Contains(got, "Used by termocode-api-1") {
		t.Fatalf("users: %s", got)
	}
	if storageFoldGlyph(m.services, r) == storageFoldGlyph(m.services, svcRow{host: hostNetwork, hostID: "other"}) {
		t.Fatal("open and folded glyphs are the same")
	}
	// A second unfold while loaded does not inspect again.
	if m.inspectStorageUsers(r) != nil {
		t.Fatal("inspected twice")
	}
	vr := ctrStorageRow(t, m, hostVolume, "termocode_db")
	msg = ctrRun[storageInspectMsg](t, ctrKey(m, " "))
	_ = m.onStorageInspect(msg)
	if got := svcHintText(m.services); !strings.Contains(got, "Used by termocode-postgres-1") {
		t.Fatalf("volume users: %s", got)
	}
	// Fold, then an error and an empty answer.
	_ = ctrKey(m, " ")
	if strings.Contains(svcHintText(m.services), "postgres-1") {
		t.Fatal("folded but listed")
	}
	m.services.collapsed[storageInspectKey(vr)] = false
	_ = m.onStorageInspect(storageInspectMsg{key: storageInspectKey(vr), gen: m.services.containerGen, row: vr, err: errors.New("x")})
	if !strings.Contains(svcHintText(m.services), "Could not check container use") {
		t.Fatalf("error: %s", svcHintText(m.services))
	}
	_ = m.onStorageInspect(storageInspectMsg{key: storageInspectKey(vr), gen: m.services.containerGen, row: vr})
	if !strings.Contains(svcHintText(m.services), "No containers use this") {
		t.Fatalf("empty: %s", svcHintText(m.services))
	}
	// A stale answer is dropped.
	_ = m.onStorageInspect(storageInspectMsg{key: storageInspectKey(vr), gen: m.services.containerGen + 1, row: vr, users: []string{"late"}})
	if strings.Contains(svcHintText(m.services), "late") {
		t.Fatal("stale answer applied")
	}
	m.services.clearStorageInspect("volume")
	if _, ok := m.services.storageInspectState[storageInspectKey(vr)]; ok {
		t.Fatal("not cleared")
	}
}

// svcHintText is the text of every hint row.
func svcHintText(st *servicesState) string {
	var out []string
	for _, r := range st.rows() {
		if r.kind == svcRowHint {
			out = append(out, r.text)
		}
	}
	return strings.Join(out, "\n")
}

// D on a network or volume checks its users first, then shows the
// Cancel-first remove card; a volume in use only says so; built-in
// networks cannot go.
func TestCovContainerStorageRemove(t *testing.T) {
	m := ctrStorageModel(t)
	ctrStorageRow(t, m, hostNetwork, "bridge")
	_ = m.onStorageInspect(ctrRun[storageInspectMsg](t, ctrKey(m, "D")))
	if m.confirmOpen || !strings.Contains(ctrToast(m), "bridge cannot be removed") {
		t.Fatalf("built-in: %s", ctrToast(m))
	}
	r := ctrStorageRow(t, m, hostNetwork, "termocode_default")
	_ = m.onStorageInspect(ctrRun[storageInspectMsg](t, ctrKey(m, "D")))
	if !m.confirmOpen || m.pendingHostOperation != "remove-storage" || m.pendingHostTarget != "networks" ||
		!strings.Contains(cardText(m), "Containers using it: termocode-api-1") || !strings.Contains(cardText(m), "network rm termocode_default") {
		t.Fatalf("network card: %s", cardText(m))
	}
	m.confirmOpen = false
	// An error while checking.
	m.services.storageInspectState[storageInspectKey(r)] = "error:x"
	_ = ctrKey(m, "D")
	if m.confirmOpen || !strings.Contains(ctrToast(m), "Could not check container use") {
		t.Fatalf("error: %s", ctrToast(m))
	}
	_ = m.onStorageInspect(storageInspectMsg{key: storageInspectKey(r), gen: m.services.containerGen, row: r, remove: true, err: errors.New("gone")})
	if m.confirmOpen {
		t.Fatal("card after a failed check")
	}
	// A volume in use: only a Close card.
	ctrStorageRow(t, m, hostVolume, "termocode_db")
	_ = m.onStorageInspect(ctrRun[storageInspectMsg](t, ctrKey(m, "D")))
	if !m.confirmOpen || m.pendingHostOperation != "" || !strings.Contains(cardText(m), "Volume is in use") {
		t.Fatalf("in use: %s", cardText(m))
	}
	m.confirmOpen = false
	// Free volume: the remove card.
	m.services.volumeRows = append(m.services.volumeRows, containers.Volume{Name: "spare", Driver: "local"})
	m.services.touch()
	vr := ctrStorageRow(t, m, hostVolume, "spare")
	m.services.storageInspectState[storageInspectKey(vr)] = "done"
	_ = ctrKey(m, "D")
	if !m.confirmOpen || m.pendingHostTarget != "volumes" || !strings.Contains(cardText(m), "volume rm spare") {
		t.Fatalf("free volume: %s", cardText(m))
	}
}

// U on VOLUMES reads `system df -v` and fills the sizes; errors toast.
func TestCovContainerStorageDiskUsage(t *testing.T) {
	m := ctrStorageModel(t)
	f := hostFake(t)
	f.Set(t, "df.txt", "VOLUME NAME    LINKS   SIZE\ntermocode_db   1       42MB\nshort\n")
	ctrSectionRow(t, m, "volumes")
	msg := ctrRun[diskUsageMsg](t, ctrKey(m, "U"))
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	_ = m.onDiskUsage(msg)
	if m.services.volumeRows[0].Size != "42MB" {
		t.Fatalf("size %+v", m.services.volumeRows[0])
	}
	_ = m.onDiskUsage(diskUsageMsg{err: errors.New("no daemon")})
	if !strings.Contains(ctrToast(m), "Cannot read disk usage") {
		t.Fatalf("toast %s", ctrToast(m))
	}
}

// Prune preview, the network inspect parser and the name wrap.
func TestCovContainerStorageHelpers(t *testing.T) {
	m := ctrStorageModel(t)
	_ = m.onStoragePrunePreview(storagePrunePreviewMsg{section: "volumes", err: errors.New("x")})
	if !strings.Contains(ctrToast(m), "Cannot check dangling volumes") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.onStoragePrunePreview(storagePrunePreviewMsg{section: "volumes", volumes: []string{"old"}})
	if !m.confirmOpen {
		t.Fatal("no prune card")
	}
	m.confirmOpen = false
	m.showStoragePrune("volumes", nil)
	if !m.confirmOpen {
		t.Fatal("no prune card")
	}
	m.confirmOpen = false
	_ = m.askStoragePrune("networks")
	for _, bad := range []any{nil, []any{}, []any{"x"}} {
		if _, err := networkInspectUsers(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	if got := wrapStorageName("abcdefgh", 3); got != "abc\ndef\ngh" {
		t.Fatalf("wrap %q", got)
	}
	if wrapStorageName("ab", 0) != "ab" || wrapStorageName("ab", 5) != "ab" {
		t.Fatal("no-wrap")
	}
	if !isAnonymousVolume(strings.Repeat("a", 64)) || isAnonymousVolume("data") {
		t.Fatal("anonymous volume")
	}
	snap := storageSnapshotUsers([]containers.Container{{ID: "x", Names: []string{"a"}, Networks: []string{"n1"}, Mounts: []string{"v1"}}}, svcRow{host: hostVolume, hostID: "v1"})
	if len(snap) != 1 {
		t.Fatalf("snapshot %v", snap)
	}
}
