package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// covSvcHost is the Containers view on a loaded docker host (every
// section open) with the fake docker answering from the docker27 data.
func covSvcHost(t *testing.T) (*Model, *servicestest.Fake) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	t.Setenv("DOCKER_CONTEXT", "")
	t.Setenv("DOCKER_CONFIG", filepath.Join(home, "docker"))
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	f := hostFake(t)
	for _, name := range []string{"inspect.json", "history.json"} {
		b, err := os.ReadFile(filepath.Join("..", "containers", "testdata", "docker27", name))
		if err != nil {
			t.Fatal(err)
		}
		f.Set(t, name, string(b))
	}
	st := svcHostState()
	st.root = t.TempDir()
	for _, sec := range hostSectionOrder {
		st.collapsed[hostSection(sec)] = false
	}
	covSvcWriteHost(t, f, st)
	mm := svcTestModel(st, 120, 80)
	m := &mm
	t.Cleanup(func() { m.services.stopHostWatch(); m.services.stopWatch() })
	return m, f
}

// covSvcWriteHost makes the fake docker list st's rows, so a refresh
// after an action shows the same host.
func covSvcWriteHost(t *testing.T, f *servicestest.Fake, st *servicesState) {
	t.Helper()
	lines := func(items []map[string]any) string {
		var out []string
		for _, it := range items {
			b, err := json.Marshal(it)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, string(b))
		}
		return strings.Join(out, "\n") + "\n"
	}
	var ps, images, networks, volumes []map[string]any
	for _, c := range st.containerRows {
		ps = append(ps, map[string]any{"ID": c.ID, "Names": c.Names, "Image": c.Image, "State": c.State, "Status": c.Status, "Labels": c.Labels, "Ports": c.PortsText})
	}
	for _, im := range st.imageRows {
		images = append(images, map[string]any{"ID": im.ID, "RepoTags": im.RepoTags, "Size": im.Size})
	}
	for _, n := range st.networkRows {
		networks = append(networks, map[string]any{"Name": n.Name, "Driver": n.Driver, "Scope": n.Scope})
	}
	for _, v := range st.volumeRows {
		volumes = append(volumes, map[string]any{"Name": v.Name, "Driver": v.Driver})
	}
	f.Set(t, "ps.json", lines(ps))
	f.Set(t, "images.json", lines(images))
	f.Set(t, "network-ls.json", lines(networks))
	f.Set(t, "volume-ls.json", lines(volumes))
}

// covSvcKey is one key on the sidebar, as Bubble Tea names it.
func covSvcKey(m *Model, s string) tea.Cmd {
	types := map[string]tea.KeyType{
		"enter": tea.KeyEnter, "esc": tea.KeyEsc, "up": tea.KeyUp, "down": tea.KeyDown,
		"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "home": tea.KeyHome, "end": tea.KeyEnd,
		"tab": tea.KeyTab, "shift+tab": tea.KeyShiftTab, " ": tea.KeySpace,
	}
	k := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	if kt, ok := types[s]; ok {
		k = tea.KeyMsg{Type: kt}
	}
	next, cmd := m.handleServicesSidebarKey(k)
	*m = next
	return cmd
}

func covSvcContainer(t *testing.T, m *Model, id string) svcRow {
	t.Helper()
	return selectRow(t, m.services, func(r svcRow) bool { return r.kind == svcRowHostItem && r.host == hostContainer && r.hostID == id })
}

func covSvcGroup(t *testing.T, m *Model, name string) svcRow {
	t.Helper()
	return selectRow(t, m.services, func(r svcRow) bool {
		return r.kind == svcRowGroup && strings.HasPrefix(r.section, "grp/containers/") && r.name == name
	})
}

func covSvcSection(t *testing.T, m *Model, sec string) svcRow {
	t.Helper()
	return selectRow(t, m.services, func(r svcRow) bool { return r.kind == svcRowSection && r.section == hostSection(sec) })
}

