package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func covSvcPalette(t *testing.T, m *Model, id string) tea.Cmd {
	t.Helper()
	cmd, ok := m.dispatchServicesPalette(id)
	if !ok {
		t.Fatalf("%s not handled", id)
	}
	return cmd
}

// The AI's container commands find the named container, image, network,
// volume or project and do what the key would; unknown names are errors.
func TestCovSvcAIContainerDispatch(t *testing.T) {
	m, f := covSvcHost(t)
	var clip []string
	defer SetClipboardSink(func(s string) { clip = append(clip, s) })()
	for _, c := range []struct{ id, target, call string }{
		{"containers-start", "termocode-api-1", "start c3"},
		{"containers-restart", "c2", "restart c2"},
		{"containers-pause", "builder", "unpause c6"},
		{"containers-pause", "nginx", "pause c5"},
		{"containers-image-history", "redis:7", "history"},
		{"containers-image-inspect", "sha256:ccc", "inspect"},
	} {
		cmd, err := m.dispatchContainerAIAction(c.id, c.target)
		if err != nil || cmd == nil {
			t.Fatalf("%s %s: %v", c.id, c.target, err)
		}
		pumpHost(t, m, cmd)
		if !strings.Contains(covSvcCalls(f), c.call) {
			t.Errorf("%s: calls %q", c.id, f.Calls())
		}
	}
	for _, c := range []struct{ id, target, want string }{
		{"containers-logs", "nginx", "Cannot open logs"},
		{"containers-shell", "nginx", "Cannot open shell"},
		{"containers-attach", "nginx", "Cannot open attach"},
		{"containers-stats", "nginx", "Cannot open stats"},
		{"containers-copy-id", "nginx", "Copied container id"},
	} {
		if _, err := m.dispatchContainerAIAction(c.id, c.target); err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
		if !strings.Contains(regToast(m), c.want) {
			t.Errorf("%s toast = %q", c.id, regToast(m))
		}
	}
	if len(clip) != 1 || clip[0] != "c5" {
		t.Errorf("clip = %q", clip)
	}
	var opened []string
	old := osOpen
	osOpen = func(u string) error { opened = append(opened, u); return nil }
	defer func() { osOpen = old }()
	if _, err := m.dispatchContainerAIAction("containers-open-port", "nginx"); err != nil || len(opened) != 1 || opened[0] != "http://localhost:8081" {
		t.Errorf("open port: %q %v", opened, err)
	}
	if _, err := m.dispatchContainerAIAction("containers-inspect", "nginx"); err != nil {
		t.Error(err)
	}
	// Cards open for the risky ones, never run.
	for _, c := range []struct{ id, target, card string }{
		{"containers-remove", "nginx", "Remove container?"},
		{"containers-image-push", "redis:7", "Push image?"},
		{"containers-image-remove", "postgres:15-alpine", "Remove image?"},
		{"containers-compose-down", "termocode", "Remove project containers?"},
	} {
		m.confirmOpen = false
		if _, err := m.dispatchContainerAIAction(c.id, c.target); err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
		if !m.confirmOpen || !strings.Contains(cardText(m), c.card) {
			t.Errorf("%s card = %s", c.id, cardText(m))
		}
	}
	m.confirmOpen = false
	for _, id := range []string{"containers-image-tag", "containers-image-run"} {
		m.promptOpen = false
		if _, err := m.dispatchContainerAIAction(id, "redis:7"); err != nil || !m.promptOpen {
			t.Errorf("%s: prompt=%v err=%v", id, m.promptOpen, err)
		}
	}
	m.promptOpen = false
	// The compose verbs run on the project (no editor: a toast) and keep
	// the cursor where it was.
	m.services.cursor = 1
	for _, id := range []string{"containers-compose-up", "containers-compose-stop", "containers-compose-restart"} {
		if _, err := m.dispatchContainerAIAction(id, "termocode"); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if !strings.Contains(regToast(m), "project") || m.services.cursor != 1 {
			t.Errorf("%s: toast=%q cursor=%d", id, regToast(m), m.services.cursor)
		}
	}
	// Errors.
	for _, c := range []struct{ id, target, want string }{
		{"containers-registry-connect", "", "registry"},
		{"containers-prune", "", "pruning is the user's"},
		{"containers-start", "", "needs a named container"},
		{"containers-image-tag", "nope:1", `image "nope:1" was not found`},
		{"containers-compose-up", "nope", `compose project "nope" was not found`},
		{"containers-start", "nope", `container "nope" was not found`},
		{"containers-explode", "nginx", "unsupported container command"},
	} {
		if _, err := m.dispatchContainerAIAction(c.id, c.target); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %q: %v", c.id, c.target, err)
		}
	}
	if !containerAITargetRequired("containers-start") || containerAITargetRequired("containers-refresh") {
		t.Error("containerAITargetRequired")
	}
}

