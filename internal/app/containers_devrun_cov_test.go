package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ctrDevrunModel is the seeded view with a workspace folder as cwd and
// the config under a temp XDG_CONFIG_HOME.
func ctrDevrunModel(t *testing.T) (*Model, string) {
	t.Helper()
	m, _ := ctrModel(t)
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	m.services.root = root
	return m, root
}

// ctrEdit opens path in the headless editor (current buffer).
func ctrEdit(t *testing.T, m *Model, path string) {
	t.Helper()
	if err := m.nvim.ExecLuaArgs(`vim.cmd('edit ' .. vim.fn.fnameescape(...))`, path); err != nil {
		t.Fatal(err)
	}
}

// Run at Cursor: without an editor or a known file it says so; on a
// Dockerfile the card shows the build; the lens click opens the same
// card only on a marked line; Run starts the terminal tab.
func TestCovContainerDevrunRunAtCursor(t *testing.T) {
	m, root := ctrDevrunModel(t)
	_ = m.runAtCursor()
	if !strings.Contains(ctrToast(m), "Open a Dockerfile or compose file first") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	if notifyHandlers["termocode_docker_lens"](m, []any{int64(1), int64(1)}) != nil {
		t.Fatal("lens without an editor")
	}
	_ = notifyHandlers["termocode_docker_buf"](m, []any{int64(1)})
	// Run without an editor: the tab cannot open.
	_ = m.askRunTarget(filepath.Join(root, "Dockerfile"), []string{"FROM alpine"}, 1, false)
	if cmd, ok := m.runDevrunHostAction(hostOpRunCommand); !ok || cmd == nil || !strings.Contains(ctrToast(m), "Cannot start the command") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	ctrNvim(t, m)
	df := filepath.Join(root, "Dockerfile")
	if err := os.WriteFile(df, []byte("FROM golang AS build\nRUN go build\nFROM alpine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(root, "notes.txt")
	_ = os.WriteFile(notes, []byte("hi\n"), 0o644)
	ctrEdit(t, m, notes)
	_ = m.runAtCursor()
	if !strings.Contains(ctrToast(m), "Not a Dockerfile or compose file") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	ctrEdit(t, m, df)
	m.dockerLensRefresh(0)
	_ = m.runAtCursor()
	if !m.confirmOpen || !strings.Contains(cardText(m), "build") || m.pendingHostOperation != hostOpRunCommand {
		t.Fatalf("card %s", cardText(m))
	}
	m.confirmOpen = false
	_ = notifyHandlers["termocode_docker_lens"](m, []any{int64(0), int64(3)})
	if !m.confirmOpen {
		t.Fatal("lens click: no card")
	}
	if cmd, ok := m.runDevrunHostAction(hostOpRunCommand); !ok || cmd != nil {
		t.Fatalf("run: %s", ctrToast(m))
	}
	if len(m.terminalTabs) != 1 {
		t.Fatalf("tabs %+v", m.terminalTabs)
	}
	// A compose service is a Start card; no engine says so.
	compose := []string{"services:", "  api:", "    build: ."}
	_ = m.askRunTarget(filepath.Join(root, "compose.yaml"), compose, 2, false)
	if !strings.Contains(m.confirm.View(), "Start service api?") {
		t.Fatalf("card %s", m.confirm.View())
	}
	// Off a mark: the exact (click) form does nothing, the cursor form
	// takes the first target.
	df2 := []string{"# base", "FROM alpine"}
	if m.askRunTarget(df, df2, 1, true) != nil {
		t.Fatal("exact off a mark")
	}
	m.confirmOpen = false
	_ = m.askRunTarget(df, df2, 1, false)
	if !m.confirmOpen {
		t.Fatal("cursor off a mark: no card")
	}
	if rows := dockerLensRows(filepath.Join(root, "compose.yaml"), compose); len(rows) != 1 || !strings.Contains(rows[0]["lens"].([][]string)[0][0], "up api") {
		t.Fatalf("compose lens %v", rows)
	}
	m.dockerLensRefresh(99999)
	m.services.containerEngine.Binary = ""
	_ = m.askRunTarget(df, []string{"FROM a"}, 9, false)
	if !strings.Contains(ctrToast(m), "No container engine found") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	if _, ok := m.runDevrunHostAction("other"); ok {
		t.Fatal("handled another operation")
	}
}

// Add Docker Files: the picker puts the detected template first and
// warns about unreadable user templates; the card lists what is created
// and kept; Create writes them and opens the Dockerfile.
func TestCovContainerDevrunScaffold(t *testing.T) {
	m, root := ctrDevrunModel(t)
	_ = os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x\n"), 0o644)
	dir, _ := configDir()
	_ = os.MkdirAll(filepath.Join(dir, devrunTemplateDir, "mine"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, devrunTemplateDir, "mine", "Dockerfile"), []byte("FROM mine\n"), 0o644)
	_ = m.askScaffold()
	if !m.pickerOpen || !strings.Contains(m.picker.View(), "fits this folder") || !strings.Contains(m.picker.View(), "your template") {
		t.Fatalf("picker:\n%s", m.picker.View())
	}
	_ = m.handleContainersPickerSelect("ctr:scaffold:nope")
	if !strings.Contains(ctrToast(m), "Template not found") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = os.WriteFile(filepath.Join(root, ".dockerignore"), []byte("x\n"), 0o644)
	_ = m.handleContainersPickerSelect("ctr:scaffold:go")
	if !m.confirmOpen || m.pendingHostOperation != hostOpScaffold || !strings.Contains(cardText(m), "Kept as they are") {
		t.Fatalf("card %s", cardText(m))
	}
	ctrNvim(t, m)
	if _, ok := m.runDevrunHostAction(hostOpScaffold); !ok || !strings.Contains(ctrToast(m), "Added") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	if _, err := os.Stat(filepath.Join(root, "Dockerfile")); err != nil {
		t.Fatal("no Dockerfile")
	}
	_ = m.askScaffoldTemplate("go")
	if !strings.Contains(ctrToast(m), "These files already exist") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	// A plan whose files appeared meanwhile writes nothing.
	m.confirmOpen = false
	_ = m.askScaffoldTemplate("custom:mine")
	if m.confirmOpen {
		t.Fatalf("custom: %s", ctrToast(m))
	}
	_ = os.Remove(filepath.Join(root, "Dockerfile"))
	_ = m.askScaffoldTemplate("custom:mine")
	_ = os.WriteFile(filepath.Join(root, "Dockerfile"), []byte("FROM x\n"), 0o644)
	if _, ok := m.runDevrunHostAction(hostOpScaffold); !ok || !strings.Contains(ctrToast(m), "Nothing written") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	// A plan that cannot be written.
	_ = os.Remove(filepath.Join(root, "Dockerfile"))
	_ = m.askScaffoldTemplate("custom:mine")
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
	if _, ok := m.runDevrunHostAction(hostOpScaffold); !ok || !strings.Contains(ctrToast(m), "Cannot write the docker files") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	// An unreadable template folder: the picker still opens and warns.
	_ = os.RemoveAll(filepath.Join(dir, devrunTemplateDir))
	_ = os.WriteFile(filepath.Join(dir, devrunTemplateDir), []byte("not a dir"), 0o644)
	_ = m.askScaffold()
	if !strings.Contains(ctrToast(m), "Some of your templates could not be read") {
		t.Fatalf("toast %s", ctrToast(m))
	}
}

// Add Compose Service: a new compose.yaml, one more service in an
// existing file, a duplicate and an unknown id; Add writes the file.
func TestCovContainerDevrunComposeSnippet(t *testing.T) {
	m, root := ctrDevrunModel(t)
	_ = m.askComposeSnippet()
	if !m.pickerOpen || !strings.Contains(m.picker.View(), "PostgreSQL") {
		t.Fatal("no snippet picker")
	}
	_ = m.handleContainersPickerSelect("ctr:snippet:nope")
	if !strings.Contains(ctrToast(m), "Service template not found") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.handleContainersPickerSelect("ctr:snippet:redis")
	if !m.confirmOpen || !strings.Contains(cardText(m), "Creates ") {
		t.Fatalf("card %s", cardText(m))
	}
	if _, ok := m.runDevrunHostAction(hostOpComposeSvc); !ok || !strings.Contains(ctrToast(m), "Service added") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	b, _ := os.ReadFile(filepath.Join(root, "compose.yaml"))
	if !strings.Contains(string(b), "redis:7") {
		t.Fatalf("compose %s", b)
	}
	_ = m.askComposeSnippetAdd("postgres")
	if !strings.Contains(cardText(m), "Adds to ") {
		t.Fatalf("card %s", cardText(m))
	}
	_ = m.askComposeSnippetAdd("redis")
	if !strings.Contains(ctrToast(m), "Service not added") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	m.services.devrun = devrunPending{compose: filepath.Join(root, "no", "dir", "compose.yaml"), text: "x"}
	if _, ok := m.runDevrunHostAction(hostOpComposeSvc); !ok || !strings.Contains(ctrToast(m), "Cannot write the compose file") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	if err := os.Chmod(filepath.Join(root, "compose.yaml"), 0); err != nil {
		t.Fatal(err)
	}
	_ = m.askComposeSnippetAdd("mysql")
	if !strings.Contains(ctrToast(m), "Cannot read the compose file") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	if m.handleContainersPickerSelect("ctr:dance:x") != nil {
		t.Fatal("unknown")
	}
	_ = m.handleContainersPickerSelect("ctr:custom:nope")
	if len(devrunPaletteItems()) != 4 {
		t.Fatal("palette rows")
	}
}

// Open Docker Templates Folder makes the folder; a config path that is a
// file says so.
func TestCovContainerDevrunTemplatesFolder(t *testing.T) {
	m, _ := ctrDevrunModel(t)
	_ = m.openTemplatesFolder()
	dir, _ := configDir()
	if fi, err := os.Stat(filepath.Join(dir, devrunTemplateDir)); err != nil || !fi.IsDir() {
		t.Fatalf("no folder: %v", err)
	}
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	_ = os.WriteFile(filepath.Join(cfg, "termocode"), []byte("file"), 0o644)
	_ = m.openTemplatesFolder()
	if !strings.Contains(ctrToast(m), "Config error") {
		t.Fatalf("toast %s", ctrToast(m))
	}
}
