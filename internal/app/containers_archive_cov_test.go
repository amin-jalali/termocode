package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
)

// ctrImageRow puts the cursor on the first image row.
func ctrImageRow(t *testing.T, m *Model) {
	t.Helper()
	m.services.sections["images"] = true
	m.services.collapsed[hostSection("images")] = false
	m.services.touch()
	selectRow(t, m.services, func(r svcRow) bool { return r.kind == svcRowHostItem && r.host == hostImage })
}

func ctrArchive(t *testing.T, m *Model, id string) {
	t.Helper()
	t.Cleanup(func() { m.services.stopHostWatch(); m.services.stopWatch() })
	if _, ok := m.dispatchContainersArchive(id); !ok {
		t.Fatalf("%s not handled", id)
	}
}

// Each archive palette action needs its row, opens its prompt, and the
// prompt runs the engine call (save/export ask before replacing a file,
// load always asks).
func TestCovContainerArchivePrompts(t *testing.T) {
	m, f := ctrModel(t)
	if _, ok := m.dispatchContainersArchive("dance"); ok {
		t.Fatal("unknown id handled")
	}
	if len(containersArchivePaletteItems()) != 8 {
		t.Fatal("palette rows")
	}
	m.services.cursor = 0
	ctrArchive(t, m, "containers-image-save")
	if !strings.Contains(ctrToast(m), "Select an image row first") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	ctrArchive(t, m, "containers-commit")
	if !strings.Contains(ctrToast(m), "Select a container row first") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	root := m.services.root
	// Save: prompt prefilled, a new file runs at once, an existing file asks.
	ctrImageRow(t, m)
	ctrArchive(t, m, "containers-image-save")
	if !m.promptOpen || m.promptKind != promptKindContainerSave || !strings.HasSuffix(m.prompt.Value(), ".tar") {
		t.Fatalf("save prompt %q", m.prompt.Value())
	}
	done := ctrRun[containersExtraDoneMsg](t, m.onArchivePrompt(promptKindContainerSave, "out.tar"))
	if done.err != nil || f.Count("save") != 1 {
		t.Fatalf("%+v %v", done, f.Calls())
	}
	_ = os.WriteFile(filepath.Join(root, "out.tar"), nil, 0o644)
	m.services.pendingArchive = "postgres:16"
	_ = m.onArchivePrompt(promptKindContainerSave, "out.tar")
	if !m.confirmOpen || !strings.Contains(m.confirm.View(), "Replace file?") {
		t.Fatal("no replace card")
	}
	_ = m.onArchivePrompt(promptKindContainerSave, "")
	if !strings.Contains(ctrToast(m), "Not run") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	// Load asks; import runs.
	ctrArchive(t, m, "containers-image-load")
	if m.promptKind != promptKindContainerLoad {
		t.Fatal("no load prompt")
	}
	m.confirmOpen = false
	_ = m.onArchivePrompt(promptKindContainerLoad, "out.tar")
	if !m.confirmOpen || !strings.Contains(m.confirm.View(), "Load images?") {
		t.Fatal("no load card")
	}
	ctrArchive(t, m, "containers-image-import")
	if m.promptKind != promptKindContainerImport {
		t.Fatal("no import prompt")
	}
	done = ctrRun[containersExtraDoneMsg](t, m.onArchivePrompt(promptKindContainerImport, "out.tar acme/root:1"))
	if done.err != nil || f.Count("import") != 1 {
		t.Fatalf("%+v %v", done, f.Calls())
	}
	// Commit and export from a container row.
	cursorOnContainer(t, m, "c5")
	ctrArchive(t, m, "containers-commit")
	if m.promptKind != promptKindContainerCommit || m.prompt.Value() != "nginx:snapshot" {
		t.Fatalf("commit prompt %q", m.prompt.Value())
	}
	done = ctrRun[containersExtraDoneMsg](t, m.onArchivePrompt(promptKindContainerCommit, "nginx:snap first try"))
	if done.err != nil || f.Count("commit") != 1 {
		t.Fatalf("%+v %v", done, f.Calls())
	}
	ctrArchive(t, m, "containers-export")
	if m.promptKind != promptKindContainerExport || m.prompt.Value() != "nginx.tar" {
		t.Fatalf("export prompt %q", m.prompt.Value())
	}
	done = ctrRun[containersExtraDoneMsg](t, m.onArchivePrompt(promptKindContainerExport, "nginx.tar"))
	if done.err != nil || f.Count("export") != 1 {
		t.Fatalf("%+v %v", done, f.Calls())
	}
	_ = os.WriteFile(filepath.Join(root, "nginx.tar"), nil, 0o644)
	m.services.pendingArchive = "c5"
	m.confirmOpen = false
	_ = m.onArchivePrompt(promptKindContainerExport, "nginx.tar")
	if !m.confirmOpen || !strings.Contains(m.confirm.View(), "Exporting replaces it") {
		t.Fatal("no export replace card")
	}
	// The prompt router.
	m.promptKind = promptKindContainerCommit
	m.services.pendingArchive = ""
	if _, ok := m.handleContainersExtraPrompt("x"); !ok {
		t.Fatal("not routed")
	}
	m.promptKind = promptKindContainerTag
	if _, ok := m.handleContainersExtraPrompt("x"); ok {
		t.Fatal("routed another prompt")
	}
}

