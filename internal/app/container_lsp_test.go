package app

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// RC-6 (plan RC phase 5): language servers of a container workspace.

var testSpecs = []lspServerSpec{
	{Name: "gopls", Cmd: []string{"gopls"}},
	{Name: "ts_ls", Cmd: []string{"typescript-language-server", "--stdio"}},
	{Name: "clangd", Cmd: []string{"clangd"}},
}

func testLSPInput(s containerLSPSettings) containerLSPInput {
	return containerLSPInput{
		Settings: s,
		Root:     "/app",
		Label:    "api-1",
		InImage:  map[string]string{"gopls": "/go/bin/gopls"},
		LocalCmd: func(bin string) (string, bool) {
			if bin == "gopls" || bin == "typescript-language-server" {
				return "/usr/local/bin/" + bin, true
			}
			return "", false
		},
		ImageArgv: func(argv []string) []string {
			return append([]string{"docker", "exec", "-i", "-w", "/app", "api-1"}, argv...)
		},
		Proxy:    "/opt/termocode",
		LocalDir: "/home/u/proj",
	}
}

func TestPlanContainerLSPPrefersTheImageAndMutesLocal(t *testing.T) {
	p := planContainerLSP(testLSPInput(containerLSPSettings{}), testSpecs)
	g := p.Servers["gopls"]
	if g.Place != lspInImage || strings.Join(g.Cmd, " ") != "docker exec -i -w /app api-1 /go/bin/gopls" || g.Mute {
		t.Errorf("gopls = %+v", g)
	}
	ts := p.Servers["ts_ls"]
	want := "/opt/termocode lsp-proxy --map /app=/home/u/proj -- /usr/local/bin/typescript-language-server --stdio"
	if ts.Place != lspLocal || strings.Join(ts.Cmd, " ") != want {
		t.Errorf("ts_ls = %+v", ts)
	}
	if !ts.Mute {
		t.Error("Q5: a local server's diagnostics are muted by default in a container workspace")
	}
	if _, ok := p.Servers["clangd"]; ok {
		t.Error("clangd is neither in the image nor local")
	}
}

func TestPlanContainerLSPSettings(t *testing.T) {
	p := planContainerLSP(testLSPInput(containerLSPSettings{LocalDiagnostics: "on"}), testSpecs)
	if p.Servers["ts_ls"].Mute {
		t.Error("diagnostics on: not muted")
	}
	p = planContainerLSP(testLSPInput(containerLSPSettings{Where: "image"}), testSpecs)
	if len(p.Servers) != 1 || p.Servers["gopls"].Place != lspInImage {
		t.Errorf("image only = %+v", p.Servers)
	}
	p = planContainerLSP(testLSPInput(containerLSPSettings{Where: "local"}), testSpecs)
	if p.Servers["gopls"].Place != lspLocal || p.Servers["ts_ls"].Place != lspLocal {
		t.Errorf("local only = %+v", p.Servers)
	}
	if p = planContainerLSP(testLSPInput(containerLSPSettings{Where: "off"}), testSpecs); len(p.Servers) != 0 {
		t.Errorf("off = %+v", p.Servers)
	}
	in := testLSPInput(containerLSPSettings{})
	in.LocalDir = ""
	if p = planContainerLSP(in, testSpecs); len(p.Servers) != 1 {
		t.Errorf("no mount and no mirror: only the image server, got %+v", p.Servers)
	}
}

func TestNeedsMirror(t *testing.T) {
	in := testLSPInput(containerLSPSettings{})
	if needsMirror(in, testSpecs) {
		t.Error("a bind mount needs no mirror")
	}
	in.LocalDir = ""
	if !needsMirror(in, testSpecs) {
		t.Error("ts_ls is only local: the mirror helps")
	}
	in.InImage = map[string]string{"gopls": "/x", "typescript-language-server": "/y"}
	if needsMirror(in, testSpecs) {
		t.Error("everything is in the image: no copy")
	}
	in.Settings.Where = "image"
	in.InImage = nil
	if needsMirror(in, testSpecs) {
		t.Error("image only never copies the code")
	}
}

func TestContainerLSPChipLabels(t *testing.T) {
	p := planContainerLSP(testLSPInput(containerLSPSettings{}), testSpecs)
	if got := p.chipLabel("gopls"); got != "gopls · in api-1" {
		t.Errorf("chip = %q", got)
	}
	if got := p.chipLabel("ts_ls"); got != "ts_ls · local" {
		t.Errorf("chip = %q", got)
	}
	if got := p.chipLabel("copilot"); got != "copilot" {
		t.Errorf("unknown client = %q", got)
	}
}

func TestContainerLSPSettingsRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if s := loadContainerLSPSettings(); s.where() != "auto" || s.showLocalDiagnostics() {
		t.Fatalf("defaults = %+v", s)
	}
	if err := saveContainerLSPSetting("container_lsp", "local"); err != nil {
		t.Fatal(err)
	}
	if err := saveContainerLSPSetting("container_local_lsp_diagnostics", "on"); err != nil {
		t.Fatal(err)
	}
	if s := loadContainerLSPSettings(); s.where() != "local" || !s.showLocalDiagnostics() {
		t.Fatalf("after save = %+v", s)
	}
	if err := saveContainerLSPSetting("container_lsp", ""); err != nil {
		t.Fatal(err)
	}
	if s := loadContainerLSPSettings(); s.where() != "auto" {
		t.Fatalf("cleared = %+v", s)
	}
}

func TestLSPSetupLuaFollowsTheContainerPlan(t *testing.T) {
	for _, want := range []string{
		"_G._termocode_lsp_container",
		"ctab == 'pending'",
		"ctab.servers[srv.name]",
		"'textDocument/publishDiagnostics'",
		"_termocode_where = where",
		"_termocode_gen = where and ctab.gen",
		"reuse_client = where and function",
		"name:sub(1, #pre) ~= pre",
	} {
		if !strings.Contains(lspSetupLua, want) {
			t.Errorf("lspSetupLua has no %s", want)
		}
	}
	for _, want := range []string{"'pending'", "vim.json.decode", "_termocode_where", "c.stop", "_G._termocode_lsp_start"} {
		if !strings.Contains(containerLSPLua, want) {
			t.Errorf("containerLSPLua has no %s", want)
		}
	}
}

func TestContainerLSPPaletteIDs(t *testing.T) {
	ids := map[string]bool{}
	for _, it := range containerLSPPaletteItems() {
		ids[it.ID] = true
		if _, ok := aiActionPolicies[it.ID]; !ok {
			t.Errorf("%s has no AI policy", it.ID)
		}
	}
	for _, id := range []string{"lsp-container-where", "lsp-container-diagnostics", "lsp-mirror-refresh", "lsp-mirror-clear"} {
		if !ids[id] {
			t.Errorf("palette has no %s", id)
		}
	}
	if aiActionPolicies["lsp-mirror-clear"].Gate != aiGateNever || aiActionPolicies["lsp-container-where"].Gate != aiGateNever {
		t.Error("the picker and the delete card are the user's")
	}
}

func TestContainerLSPPaletteOnLocalWorkspace(t *testing.T) {
	m := newTestModel(t)
	if _, ok := m.dispatchContainerLSPPalette("lsp-container-where"); !ok {
		t.Fatal("not handled")
	}
	if !strings.Contains(regToast(&m), "Only for a folder in a container") {
		t.Errorf("toast = %q", regToast(&m))
	}
	if m.pickerOpen {
		t.Error("no picker for a local workspace")
	}
	if m.containerLSPInitCmd() != nil {
		t.Error("a local workspace probes nothing")
	}
}

// fakeLSPHost is a container host with language-server answers.
type fakeLSPHost struct {
	workspace.Host
	found   map[string]string
	mountOf string
	tar     []byte
	asked   []string
}

func (h *fakeLSPHost) Label() string { return "api-1" }
func (h *fakeLSPHost) Caps() workspace.Caps {
	return workspace.Caps{HasShell: true, MountOf: h.mountOf}
}
func (h *fakeLSPHost) FindCommands(_ context.Context, names []string) (map[string]string, error) {
	h.asked = names
	return h.found, nil
}
func (h *fakeLSPHost) LSPArgv(argv []string) []string {
	return append([]string{"docker", "exec", "-i", "api-1"}, argv...)
}
func (h *fakeLSPHost) TreeTar(context.Context) (io.ReadCloser, func(), error) {
	return io.NopCloser(bytes.NewReader(h.tar)), func() {}, nil
}

func lspContainerModel(t *testing.T, h *fakeLSPHost) *Model {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	base := t.TempDir()
	old := containerMirrorBase
	containerMirrorBase = func() (string, error) { return base, nil }
	t.Cleanup(func() { containerMirrorBase = old })
	mm := newTestModel(t)
	mm.ws = workspace.Workspace{ID: "container:default/api-1:/app", Kind: workspace.KindContainer, Root: "/app", Host: h}
	mm.containerLSP = &containerLSPState{}
	t.Cleanup(func() { activeLSPMirror.Store(nil) })
	return &mm
}

func TestContainerLSPProbeThenPlan(t *testing.T) {
	h := &fakeLSPHost{found: map[string]string{"gopls": "/go/bin/gopls"}, mountOf: "/home/u/proj"}
	m := lspContainerModel(t, h)
	cmd := m.containerLSPInitCmd()
	if cmd == nil {
		t.Fatal("a container workspace probes its image")
	}
	msg := cmd()
	if _, ok := m.handleContainerLSPMsg(msg); !ok {
		t.Fatal("probe answer not handled")
	}
	if m.containerLSP.filling {
		t.Error("a bind mount needs no mirror fill")
	}
	if len(h.asked) == 0 {
		t.Fatal("no server names asked")
	}
	gen := m.containerLSP.plan.Gen
	m.applyContainerLSP(false)
	if m.containerLSP.plan.Gen != gen+1 {
		t.Errorf("a new plan must get a new generation: %d → %d", gen, m.containerLSP.plan.Gen)
	}
	g := m.containerLSP.plan.Servers["gopls"]
	if g.Place != lspInImage || g.Cmd[len(g.Cmd)-1] != "/go/bin/gopls" {
		t.Errorf("gopls plan = %+v", g)
	}
	m.setLSPClients([]string{"gopls"})
	if got := m.lspChipClients(); len(got) != 1 || got[0] != "gopls · in api-1" {
		t.Errorf("chip = %q", got)
	}
}