func covSvcHostItem(t *testing.T, m *Model, host hostKind, name string) svcRow {
	t.Helper()
	return selectRow(t, m.services, func(r svcRow) bool {
		return r.kind == svcRowHostItem && r.host == host && (r.name == name || r.hostID == name || r.imageRef == name)
	})
}

func covSvcCalls(f *servicestest.Fake) string { return strings.Join(f.Calls(), "\n") }

// Container keys: s/x/r/p run the engine verb, y copies the id, D asks
// first, the terminal keys need an editor and say so.
func TestCovSvcContainerKeys(t *testing.T) {
	m, f := covSvcHost(t)
	var clip []string
	defer SetClipboardSink(func(s string) { clip = append(clip, s) })()
	for _, c := range []struct{ id, key, want string }{
		{"c3", "s", "start c3"}, {"c1", "x", "stop c1"}, {"c2", "r", "restart c2"},
		{"c1", "p", "pause c1"}, {"c6", "p", "unpause c6"},
	} {
		covSvcContainer(t, m, c.id)
		cmd := covSvcKey(m, c.key)
		if _, marked := m.services.busy[containerBusyPrefix+c.id]; !marked {
			t.Errorf("%s %s: not marked", c.key, c.id)
		}
		pumpHost(t, m, cmd)
		if !strings.Contains(covSvcCalls(f), c.want) {
			t.Errorf("%s on %s: calls %q", c.key, c.id, f.Calls())
		}
		if _, marked := m.services.busy[containerBusyPrefix+c.id]; marked {
			t.Errorf("%s %s: still marked after done", c.key, c.id)
		}
	}
	// A busy container answers with a note.
	covSvcContainer(t, m, "c1")
	m.services.busy[containerBusyPrefix+"c1"] = services.Stopping
	covSvcKey(m, "s")
	if !strings.Contains(regToast(m), "Container is busy") {
		t.Errorf("busy toast = %q", regToast(m))
	}
	delete(m.services.busy, containerBusyPrefix+"c1")
	covSvcKey(m, "y")
	if len(clip) != 1 || clip[0] != "c1" || !strings.Contains(regToast(m), "Copied container id") {
		t.Errorf("copy: %q %q", clip, regToast(m))
	}
	for key, want := range map[string]string{"e": "Cannot open shell", "t": "Cannot open attach", "S": "Cannot open stats", "enter": "Cannot open logs"} {
		covSvcContainer(t, m, "c1")
		covSvcKey(m, key)
		if !strings.Contains(regToast(m), want) {
			t.Errorf("%s toast = %q", key, regToast(m))
		}
	}
	// i reads `inspect`; without an editor no page opens, and no error.
	covSvcContainer(t, m, "c1")
	pumpHost(t, m, covSvcKey(m, "i"))
	if f.Count("inspect") == 0 {
		t.Errorf("inspect not run: %q", f.Calls())
	}
	// D on a running container offers "Stop and remove", Cancel first.
	covSvcKey(m, "D")
	if !m.confirmOpen || !strings.Contains(cardText(m), "Stop and remove") || !strings.Contains(cardText(m), "docker stop c1 && docker rm c1") {
		t.Fatalf("remove card = %s", cardText(m))
	}
	n := len(f.Calls())
	covSvcConfirm(t, m, "cancel")
	if len(f.Calls()) != n || m.pendingContainerID != "c1" {
		t.Errorf("cancel ran %q", f.Calls()[n:])
	}
	pumpHost(t, m, covSvcConfirm(t, m, "stop-remove"))
	if !strings.Contains(covSvcCalls(f), "stop c1") || !strings.Contains(covSvcCalls(f), "rm c1") {
		t.Errorf("stop+remove calls = %q", f.Calls())
	}
	// A stopped one is a plain remove.
	covSvcContainer(t, m, "c3")
	covSvcKey(m, "D")
	if strings.Contains(cardText(m), "Stop and remove") {
		t.Errorf("stopped card = %s", cardText(m))
	}
	pumpHost(t, m, covSvcConfirm(t, m, "remove"))
	if !strings.Contains(covSvcCalls(f), "rm c3") {
		t.Errorf("remove calls = %q", f.Calls())
	}
}