// The "Containers: …" palette acts on the selected row; with no fitting
// row it says what to select.
func TestCovSvcPaletteOnSelection(t *testing.T) {
	m, f := covSvcHost(t)
	var clip []string
	defer SetClipboardSink(func(s string) { clip = append(clip, s) })()
	covSvcSection(t, m, "containers")
	for _, id := range []string{"containers-start", "containers-logs", "containers-copy-id"} {
		covSvcPalette(t, m, id)
		if !strings.Contains(regToast(m), "Select a container row first") {
			t.Errorf("%s with no container: %q", id, regToast(m))
		}
	}
	covSvcContainer(t, m, "c6")
	for _, c := range []struct{ id, call string }{
		{"containers-start", "start c6"}, {"containers-stop", "stop c6"}, {"containers-restart", "restart c6"}, {"containers-pause", "unpause c6"},
	} {
		covSvcContainer(t, m, "c6")
		pumpHost(t, m, covSvcPalette(t, m, c.id))
		if !strings.Contains(covSvcCalls(f), c.call) {
			t.Errorf("%s: calls %q", c.id, f.Calls())
		}
	}
	covSvcContainer(t, m, "c5")
	for id, want := range map[string]string{
		"containers-logs": "Cannot open logs", "containers-shell": "Cannot open shell", "containers-attach": "Cannot open attach",
		"containers-stats": "Cannot open stats", "containers-copy-id": "Copied container id",
	} {
		covSvcContainer(t, m, "c5")
		covSvcPalette(t, m, id)
		if !strings.Contains(regToast(m), want) {
			t.Errorf("%s toast = %q", id, regToast(m))
		}
	}
	var opened []string
	old := osOpen
	osOpen = func(u string) error { opened = append(opened, u); return nil }
	defer func() { osOpen = old }()
	covSvcContainer(t, m, "c5")
	covSvcPalette(t, m, "containers-open-port")
	if len(opened) != 1 {
		t.Errorf("open port: %q", opened)
	}
	covSvcContainer(t, m, "c5")
	pumpHost(t, m, covSvcPalette(t, m, "containers-inspect"))
	covSvcContainer(t, m, "c5")
	covSvcPalette(t, m, "containers-remove")
	if !m.confirmOpen || !strings.Contains(cardText(m), "nginx") {
		t.Errorf("remove card = %s", cardText(m))
	}
	m.confirmOpen = false
	// Image commands on the selected image.
	for _, c := range []struct {
		id     string
		prompt bool
		card   string
	}{
		{"containers-image-pull", true, ""}, {"containers-image-tag", true, ""}, {"containers-image-run", true, ""},
		{"containers-image-push", false, "Push image?"}, {"containers-image-remove", false, "Remove image?"},
	} {
		covSvcImage(t, m, "postgres:15-alpine")
		m.promptOpen, m.confirmOpen = false, false
		covSvcPalette(t, m, c.id)
		if c.prompt != m.promptOpen || (c.card != "" && !strings.Contains(cardText(m), c.card)) {
			t.Errorf("%s: prompt=%v card=%s", c.id, m.promptOpen, cardText(m))
		}
	}
	m.promptOpen, m.confirmOpen = false, false
	covSvcImage(t, m, "postgres:15-alpine")
	pumpHost(t, m, covSvcPalette(t, m, "containers-image-history"))
	covSvcImage(t, m, "postgres:15-alpine")
	pumpHost(t, m, covSvcPalette(t, m, "containers-image-inspect"))
	if f.Count("history") == 0 {
		t.Errorf("history not read: %q", f.Calls())
	}
	// Pull with no image selected asks for a name.
	covSvcSection(t, m, "images")
	covSvcPalette(t, m, "containers-image-pull")
	if !m.promptOpen || m.prompt.Value() != "" {
		t.Errorf("pull prompt open=%v value=%q", m.promptOpen, m.prompt.Value())
	}
	m.promptOpen = false
	// Storage and prune commands.
	for _, c := range []struct {
		id     string
		prompt bool
	}{
		{"containers-network-create", true}, {"containers-volume-create", true},
		{"containers-prune", false}, {"containers-system-prune", false}, {"containers-image-prune", false},
	} {
		m.promptOpen, m.confirmOpen = false, false
		pumpHost(t, m, covSvcPalette(t, m, c.id))
		if c.prompt != m.promptOpen || (!c.prompt && !m.confirmOpen) {
			t.Errorf("%s: prompt=%v card=%v", c.id, m.promptOpen, m.confirmOpen)
		}
	}
	m.promptOpen, m.confirmOpen = false, false
	pumpHost(t, m, covSvcPalette(t, m, "containers-network-prune"))
	m.confirmOpen = false
	pumpHost(t, m, covSvcPalette(t, m, "containers-volume-prune"))
	m.confirmOpen = false
	covSvcHostItem(t, m, hostVolume, "termocode_db")
	pumpHost(t, m, covSvcPalette(t, m, "containers-volume-remove"))
	if !m.confirmOpen {
		t.Error("volume remove: no card")
	}
	m.confirmOpen = false
	pumpHost(t, m, covSvcPalette(t, m, "containers-volume-disk-usage"))
	// Compose project commands on the selected group.
	covSvcGroup(t, m, "shop")
	for _, id := range []string{"containers-compose-up", "containers-compose-stop", "containers-compose-restart"} {
		covSvcGroup(t, m, "shop")
		pumpHost(t, m, covSvcPalette(t, m, id))
	}
	if !strings.Contains(covSvcCalls(f), "start c4") || !strings.Contains(covSvcCalls(f), "restart c4") {
		t.Errorf("compose fallbacks: %q", f.Calls())
	}
	covSvcGroup(t, m, "shop")
	covSvcPalette(t, m, "containers-compose-down")
	if !strings.Contains(regToast(m), "Cannot safely remove project") {
		t.Errorf("down toast = %q", regToast(m))
	}
	// Filter, refresh, search, connect.
	covSvcPalette(t, m, "containers-filter")
	if !m.services.filterTyping || m.focus != FocusExplorer {
		t.Error("filter not typing")
	}
	m.services.filterTyping = false
	pumpHost(t, m, covSvcPalette(t, m, "containers-refresh"))
	covSvcPalette(t, m, "containers-search")
	if !m.promptOpen {
		t.Error("search: no prompt")
	}
	m.promptOpen = false
	covSvcPalette(t, m, "containers-registry-connect")
	if !m.promptOpen && !m.confirmOpen && !m.pickerOpen {
		t.Error("connect opened nothing")
	}
	m.promptOpen, m.confirmOpen, m.pickerOpen = false, false, false
	// Open in Databases with nothing selected says so.
	m.services.cursor = -1
	m.services.det.ComposeFiles = nil
	m.services.compose, m.services.local = nil, nil
	m.services.sections = map[string]bool{"project": false, "containers": false, "images": false, "registries": false, "networks": false, "volumes": false, "contexts": false, "help": false}
	covSvcPalette(t, m, "containers-open-databases")
	if !strings.Contains(regToast(m), "Select a database container or service first") {
		t.Errorf("databases toast = %q", regToast(m))
	}
	if _, ok := m.dispatchServicesPalette("git-commit"); ok {
		t.Error("a non-services id was handled")
	}
}

