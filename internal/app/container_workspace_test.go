package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/workspace"
)

func TestContainerRefRoundTripsTheWorkspaceID(t *testing.T) {
	for _, r := range []containerRef{
		{Name: "api-1", Root: "/app"},
		{Context: "prod-ssh", Name: "api-1", Root: "/srv/app"},
	} {
		got, ok := parseContainerRef(r.id())
		if !ok || got != r {
			t.Fatalf("parse(%q) = %+v %v, want %+v", r.id(), got, ok, r)
		}
	}
	if (containerRef{Name: "api-1", Root: "/app"}).id() != "container:default/api-1:/app" {
		t.Fatal("the ID must match containerhost.Host.ID")
	}
	for _, bad := range []string{"/home/u/proj", "local:/p", "container:default/api-1:app", "container:default/-x:/app", "container:api-1"} {
		if _, ok := parseContainerRef(bad); ok {
			t.Errorf("parse(%q) accepted", bad)
		}
	}
}

func TestContainerRefArgsAndTitle(t *testing.T) {
	r := containerRef{Context: "prod-ssh", Name: "api-1", Root: "/app"}
	if got := strings.Join(r.args(), " "); got != "--container api-1 --context prod-ssh /app" {
		t.Fatalf("args = %q", got)
	}
	if r.title() != "▣ api-1 · prod-ssh /app" {
		t.Fatalf("title = %q", r.title())
	}
	if got := strings.Join(containerRef{Name: "api-1", Root: "/app"}.args(), " "); got != "--container api-1 /app" {
		t.Fatalf("default context args = %q", got)
	}
}

func TestPushContainerWorkspaceKeepsOneEntryFirst(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	PushContainerWorkspace("", "api-1", "/app")
	pushWorkspaceID("container:default/db-1:/data")
	PushContainerWorkspace("", "api-1", "/app")
	e := loadWorkspaces()
	if len(e) != 2 || e[0].Path != "container:default/api-1:/app" || e[1].Path != "container:default/db-1:/data" {
		t.Fatalf("entries = %+v", e)
	}
	if workspaceEntryTitle(e[0].Path) != "▣ api-1 /app" || workspaceEntryTitle("/home/u/p") != "/home/u/p" {
		t.Fatalf("titles = %q", workspaceEntryTitle(e[0].Path))
	}
}

// swapReexec records the re-exec instead of replacing the process.
func swapReexec(t *testing.T) *[][]string {
	t.Helper()
	var got [][]string
	oldR, oldC := reexecTermocode, canReexec
	reexecTermocode = func(a []string) error { got = append(got, a); return nil }
	canReexec = func() bool { return true }
	t.Cleanup(func() { reexecTermocode, canReexec = oldR, oldC })
	return &got
}

func TestOpenRecentContainerEntryReexecsWithContainer(t *testing.T) {
	got := swapReexec(t)
	m := newTestModel(t)
	m.openWorkspace("container:prod-ssh/api-1:/app")
	if len(*got) != 1 || strings.Join((*got)[0], " ") != "--container api-1 --context prod-ssh /app" {
		t.Fatalf("re-exec = %v", *got)
	}
}

func TestContainerRowWOpensAFolderPromptThenReexecs(t *testing.T) {
	got := swapReexec(t)
	m, _ := covSvcHost(t)
	for i := range m.services.containerRows {
		if m.services.containerRows[i].ID == "c5" {
			m.services.containerRows[i].WorkingDir = "/usr/share/nginx"
		}
	}
	covSvcContainer(t, m, "c5")
	covSvcKey(m, "w")
	if !m.promptOpen || m.promptKind != promptKindContainerFolder || m.prompt.Value() != "/usr/share/nginx" {
		t.Fatalf("prompt open %v kind %v value %q", m.promptOpen, m.promptKind, m.prompt.Value())
	}
	submitPrompt(t, m, "/etc/nginx/")
	if len(*got) != 1 || strings.Join((*got)[0], " ") != "--container nginx /etc/nginx" {
		t.Fatalf("re-exec = %v", *got)
	}
}

func TestContainerRowWOnAStoppedContainerSaysStartFirst(t *testing.T) {
	got := swapReexec(t)
	m, _ := covSvcHost(t)
	covSvcContainer(t, m, "c3")
	covSvcKey(m, "w")
	if m.promptOpen || !strings.Contains(regToast(m), "Start the container first") || len(*got) != 0 {
		t.Fatalf("prompt %v toast %q re-exec %v", m.promptOpen, regToast(m), *got)
	}
}

func TestContainerFolderPromptWantsAnAbsolutePath(t *testing.T) {
	got := swapReexec(t)
	m, _ := covSvcHost(t)
	covSvcContainer(t, m, "c5")
	covSvcKey(m, "w")
	submitPrompt(t, m, "app")
	if len(*got) != 0 || !strings.Contains(regToast(m), "full folder path") {
		t.Fatalf("re-exec %v toast %q", *got, regToast(m))
	}
}