// A failed container verb is a toast with the engine's error line; an
// engine that is not ready refuses.
func TestCovSvcContainerActionErrors(t *testing.T) {
	m, f := covSvcHost(t)
	c1 := m.services.containerRows[0]
	f.Set(t, "all.err", "Error response from daemon: permission denied\nmore")
	covSvcContainer(t, m, "c1")
	pumpHost(t, m, covSvcKey(m, "x"))
	if got := regToast(m); !strings.Contains(got, "Could not stop container") || !strings.Contains(got, "No access to the Docker socket") {
		t.Errorf("toast = %q", got)
	}
	pumpHost(t, m, m.inspectContainer(c1))
	if !strings.Contains(regToast(m), "Cannot inspect container") {
		t.Errorf("inspect toast = %q", regToast(m))
	}
	f.Remove("all.err")
	m.services.containerEngine.Reachable = false
	if cmd := m.containerAction("start", c1); cmd == nil || !strings.Contains(regToast(m), "Container engine is not ready") {
		t.Errorf("not ready toast = %q", regToast(m))
	}
	m.services.containerEngine.Reachable = true
	if cmd := m.containerAction("explode", c1); cmd != nil {
		t.Error("an unknown verb did something")
	}
	if cmd := m.openContainerTerminal(c1, "explode"); cmd != nil {
		t.Error("an unknown terminal kind did something")
	}
}

// o opens the first published port in the browser; over SSH it copies
// the URL and names the tunnel; no port is a note.
func TestCovSvcContainerOpenPort(t *testing.T) {
	m, _ := covSvcHost(t)
	var opened, clip []string
	old := osOpen
	osOpen = func(u string) error { opened = append(opened, u); return nil }
	defer func() { osOpen = old }()
	defer SetClipboardSink(func(s string) { clip = append(clip, s) })()
	rows := m.services.containerRows
	rows[4].Ports = []containers.Port{{PrivatePort: 22}, {IP: "0.0.0.0", PrivatePort: 80, PublicPort: 8081}}
	rows[3].Ports = []containers.Port{{IP: "::1", PrivatePort: 80, PublicPort: 9000}}
	covSvcContainer(t, m, "c5")
	covSvcKey(m, "o")
	covSvcContainer(t, m, "c4")
	covSvcKey(m, "o")
	if strings.Join(opened, ",") != "http://localhost:8081,http://[::1]:9000" {
		t.Errorf("opened = %q", opened)
	}
	covSvcContainer(t, m, "c2")
	covSvcKey(m, "o")
	if !strings.Contains(regToast(m), "No published port") {
		t.Errorf("toast = %q", regToast(m))
	}
	osOpen = func(string) error { return os.ErrPermission }
	covSvcContainer(t, m, "c5")
	covSvcKey(m, "o")
	if !strings.Contains(regToast(m), "Cannot open port") {
		t.Errorf("open error toast = %q", regToast(m))
	}
	t.Setenv("SSH_CONNECTION", "1.2.3.4 5 9.9.9.9 22")
	covSvcKey(m, "o")
	if len(clip) != 1 || clip[0] != "http://localhost:8081" || !strings.Contains(regToast(m), "ssh -L 8081:localhost:8081 9.9.9.9") {
		t.Errorf("ssh: %q %q", clip, regToast(m))
	}
}