// The context palette commands with a context selected.
func TestCovSvcPaletteContexts(t *testing.T) {
	m, f := covSvcHost(t)
	covSvcHostItem(t, m, hostContext, "prod")
	covSvcPalette(t, m, "containers-context-default")
	if !m.confirmOpen || !strings.Contains(cardText(m), "context use prod") {
		t.Errorf("default card = %s", cardText(m))
	}
	m.confirmOpen = false
	covSvcHostItem(t, m, hostContext, "prod")
	pumpHostUntil(t, m, covSvcPalette(t, m, "containers-context-use"), func(tea.Msg) bool { return m.services.sessionContext == "prod" && len(f.CtxCalls()) > 0 })
	if m.services.sessionContext != "prod" || !strings.Contains(regToast(m), "Using context prod") {
		t.Errorf("use: %q %q", m.services.sessionContext, regToast(m))
	}
	// With the cursor elsewhere it asks to pick.
	covSvcSection(t, m, "containers")
	covSvcPalette(t, m, "containers-context-use")
	if !strings.Contains(regToast(m), "Pick a context") {
		t.Errorf("pick toast = %q", regToast(m))
	}
}

// Services palette commands: the pickers list every service with its
// state; a pick runs the verb; start/stop all reach both sections.
func TestCovSvcPaletteServices(t *testing.T) {
	m, f := covSvcProject(t)
	for _, verb := range []string{"start", "stop", "restart", "logs"} {
		m.pickerOpen = false
		covSvcPalette(t, m, "services-"+verb)
		if !m.pickerOpen {
			t.Errorf("%s: no picker", verb)
		}
	}
	pumpHost(t, m, m.handleServicesPickerSelect("svc:stop:compose/redis"))
	if !strings.Contains(covSvcCalls(f), "stop redis") {
		t.Errorf("picked stop: %q", f.Calls())
	}
	pumpHost(t, m, m.handleServicesPickerSelect("svc:restart:compose/postgres"))
	if !strings.Contains(covSvcCalls(f), "restart postgres") {
		t.Errorf("picked restart: %q", f.Calls())
	}
	m.handleServicesPickerSelect("svc:start:compose/redis")
	if !strings.Contains(regToast(m), "already running") {
		t.Errorf("picked start toast = %q", regToast(m))
	}
	m.handleServicesPickerSelect("svc:logs:compose/redis")
	if !strings.Contains(regToast(m), "Cannot show logs") {
		t.Errorf("picked logs toast = %q", regToast(m))
	}
	if m.handleServicesPickerSelect("svc:explode:compose/redis") != nil {
		t.Error("unknown verb did something")
	}
	pumpHost(t, m, covSvcPalette(t, m, "services-stop-all"))
	covSvcPalette(t, m, "services-start-all")
	if !m.confirmOpen || !strings.Contains(cardText(m), "Run web?") {
		t.Errorf("start all asks for the local command: %s", cardText(m))
	}
	m.confirmOpen = false
	pumpHost(t, m, covSvcPalette(t, m, "services-refresh"))
	covSvcPalette(t, m, "services-focus")
	if m.focus != FocusExplorer {
		t.Error("focus not in the view")
	}
	covSvcPalette(t, m, "services-open-compose")
	covSvcPalette(t, m, "services-configure")
	covSvcPalette(t, m, "services-trust")
	covSvcPalette(t, m, "services-unknown")
	// Before the first snapshot a command waits for it.
	m.services.loaded = false
	covSvcPalette(t, m, "services-start-all")
	if len(m.services.waiters) == 0 {
		t.Error("no waiter before the first snapshot")
	}
	// No services: the picker says so.
	m.services.compose, m.services.local = nil, nil
	if m.openServicesPicker("start") == nil || !strings.Contains(regToast(m), "No services") {
		t.Errorf("empty picker toast = %q", regToast(m))
	}
}