func testTar(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	return buf.Bytes()
}

func TestContainerMirrorFillSyncAndClearCard(t *testing.T) {
	h := &fakeLSPHost{found: map[string]string{}, tar: testTar(t, map[string]string{"app/main.go": "package main\n"})}
	m := lspContainerModel(t, h)
	m.containerLSPInitCmd() // makes the mirror
	st := m.containerLSP
	if st.mirror == nil {
		t.Fatal("no mirror")
	}
	fill := m.fillContainerMirrorCmd()
	if fill == nil || !st.filling {
		t.Fatal("no fill")
	}
	done, ok := fill().(containerMirrorFilledMsg)
	if !ok || done.err != nil || done.stats.Files != 1 {
		t.Fatalf("fill = %+v", done)
	}
	m.handleContainerLSPMsg(done)
	if !st.mirrored || activeLSPMirror.Load() != st.mirror {
		t.Fatal("the mirror is not active after the fill")
	}
	if !strings.Contains(regToast(m), "Local language server ready") {
		t.Errorf("toast = %q", regToast(m))
	}
	in, _ := m.containerLSPInputNow()
	if in.LocalDir != st.mirror.Dir {
		t.Errorf("local dir = %q, want the mirror", in.LocalDir)
	}
	// A save from termocode reaches the mirror.
	noteMirrorWrite("/app/main.go", []byte("package main // saved\n"))
	b, err := os.ReadFile(filepath.Join(st.mirror.Dir, "main.go"))
	if err != nil || !strings.Contains(string(b), "saved") {
		t.Errorf("mirror after save = %q, %v", b, err)
	}
	if fi, err := os.Stat(st.mirror.Dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Errorf("mirror mode = %v, %v", fi.Mode().Perm(), err)
	}

	// Clear: a Cancel-first card; Cancel keeps it, Clear deletes it.
	m.dispatchContainerLSPPalette("lsp-mirror-clear")
	if !m.confirmOpen || m.confirmKind != confirmKindContainerMirrorClear || m.confirm.SafeID() != "cancel" {
		t.Fatalf("card = %v %v %q", m.confirmOpen, m.confirmKind, m.confirm.SafeID())
	}
	row := ""
	for _, l := range strings.Split(ansi.Strip(m.confirm.Box()), "\n") {
		if strings.Contains(l, "Cancel") && strings.Contains(l, "Clear") {
			row = l
		}
	}
	if row == "" || strings.Index(row, "Cancel") > strings.Index(row, "Clear") {
		t.Errorf("Cancel must come first: %q", row)
	}
	m.answerMirrorClear("cancel")
	if _, err := os.Stat(st.mirror.Dir); err != nil {
		t.Fatal("Cancel deleted the mirror")
	}
	m.answerMirrorClear("clear")
	if _, err := os.Stat(st.mirror.Dir); err == nil || st.mirrored || activeLSPMirror.Load() != nil {
		t.Error("Clear left the mirror")
	}
}

func TestContainerLSPWherePickerSavesAndMarks(t *testing.T) {
	h := &fakeLSPHost{found: map[string]string{"gopls": "/go/bin/gopls"}, mountOf: "/home/u/proj"}
	m := lspContainerModel(t, h)
	m.dispatchContainerLSPPalette("lsp-container-where")
	if !m.pickerOpen || m.pickerKind != pickerKindContainerLSPWhere {
		t.Fatal("no picker")
	}
	rows := containerLSPWhereRows("auto")
	if !strings.HasPrefix(rows[0].Title, "●") {
		t.Errorf("current not marked: %+v", rows)
	}
	m.pickerOpen = false
	m.update(picker.SelectMsg{ID: "image"})
	if loadContainerLSPSettings().where() != "image" {
		t.Error("choice not saved")
	}
	m.toggleContainerLocalDiagnostics()
	if !loadContainerLSPSettings().showLocalDiagnostics() {
		t.Error("diagnostics toggle not saved")
	}
	b, _ := json.Marshal(m.containerLSP.plan)
	if strings.Contains(string(b), "lsp-proxy") {
		t.Errorf("image only must not plan a local server: %s", b)
	}
}

func TestMirrorKeyIsTheWorkspaceKey(t *testing.T) {
	m := lspContainerModel(t, &fakeLSPHost{})
	base, _ := containerMirrorBase()
	m.containerLSPInitCmd()
	if got, want := m.containerLSP.mirror.Dir, filepath.Join(base, workspace.Key(m.workspace().ID)); got != want {
		t.Errorf("mirror dir = %q, want %q", got, want)
	}
}