// Project group keys: the current project runs compose in its folder (an
// editor tab); a project without a folder falls back to its containers;
// D asks first and refuses without a folder.
func TestCovSvcProjectGroupKeys(t *testing.T) {
	m, f := covSvcHost(t)
	covSvcGroup(t, m, "termocode")
	covSvcKey(m, "s")
	if !strings.Contains(regToast(m), "Cannot up project") {
		t.Errorf("up toast = %q", regToast(m))
	}
	covSvcGroup(t, m, "shop")
	for _, key := range []string{"s", "x", "r"} {
		cmd := covSvcKey(m, key)
		if _, marked := m.services.busy[containerBusyPrefix+"c4"]; !marked {
			t.Errorf("%s: shop container not busy", key)
		}
		pumpHost(t, m, cmd)
		if !strings.Contains(regToast(m), "Project folder not available") {
			t.Errorf("%s fallback toast = %q", key, regToast(m))
		}
	}
	for _, want := range []string{"start c4", "stop c4", "restart c4"} {
		if !strings.Contains(covSvcCalls(f), want) {
			t.Errorf("missing %q in %q", want, f.Calls())
		}
	}
	f.Set(t, "all.err", "boom")
	pumpHost(t, m, covSvcKey(m, "x"))
	if !strings.Contains(regToast(m), "Project action failed") {
		t.Errorf("fail toast = %q", regToast(m))
	}
	f.Remove("all.err")
	pumpHost(t, m, m.containersRefresh("containers"))
	covSvcGroup(t, m, "shop")
	covSvcKey(m, "D")
	if m.confirmOpen || !strings.Contains(regToast(m), "Cannot safely remove project") {
		t.Errorf("down without folder: open=%v toast=%q", m.confirmOpen, regToast(m))
	}
	// The "other" group is not a project.
	covSvcGroup(t, m, "other")
	if cmd := covSvcKey(m, "D"); cmd != nil || m.confirmOpen {
		t.Error("D on other did something")
	}
	if cmd := m.projectGroupAction("up"); cmd != nil {
		t.Error("up on other did something")
	}
	// The current project: D shows compose down; Down runs it.
	m.services.containerRows[0].ConfigFiles = []string{filepath.Join(m.services.root, "compose.yaml")}
	covSvcGroup(t, m, "termocode")
	covSvcKey(m, "D")
	if !m.confirmOpen || !strings.Contains(cardText(m), "compose") || !strings.Contains(cardText(m), "down") {
		t.Fatalf("down card = %s", cardText(m))
	}
	covSvcConfirm(t, m, "cancel")
	covSvcKey(m, "D")
	cmd := covSvcConfirm(t, m, "down")
	if m.services.busy[containerBusyPrefix+"c1"] != services.Stopping {
		t.Error("project containers not stopping")
	}
	pumpHost(t, m, cmd)
	if !strings.Contains(covSvcCalls(f), "down") || m.services.busy[containerBusyPrefix+"c1"].Busy() {
		t.Errorf("down calls = %q busy=%v", f.Calls(), m.services.busy)
	}
	// compose down fails: the error line is a toast.
	f.Set(t, "down.err", "network in use")
	covSvcKey(m, "D")
	pumpHost(t, m, covSvcConfirm(t, m, "down"))
	if !strings.Contains(regToast(m), "network in use") {
		t.Errorf("down error toast = %q", regToast(m))
	}
	// Containers of a project with no rows at all.
	m.services.containerRows[3].Project = "gone"
	if cmd := m.projectGroupFallback("nothing", "up"); cmd == nil || !strings.Contains(regToast(m), "No containers were found for nothing") {
		t.Errorf("empty fallback toast = %q", regToast(m))
	}
}