// Diff, layers and used-by open pages; a failing engine toasts; no
// engine says so.
func TestCovContainerArchivePages(t *testing.T) {
	m, f := ctrModel(t)
	cursorOnContainer(t, m, "c5")
	ctrArchive(t, m, "containers-diff")
	f.Set(t, "history.json", `{"ID":"sha256:1","CreatedBy":"/bin/sh -c apk add x","Size":"12MB"}
{"ID":"sha256:2","CreatedBy":"ENV A=1","Size":"0B"}
{"ID":"sha256:3","CreatedBy":"LABEL x=y","Size":"0B"}`)
	page := ctrRun[containersPageMsg](t, m.containerDiffPage("c5", "nginx"))
	if page.err != nil || !strings.Contains(page.text, "No changes.") {
		t.Fatalf("diff %+v", page)
	}
	ctrImageRow(t, m)
	ctrArchive(t, m, "containers-image-layers")
	page = ctrRun[containersPageMsg](t, m.imageLayersPage("postgres:16"))
	if page.err != nil || !strings.Contains(page.text, "Layers of postgres:16") || !strings.Contains(page.text, "2 empty layers") {
		t.Fatalf("layers %+v", page)
	}
	ctrArchive(t, m, "containers-image-used-by")
	if got := usedByText("x", nil); !strings.Contains(got, "No container uses x") {
		t.Fatal(got)
	}
	if got := usedByText("postgres:16", []containers.Container{{ID: "c1", Names: []string{"/pg"}, State: "running"}}); !strings.Contains(got, "1 containers use postgres:16") || !strings.Contains(got, "pg") {
		t.Fatal(got)
	}
	f.Set(t, "all.err", "daemon gone")
	page = ctrRun[containersPageMsg](t, m.containerDiffPage("c5", "nginx"))
	if page.err == nil {
		t.Fatal("no error")
	}
	if _ = m.onContainersPage(page); !strings.Contains(ctrToast(m), "Cannot open diff: nginx") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.containerDiffPage("-bad", "x")
	if !strings.Contains(ctrToast(m), "Not shown") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	m.services.containerEngine.Reachable = false
	_ = m.containerDiffPage("c5", "nginx")
	_ = m.imageLayersPage("x")
	if !strings.Contains(ctrToast(m), "No container engine") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	// Text helpers.
	txt := containerDiffText("n", []containers.DiffEntry{{Kind: "added", Path: "/a"}, {Kind: "deleted", Path: "/b"}, {Kind: "changed", Path: "/c"}})
	for _, want := range []string{"3 paths", "+ /a", "- /b", "~ /c"} {
		if !strings.Contains(txt, want) {
			t.Errorf("diff text lacks %q", want)
		}
	}
	long := containers.LayerReport{Layers: []containers.Layer{{CreatedBy: strings.Repeat("x", 150), Bytes: 10}}, Total: 10, Empty: 1}
	if got := layerSizesText("r", long); !strings.Contains(got, "…") || !strings.Contains(got, "1 empty layer") {
		t.Fatal(got)
	}
	if got := layerSizesText("r", containers.LayerReport{}); !strings.Contains(got, "No layer adds bytes") {
		t.Fatal(got)
	}
	if archiveContainerName(containers.Container{ID: "abcdef0123456789"}) != shortImageID("abcdef0123456789") {
		t.Fatal("name fallback")
	}
	if archivePath("/w", " /abs/x.tar ") != "/abs/x.tar" || archivePath("/w", "") != "" {
		t.Fatal("archive path")
	}
}