// The service row menu: every item does what its key does.
func TestCovSvcServiceMenu(t *testing.T) {
	m, f := covSvcProject(t)
	r := selectRow(t, m.services, func(r svcRow) bool { return r.kind == svcRowService && r.name == "redis" })
	m.openServicesMenu(r, 5, 5)
	if !m.menuOpen || m.menuKind != menuKindServices {
		t.Fatal("no menu")
	}
	m.handleServicesMenuSelect("start")
	if !strings.Contains(regToast(m), "already running") {
		t.Errorf("start toast = %q", regToast(m))
	}
	pumpHost(t, m, m.handleServicesMenuSelect("stop"))
	pumpHost(t, m, m.handleServicesMenuSelect("restart"))
	if !strings.Contains(covSvcCalls(f), "stop redis") || !strings.Contains(covSvcCalls(f), "restart redis") {
		t.Errorf("calls = %q", f.Calls())
	}
	m.handleServicesMenuSelect("logs")
	m.handleServicesMenuSelect("shell")
	if !strings.Contains(regToast(m), "Cannot open a shell") {
		t.Errorf("shell toast = %q", regToast(m))
	}
	var clip []string
	defer SetClipboardSink(func(s string) { clip = append(clip, s) })()
	m.handleServicesMenuSelect("copy")
	if len(clip) != 1 || clip[0] != "localhost:6379" {
		t.Errorf("copy = %q", clip)
	}
	m.handleServicesMenuSelect("open")
	m.handleServicesMenuSelect("down")
	if !m.confirmOpen || m.confirmKind != confirmKindServicesDown {
		t.Error("down: no card")
	}
	// A local row's menu names its source file.
	local := selectRow(t, m.services, func(r svcRow) bool { return r.kind == svcRowService && r.name == "web" })
	m.openServicesMenu(local, 5, 5)
	if view := ansi.Strip(m.menu.View()); !strings.Contains(view, "Open Procfile") {
		t.Errorf("local menu = %s", view)
	}
}