// Section header keys: r refreshes, z asks to prune, n names a new
// network or volume, d reads the disk use, u asks for an image, ? shows
// the keys, Space and Enter fold.
func TestCovSvcSectionKeys(t *testing.T) {
	m, f := covSvcHost(t)
	f.Set(t, "df.txt", "VOLUME NAME   LINKS   SIZE\ntermocode_db  1       42MB\n")
	covSvcSection(t, m, "containers")
	pumpHost(t, m, covSvcKey(m, "r"))
	if f.Count("ps") == 0 {
		t.Errorf("r did not list containers: %q", f.Calls())
	}
	covSvcSection(t, m, "containers")
	covSvcKey(m, "z")
	if !m.confirmOpen {
		t.Errorf("z: no prune card")
	}
	m.confirmOpen = false
	covSvcSection(t, m, "networks")
	covSvcKey(m, "n")
	if !m.promptOpen || m.promptKind != promptKindContainerStorageName {
		t.Fatal("n: no name prompt")
	}
	pumpHost(t, m, submitPrompt(t, m, "-bad"))
	if !strings.Contains(regToast(m), "Name cannot start with a dash") {
		t.Errorf("dash toast = %q", regToast(m))
	}
	covSvcKey(m, "n")
	pumpHost(t, m, submitPrompt(t, m, "two words"))
	if !strings.Contains(regToast(m), "Enter a name without spaces") {
		t.Errorf("space toast = %q", regToast(m))
	}
	covSvcKey(m, "n")
	pumpHost(t, m, submitPrompt(t, m, "backend"))
	if !strings.Contains(covSvcCalls(f), "network create backend") {
		t.Errorf("network create calls = %q", f.Calls())
	}
	covSvcSection(t, m, "volumes")
	covSvcKey(m, "n")
	pumpHost(t, m, submitPrompt(t, m, "cache"))
	if !strings.Contains(covSvcCalls(f), "volume create cache") {
		t.Errorf("volume create calls = %q", f.Calls())
	}
	f.Set(t, "all.err", "no space left")
	covSvcSection(t, m, "volumes")
	covSvcKey(m, "n")
	pumpHost(t, m, submitPrompt(t, m, "cache2"))
	if !strings.Contains(regToast(m), "Could not create volume") {
		t.Errorf("create error toast = %q", regToast(m))
	}
	covSvcSection(t, m, "volumes")
	pumpHost(t, m, covSvcKey(m, "d"))
	if !strings.Contains(regToast(m), "Cannot read disk usage") {
		t.Errorf("df error toast = %q", regToast(m))
	}
	f.Remove("all.err")
	covSvcSection(t, m, "volumes")
	pumpHost(t, m, covSvcKey(m, "d"))
	if m.services.volumeRows[0].Size != "42MB" {
		t.Errorf("volume size = %q", m.services.volumeRows[0].Size)
	}
	covSvcSection(t, m, "images")
	covSvcKey(m, "u")
	if !m.promptOpen || m.promptKind != promptKindContainerPull {
		t.Error("u on IMAGES: no pull prompt")
	}
	if cmd := submitPrompt(t, m, "  "); cmd != nil {
		t.Error("an empty pull did something")
	}
	covSvcSection(t, m, "images")
	covSvcKey(m, "?")
	if !m.confirmOpen || !strings.Contains(cardText(m), "Section keys") {
		t.Errorf("? card = %s", cardText(m))
	}
	m.confirmOpen = false
	covSvcSection(t, m, "networks")
	covSvcKey(m, " ")
	if !m.services.collapsed[hostSection("networks")] {
		t.Error("Space did not fold NETWORKS")
	}
	covSvcSection(t, m, "networks")
	pumpHost(t, m, covSvcKey(m, "enter"))
	if m.services.collapsed[hostSection("networks")] {
		t.Error("Enter did not open NETWORKS")
	}
}