func TestOpenFolderInContainerPaletteWithoutARowShowsTheView(t *testing.T) {
	m := newTestModel(t)
	if _, ok := m.selectedContainer(); ok {
		t.Skip("a container row is selected in the test model")
	}
	m.openFolderInContainerPalette()
	if !strings.Contains(regToast(&m), "press w") {
		t.Fatalf("toast = %q", regToast(&m))
	}
}

// fakeContainerHost is a container workspace host whose state a test sets.
type fakeContainerHost struct {
	workspace.Host
	online   bool
	startErr error
	started  int
}

func (h *fakeContainerHost) CheckOnline(context.Context) bool { return h.online }
func (h *fakeContainerHost) Online() bool                     { return h.online }
func (h *fakeContainerHost) Label() string                    { return "api-1" }
func (h *fakeContainerHost) Start(context.Context) error {
	h.started++
	if h.startErr == nil {
		h.online = true
	}
	return h.startErr
}

func containerModel(t *testing.T) (*Model, *fakeContainerHost) {
	t.Helper()
	mm := newTestModel(t)
	h := &fakeContainerHost{online: true}
	mm.ws = workspace.Workspace{ID: "container:default/api-1:/app", Kind: workspace.KindContainer, Root: "/app", Host: h}
	return &mm, h
}

func runMsg(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if msg := cmd(); msg != nil {
		if c, ok := m.handleContainerWSMsg(msg); ok {
			_ = c
		}
	}
}

func TestContainerWatchOnlyForContainerWorkspaces(t *testing.T) {
	m := newTestModel(t)
	if m.containerWatchInitCmd() != nil {
		t.Fatal("a local workspace is not watched")
	}
	cm, _ := containerModel(t)
	if cm.containerWatchInitCmd() == nil {
		t.Fatal("a container workspace is watched")
	}
	if cm.workspaceHostLabel() != "api-1" || m.workspaceHostLabel() != "" {
		t.Fatalf("labels %q %q", cm.workspaceHostLabel(), m.workspaceHostLabel())
	}
}

func TestContainerStopOpensTheOfflineCardWaitFirst(t *testing.T) {
	m, h := containerModel(t)
	h.online = false
	runMsg(m, m.containerCheckCmd())
	if !m.containerOffline || !m.confirmOpen || m.confirmKind != confirmKindContainerOffline {
		t.Fatalf("offline %v card %v kind %v", m.containerOffline, m.confirmOpen, m.confirmKind)
	}
	b := m.confirm.Buttons()
	if len(b) != 3 || b[0].ID != "wait" || b[1].ID != "start" || b[2].ID != "disconnect" {
		t.Fatalf("buttons = %+v", b)
	}
	if st := m.statusState(); st.Host != "api-1" || !st.HostOffline {
		t.Fatalf("status host %q offline %v", st.Host, st.HostOffline)
	}
	// Wait closes nothing else; a second offline answer does not reopen it.
	m.confirmOpen = false
	m.handleConfirmSelect("wait")
	runMsg(m, m.containerCheckCmd())
	if m.confirmOpen {
		t.Fatal("the card came again while still offline")
	}
	// Back by itself: the pill turns normal.
	h.online = true
	runMsg(m, m.containerCheckCmd())
	if m.containerOffline {
		t.Fatal("still offline after it came back")
	}
}

func TestOfflineCardStartRunsTheContainer(t *testing.T) {
	m, h := containerModel(t)
	h.online = false
	runMsg(m, m.containerCheckCmd())
	m.confirmOpen = false
	runMsg(m, m.handleConfirmSelect("start"))
	if h.started != 1 || m.containerOffline || m.containerStarting {
		t.Fatalf("started %d offline %v starting %v", h.started, m.containerOffline, m.containerStarting)
	}
	h.online, h.startErr = false, errors.New("boom")
	runMsg(m, m.containerCheckCmd())
	m.confirmOpen = false
	runMsg(m, m.handleConfirmSelect("start"))
	if !m.containerOffline || !strings.Contains(regToast(m), "Could not start") {
		t.Fatalf("offline %v toast %q", m.containerOffline, regToast(m))
	}
}

func TestOfflineCardDisconnectOpensHome(t *testing.T) {
	got := swapReexec(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	m, h := containerModel(t)
	h.online = false
	runMsg(m, m.containerCheckCmd())
	m.confirmOpen = false
	m.handleConfirmSelect("disconnect")
	if len(*got) != 1 || len((*got)[0]) != 1 || (*got)[0][0] != home {
		t.Fatalf("re-exec = %v", *got)
	}
}