// The ? card names the keys of the row kind it is on.
func TestCovSvcKeysHelpPerRow(t *testing.T) {
	for _, c := range []struct {
		r    svcRow
		want string
	}{
		{svcRow{kind: svcRowHostItem, host: hostContainer}, "Container keys"},
		{svcRow{kind: svcRowGroup, section: "grp/containers/x"}, "Project keys"},
		{svcRow{kind: svcRowHostItem, host: hostImage}, "Image keys"},
		{svcRow{kind: svcRowGroup, section: "grp/images/x"}, "Image keys"},
		{svcRow{kind: svcRowHostItem, host: hostRemoteRepo}, "Remote tags keys"},
		{svcRow{kind: svcRowHostItem, host: hostRemoteTag}, "Remote tag keys"},
		{svcRow{kind: svcRowHostItem, host: hostVolume}, "Network and volume keys"},
		{svcRow{kind: svcRowHostItem, host: hostContext}, "Context keys"},
		{svcRow{kind: svcRowHostItem, host: hostRegistryConnect}, "Registry keys"},
		{svcRow{kind: svcRowHostItem, host: hostHelpGuide}, "Help keys"},
		{svcRow{kind: svcRowSection, section: "host/images"}, "Section keys"},
		{svcRow{kind: svcRowService}, "Service keys"},
		{svcRow{kind: svcRowSection, section: svcSecCompose}, "Containers keys"},
	} {
		title, rows := hostKeysHelp(c.r)
		if title != c.want || len(rows) < 6 {
			t.Errorf("%+v: %q (%d rows), want %q", c.r, title, len(rows), c.want)
		}
	}
}

// Navigation keys move the cursor; Tab jumps headers and wraps; Esc
// gives the keyboard back to the editor.
func TestCovSvcNavigationKeys(t *testing.T) {
	m, _ := covSvcHost(t)
	st := m.services
	rows := st.rows()
	covSvcKey(m, "end")
	if r, _ := m.servicesCurrentRow(); st.cursor < len(rows)-5 || !r.selectable() {
		t.Errorf("end: %d of %d", st.cursor, len(rows))
	}
	covSvcKey(m, "home")
	if st.cursor != 0 {
		t.Errorf("home: %d", st.cursor)
	}
	covSvcKey(m, "pgdown")
	if st.cursor < 10 || st.cursor > 12 {
		t.Errorf("pgdown: %d", st.cursor)
	}
	covSvcKey(m, "k")
	covSvcKey(m, "up")
	covSvcKey(m, "pgup")
	if st.cursor != 0 {
		t.Errorf("pgup: %d", st.cursor)
	}
	covSvcKey(m, "tab")
	if r, _ := m.servicesCurrentRow(); r.kind != svcRowSection || r.section != svcSecLocal {
		t.Errorf("tab: %+v", r)
	}
	covSvcKey(m, "shift+tab")
	covSvcKey(m, "shift+tab")
	if r, _ := m.servicesCurrentRow(); r.kind != svcRowSection || r.section != hostSection("help") {
		t.Errorf("shift+tab wraps to the last header: %+v", r)
	}
	covSvcKey(m, "G")
	covSvcKey(m, "g")
	m.focus = FocusExplorer
	covSvcKey(m, "esc")
	if m.focus != FocusEditor {
		t.Error("esc kept the focus")
	}
	covSvcKey(m, "m")
	if !m.menuOpen || m.menuKind != menuKindContainers {
		t.Error("m: no sections menu")
	}
}

// FIX-32: a restart or pause in flight marks the container Restarting;
// a second pause or restart is refused until it is done.
func TestCovSvcRestartInFlightBlocksASecondAction(t *testing.T) {
	m, f := covSvcHost(t)
	covSvcContainer(t, m, "c2")
	if cmd := covSvcKey(m, "r"); cmd == nil {
		t.Fatal("restart did not start")
	}
	if got := m.services.busy[containerBusyPrefix+"c2"]; got != services.Restarting {
		t.Fatalf("busy = %v, want restarting", got)
	}
	covSvcKey(m, "p")
	if !strings.Contains(regToast(m), "Container is busy") {
		t.Errorf("second action toast = %q", regToast(m))
	}
	if strings.Contains(covSvcCalls(f), "pause c2") {
		t.Errorf("a pause was sent while the restart ran: %q", f.Calls())
	}
}
