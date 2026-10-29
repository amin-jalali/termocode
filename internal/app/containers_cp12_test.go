package app

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
	"github.com/amin-jalali/termocode/internal/theme"
)

// CP-12: the "/" filter, remote tags, Docker Hub search, podman
// connections and the "Open in Databases" link from CONTAINERS rows.

func typeKeys(m *Model, keys ...string) []tea.Cmd {
	var cmds []tea.Cmd
	for _, k := range keys {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case " ":
			msg = tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
		}
		next, cmd := m.handleServicesSidebarKey(msg)
		*m = next
		cmds = append(cmds, cmd)
	}
	return cmds
}

// promptValue is what Enter would submit on the open prompt.
func promptValue(m *Model) string {
	_, cmd := m.prompt.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		return ""
	}
	if s, ok := cmd().(prompt.SubmitMsg); ok {
		return s.Value
	}
	return ""
}

func filterDemo(t *testing.T) *Model {
	t.Helper()
	withConfigDir(t)
	m0 := newTestModel(t)
	m0.w, m0.h = 120, 60
	m := &m0
	m.containersDemo("")
	m.services.visible = true
	for _, s := range hostSectionOrder {
		m.services.collapsed[hostSection(s)] = false
	}
	m.services.touch()
	return m
}

func rowNames(st *servicesState) []string {
	var out []string
	for _, r := range st.rows() {
		switch r.kind {
		case svcRowSection:
			out = append(out, "#"+strings.TrimPrefix(r.section, "host/"))
		case svcRowHint, svcRowNote:
			out = append(out, "!"+r.text)
		default:
			out = append(out, r.name)
		}
	}
	return out
}

func TestContainersFilterNarrowsEverySection(t *testing.T) {
	m := filterDemo(t)
	st := m.services
	all := strings.Join(rowNames(st), ",")
	for _, c := range typeKeys(m, "/", "p", "o", "s") {
		if c != nil {
			t.Fatal("typing the filter returned a command")
		}
	}
	got := rowNames(st)
	joined := strings.Join(got, ",")
	for _, want := range []string{"postgres", "postgres-1", "16", "15-alpine"} {
		if !containsString(got, want) {
			t.Errorf("filter pos: no %q row in %s", want, joined)
		}
	}
	for _, gone := range []string{"redis", "redis-1", "nginx", "builder", "bridge", "cache", "prod", "ghcr.io"} {
		if containsString(got, gone) {
			t.Errorf("filter pos: %q still shown in %s", gone, joined)
		}
	}
	// Every section header stays (Tab still finds them).
	for _, h := range []string{"#compose", "#containers", "#images", "#registries", "#networks", "#volumes", "#contexts", "#help"} {
		if !containsString(got, h) {
			t.Errorf("header %s gone: %s", h, joined)
		}
	}
	lines := plainLines(m, 40, 60)
	if !strings.Contains(lines[0], "/pos") {
		t.Errorf("title row does not show the filter: %q", lines[0])
	}
	for sec, want := range map[string]string{"CONTAINERS": "1/8", "IMAGES": "2/4", "NETWORKS": "0/3", "VOLUMES": "0/2", "PROJECT": "1/4"} {
		if l := strings.TrimRight(lineWith(lines[1:], sec+" "), " "); !strings.HasSuffix(l, " "+want) {
			t.Errorf("%s header = %q, want count %s", sec, l, want)
		}
	}
	// The cursor sits on a match.
	if r, ok := m.servicesCurrentRow(); !ok || !st.rowMatches(r, "pos") {
		t.Errorf("cursor row = %+v, want a match", r)
	}
	// Enter keeps the filter and gives the keys back: j moves.
	typeKeys(m, "enter")
	if st.filterTyping || st.filter != "pos" {
		t.Fatalf("after Enter: typing=%v filter=%q", st.filterTyping, st.filter)
	}
	c0 := st.cursor
	typeKeys(m, "j")
	if st.filter != "pos" || st.cursor == c0 {
		t.Errorf("j after Enter: filter=%q cursor %d→%d", st.filter, c0, st.cursor)
	}
	if strings.Contains(plainLines(m, 40, 60)[0], "▏") {
		t.Error("caret shown after Enter")
	}
	// Esc clears: the rows come back.
	typeKeys(m, "esc")
	if st.filter != "" || strings.Join(rowNames(st), ",") != all {
		t.Errorf("Esc did not restore the rows:\n%s\nwant\n%s", strings.Join(rowNames(st), ","), all)
	}
	if m.focus == FocusEditor && false {
		t.Error("unreachable")
	}
}

func TestContainersFilterTypingEditsAndEscClears(t *testing.T) {
	m := filterDemo(t)
	st := m.services
	typeKeys(m, "/", "w", "o", "r", "k", "x", "backspace")
	if st.filter != "work" || !st.filterTyping {
		t.Fatalf("filter = %q typing=%v", st.filter, st.filterTyping)
	}
	// Folded groups are looked into: shop is folded in the demo.
	got := rowNames(st)
	if !containsString(got, "shop") || !containsString(got, "worker-1") || containsString(got, "web-1") {
		t.Errorf("folded group not searched: %s", strings.Join(got, ","))
	}
	// Letters that are keys elsewhere are text while typing.
	typeKeys(m, "D", "x", "s")
	if st.filter != "workDxs" || m.confirmOpen {
		t.Fatalf("keys were not typed: filter=%q confirm=%v", st.filter, m.confirmOpen)
	}
	// Nothing matches: one note says so; the headers stay.
	got = rowNames(st)
	if len(got) == 0 || !strings.HasPrefix(got[0], "!No rows match \"workDxs\"") {
		t.Errorf("no-match note missing: %s", strings.Join(got, ","))
	}
	typeKeys(m, "esc")
	if st.filter != "" || st.filterTyping {
		t.Fatalf("Esc while typing: filter=%q typing=%v", st.filter, st.filterTyping)
	}
	// Esc with no filter still leaves the view (the old behaviour).
	m.focus = FocusExplorer
	typeKeys(m, "esc")
	if m.focus != FocusEditor {
		t.Error("Esc without a filter did not give the focus back to the editor")
	}
}

// "/" and the next letters can arrive in one key message (fast typing,
// a paste): the letters are the filter text.
func TestContainersFilterSlashWithLettersInOneKey(t *testing.T) {
	m := filterDemo(t)
	next, _ := m.handleServicesSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/ngin")})
	*m = next
	if !m.services.filterTyping || m.services.filter != "ngin" {
		t.Fatalf("typing=%v filter=%q", m.services.filterTyping, m.services.filter)
	}
}

// A folded section keeps its fold, but its header counts the matches.
func TestContainersFilterCountsFoldedSections(t *testing.T) {
	m := filterDemo(t)
	st := m.services
	st.collapsed[hostSection("networks")] = true
	st.touch()
	typeKeys(m, "/", "b", "r", "i")
	lines := plainLines(m, 40, 60)
	if l := strings.TrimRight(lineWith(lines, "NETWORKS"), " "); !strings.HasSuffix(l, " 1/3") {
		t.Errorf("folded NETWORKS header = %q, want 1/3", l)
	}
	for _, r := range st.rows() {
		if r.host == hostNetwork {
			t.Errorf("folded section shows a row: %+v", r)
		}
	}
}

// Typing never calls the engine; the filter uses the snapshot.
func TestContainersFilterRunsNoEngineCalls(t *testing.T) {
	m, f := ctxSession(t)
	time.Sleep(50 * time.Millisecond)
	before := len(f.Calls())
	for _, c := range typeKeys(m, "/", "d", "e", "f", "backspace", "enter", "/", "x", "esc") {
		if c != nil {
			t.Fatal("a filter key returned a command")
		}
	}
	_ = plainLines(m, 40, 80)
	time.Sleep(50 * time.Millisecond)
	if calls := f.Calls()[before:]; len(calls) != 0 {
		t.Errorf("engine called while filtering: %q", calls)
	}
}

// Tab jumps between the headers of the filtered rows, and a click hits
// the filtered row under the mouse.
func TestContainersFilterTabJumpAndMouse(t *testing.T) {
	m := filterDemo(t)
	st := m.services
	typeKeys(m, "/", "r", "e", "d", "i", "s", "enter")
	rows := st.rows()
	var headers []int
	for i, r := range rows {
		if r.kind == svcRowSection {
			headers = append(headers, i)
		}
	}
	seen := map[int]bool{}
	for i := 0; i < len(headers); i++ {
		typeKeys(m, "tab")
		if st.rows()[st.cursor].kind != svcRowSection {
			t.Fatalf("Tab landed on %+v", st.rows()[st.cursor])
		}
		seen[st.cursor] = true
	}
	if len(seen) != len(headers) {
		t.Errorf("Tab visited %d of %d headers", len(seen), len(headers))
	}
	// Tab works while typing too.
	typeKeys(m, "/", "tab")
	if !st.filterTyping || st.rows()[st.cursor].kind != svcRowSection {
		t.Error("Tab while typing did not jump")
	}
	typeKeys(m, "enter")
	// Click the redis-1 row.
	m.explorerWidth = 41
	lines := plainLines(m, 40, 60)
	y := -1
	for i, l := range lines {
		if strings.Contains(l, "redis-1") {
			y = i
		}
	}
	if y < 0 {
		t.Fatalf("no redis-1 line:\n%s", strings.Join(lines, "\n"))
	}
	next, _ := m.handleServicesSidebarMouse(5, y, tea.MouseLeft)
	*m = next
	if r, _ := m.servicesCurrentRow(); r.name != "redis-1" {
		t.Errorf("click selected %+v, want redis-1", r)
	}
}

// ── Remote tags ──────────────────────────────────────────────────────────

const appTestToken = "app-TOKEN-must-not-show"

// tagServer is a TLS registry with the anonymous token exchange; private
// repositories answer 401 even with the token.
func tagServer(t *testing.T, tags []string) (*httptest.Server, string) {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			fmt.Fprintf(w, `{"token":%q}`, appTestToken)
			return
		}
		if strings.Contains(r.URL.Path, "/private/") || r.Header.Get("Authorization") != "Bearer "+appTestToken {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+srv.URL+`/token",service="test"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		b, _ := json.Marshal(map[string]any{"name": "x", "tags": tags})
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	old := newRegistryAPI
	newRegistryAPI = func() *containers.RegistryAPI {
		return &containers.RegistryAPI{RootCAs: pool, Timeout: 5 * time.Second}
	}
	t.Cleanup(func() { newRegistryAPI = old })
	u, _ := url.Parse(srv.URL)
	return srv, u.Host
}

// runServicesCmds runs a command tree and feeds the services answers back.
func runServicesCmds(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for n := 0; len(queue) > 0 && n < 50; n++ {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		done := make(chan tea.Msg, 1)
		go func() { done <- c() }()
		var msg tea.Msg
		select {
		case msg = <-done:
		case <-time.After(1500 * time.Millisecond):
			continue // a timer (a toast's expiry): not a services answer
		}
		if b, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, b...)
			continue
		}
		if next, ok := m.handleServicesMsg(msg); ok {
			queue = append(queue, next)
		}
	}
}

func TestContainersImageRemoteTags(t *testing.T) {
	_, host := tagServer(t, []string{"1.0", "1.1", "latest"})
	m := filterDemo(t)
	st := m.services
	st.imageRows = append(st.imageRows, containers.Image{ID: "sha256:9999", RepoTags: []string{host + "/acme/api:1.0"}, Size: "10MB"})
	st.touch()
	if !m.servicesCursorTo(func(r svcRow) bool { return r.kind == svcRowGroup && r.name == host+"/acme/api" }) {
		t.Fatal("no image group")
	}
	cmds := typeKeys(m, "b")
	if cmds[0] == nil {
		t.Fatal("b did not load the tags")
	}
	if got := strings.Join(rowNames(st), ","); !strings.Contains(got, "remote tags,!      Loading tags…") {
		t.Errorf("no loading row: %s", got)
	}
	runServicesCmds(t, m, cmds[0])
	got := rowNames(st)
	i := 0
	for ; i < len(got) && got[i] != "remote tags"; i++ {
	}
	if i+3 >= len(got) || got[i+1] != "1.0" || got[i+2] != "1.1" || got[i+3] != "latest" {
		t.Fatalf("remote tags not under the repo: %s", strings.Join(got, ","))
	}
	out := ansi.Strip(m.renderServicesSidebarRaw(40, 60, true, svcTestNow, -1, -1))
	if strings.Contains(out, appTestToken) {
		t.Fatal("the registry token shows on screen")
	}
	if l := strings.TrimRight(lineWith(strings.Split(out, "\n"), "remote tags"), " "); !strings.HasSuffix(l, " 3") {
		t.Errorf("remote header = %q, want the count 3", l)
	}
	// Enter on a tag opens the existing pull prompt with the full name.
	m.servicesCursorTo(func(r svcRow) bool { return r.host == hostRemoteTag && r.tag == "1.1" })
	typeKeys(m, "enter")
	if !m.promptOpen || m.promptKind != promptKindContainerPull || promptValue(m) != host+"/acme/api:1.1" {
		t.Fatalf("pull prompt open=%v kind=%v value=%q", m.promptOpen, m.promptKind, promptValue(m))
	}
	m.promptOpen = false
	// Space folds the list; b on the group closes it too.
	m.servicesCursorTo(func(r svcRow) bool { return r.host == hostRemoteRepo })
	typeKeys(m, " ")
	if containsString(rowNames(st), "1.1") {
		t.Error("Space did not fold the remote tags")
	}
	// The filter looks into remote tags.
	typeKeys(m, " ", "/", "l", "a", "t")
	if got := rowNames(st); !containsString(got, "latest") || containsString(got, "1.1") {
		t.Errorf("filter over remote tags: %s", strings.Join(got, ","))
	}
}

func TestContainersRemoteTagsPrivateNote(t *testing.T) {
	_, host := tagServer(t, nil)
	m := filterDemo(t)
	st := m.services
	cmd := m.browseRemote(host+"/private/app", false)
	runServicesCmds(t, m, cmd)
	got := strings.Join(rowNames(st), ",")
	if !strings.Contains(got, host+"/private/app,!  needs sign-in; pull by name still works") {
		t.Errorf("private note missing: %s", got)
	}
	// The cursor went to the browsed repository in REGISTRIES; u pulls by name.
	r, _ := m.servicesCurrentRow()
	if r.host != hostRemoteRepo || r.hostSectionID() != "registries" {
		t.Fatalf("cursor = %+v", r)
	}
	typeKeys(m, "u")
	if !m.promptOpen || promptValue(m) != host+"/private/app" {
		t.Errorf("pull prompt value = %q", promptValue(m))
	}
	m.promptOpen = false
	// x removes it from the list.
	typeKeys(m, "x")
	if strings.Contains(strings.Join(rowNames(st), ","), "private/app") {
		t.Error("x did not remove the browsed repository")
	}
}

// b in REGISTRIES asks for a repository; a bad one never reaches the
// network.
func TestContainersBrowsePromptFromRegistries(t *testing.T) {
	_, host := tagServer(t, []string{"v1"})
	m := filterDemo(t)
	m.servicesCursorTo(func(r svcRow) bool { return r.kind == svcRowSection && r.section == hostSection("registries") })
	typeKeys(m, "b")
	if !m.promptOpen || m.promptKind != promptKindContainerBrowse {
		t.Fatalf("browse prompt open=%v kind=%v", m.promptOpen, m.promptKind)
	}
	if cmd := m.onRemotePrompt(promptKindContainerBrowse, "../etc/passwd"); cmd == nil || len(m.services.remoteTags) != 0 {
		t.Error("a bad repository was accepted")
	}
	runServicesCmds(t, m, m.onRemotePrompt(promptKindContainerBrowse, host+"/acme/web"))
	if got := strings.Join(rowNames(m.services), ","); !strings.Contains(got, host+"/acme/web,v1") {
		t.Errorf("browsed repo not listed: %s", got)
	}
}

// Hiding the view cancels a running tag request; the list asks again on
// the next open and a late answer is dropped.
func TestContainersRemoteTagsCancelledWhenHidden(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	old := newRegistryAPI
	newRegistryAPI = func() *containers.RegistryAPI { return &containers.RegistryAPI{RootCAs: pool} }
	defer func() { newRegistryAPI = old }()
	u, _ := url.Parse(srv.URL)
	m := filterDemo(t)
	st := m.services
	cmd := m.browseRemote(u.Host+"/slow/app", false)
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	st.cancelHostWork()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("hiding did not cancel the registry request")
	}
	m.handleServicesMsg(msg)
	var rt *remoteTags
	for _, v := range st.remoteTags {
		rt = v
	}
	if rt.loading || rt.loaded || rt.err != "" {
		t.Errorf("after hide: %+v (it must ask again next time)", rt)
	}
}

// ── Search ───────────────────────────────────────────────────────────────

func TestContainersSearchPickPullAndBrowse(t *testing.T) {
	m := filterDemo(t)
	f := hostFake(t)
	b, err := os.ReadFile(filepath.Join("..", "containers", "testdata", "docker27", "search.json"))
	if err != nil {
		t.Fatal(err)
	}
	f.Set(t, "search.json", string(b))
	m.services.containerEngine.Binary = f.Docker()
	m.servicesCursorTo(func(r svcRow) bool { return r.kind == svcRowSection && r.section == hostSection("registries") })
	typeKeys(m, "s")
	if !m.promptOpen || m.promptKind != promptKindContainerSearch {
		t.Fatalf("search prompt open=%v kind=%v", m.promptOpen, m.promptKind)
	}
	m.promptOpen = false
	if cmd := m.onRemotePrompt(promptKindContainerSearch, "-rm"); cmd == nil || f.Count("search") != 0 {
		t.Fatal("a flag-like term reached the engine")
	}
	runServicesCmds(t, m, m.onRemotePrompt(promptKindContainerSearch, "nginx"))
	if c := callsWithPrefix(f.Calls(), "search"); len(c) != 1 || c[0] != "search --format {{json .}} --limit 25 --no-trunc nginx" {
		t.Fatalf("search argv = %q", c)
	}
	if !m.pickerOpen || m.pickerKind != pickerKindContainerSearch {
		t.Fatal("no result picker")
	}
	view := ansi.Strip(m.picker.View())
	if !strings.Contains(view, "bitnami/nginx") || strings.Contains(view, "\x1b[31m") {
		t.Errorf("picker:\n%s", view)
	}
	m.pickerOpen = false
	m.onSearchPicked("0")
	if !m.confirmOpen || m.confirmKind != confirmKindContainerSearchResult {
		t.Fatal("no result card")
	}
	card := ansi.Strip(m.confirm.View())
	if !strings.Contains(card, "Browse tags") || !strings.Contains(card, "Pull") {
		t.Errorf("card:\n%s", card)
	}
	m.confirmOpen = false
	if cmd, ok := m.handleServicesConfirm("pull"); !ok || cmd != nil || !m.promptOpen || m.promptKind != promptKindContainerPull || promptValue(m) != "nginx" {
		t.Fatalf("pull: prompt open=%v kind=%v value=%q", m.promptOpen, m.promptKind, promptValue(m))
	}
	if n := f.Count("pull"); n != 0 {
		t.Error("pull ran before the prompt was answered")
	}
}

func TestContainersSearchErrorToast(t *testing.T) {
	m := filterDemo(t)
	f := hostFake(t)
	f.Set(t, "search.err", "Error response from daemon: unauthorized: \x1b[31mrate limit")
	m.services.containerEngine.Binary = f.Docker()
	runServicesCmds(t, m, m.onRemotePrompt(promptKindContainerSearch, "redis"))
	if m.pickerOpen {
		t.Error("picker opened on a failed search")
	}
	text := ansi.Strip(regToast(m))
	if !strings.Contains(text, "Search failed") || !strings.Contains(text, "rate limit") || strings.Contains(text, "[31m") {
		t.Errorf("toast = %q (the engine's error, without escapes)", text)
	}
}

// ── Podman connections ───────────────────────────────────────────────────

// podSession is a Containers view on a fake podman (no docker on PATH)
// with two connections.
func podSession(t *testing.T) (*Model, *servicestest.Fake) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("CONTAINER_CONNECTION", "")
	t.Setenv("DOCKER_CONTEXT", "")
	f := hostFake(t)
	f.InstallPodman(t)
	if err := os.Remove(f.Docker()); err != nil {
		t.Fatal(err)
	}
	// Only the fake podman and the tools its script runs: never a real
	// docker or podman from the machine's PATH.
	tools := t.TempDir()
	for _, name := range []string{"cat", "sleep", "tail", "touch"} {
		p, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(p, filepath.Join(tools, name)); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", f.Bin+string(os.PathListSeparator)+tools)
	b, err := os.ReadFile(filepath.Join("..", "containers", "testdata", "podman5", "connection-ls.json"))
	if err != nil {
		t.Fatal(err)
	}
	f.Set(t, "connection-ls.json", string(b))
	v, err := os.ReadFile(filepath.Join("..", "containers", "testdata", "podman5", "version.json"))
	if err != nil {
		t.Fatal(err)
	}
	f.Set(t, "version.json", string(v))
	st := svcTestState()
	st.root = t.TempDir()
	st.sections = map[string]bool{}
	st.visible = true
	for _, s := range hostSectionOrder {
		st.collapsed[hostSection(s)] = false
	}
	m := svcTestModel(st, 100, 80)
	pumpHostUntil(t, &m, m.containersDetect(), contextsLoaded(&m)) // detect is slow on a busy runner
	if st.containerEngine.Kind != "podman" || !st.containerEngine.Reachable || len(st.contextRows) != 2 {
		t.Fatalf("setup: engine=%+v contexts=%v", st.containerEngine, st.contextRows)
	}
	return &m, f
}

func TestPodmanConnectionsAreContexts(t *testing.T) {
	m, f := podSession(t)
	st := m.services
	got := strings.Join(rowNames(st), ",")
	if !strings.Contains(got, "#contexts") || !strings.Contains(got, "podman-machine-default") || !strings.Contains(got, "build-box") {
		t.Fatalf("connections not listed as CONTEXTS: %s", got)
	}
	if c := callsWithPrefix(f.ConnCalls(), "|system connection ls"); len(c) == 0 {
		t.Errorf("no `system connection ls` call: %q", f.ConnCalls())
	}
	// Enter on a connection: every podman call carries it from now on.
	before := len(f.ConnCalls())
	selectContext(t, st, "build-box")
	pumpHost(t, m, press(m, "enter"))
	if st.sessionContext != "build-box" {
		t.Fatalf("session connection = %q", st.sessionContext)
	}
	calls := f.ConnCalls()[before:]
	seen := map[string]bool{}
	for _, c := range calls {
		argv := c[strings.IndexByte(c, '|')+1:]
		if strings.HasPrefix(argv, "system connection") {
			if !strings.HasPrefix(c, "|") {
				t.Errorf("connection listing carried the session connection: %q", c)
			}
			continue
		}
		if !strings.HasPrefix(c, "build-box|") {
			t.Errorf("podman call without CONTAINER_CONNECTION=build-box: %q", c)
		}
		for _, sub := range []string{"version", "ps", "images", "events"} {
			if strings.HasPrefix(argv, sub) {
				seen[sub] = true
			}
		}
	}
	for _, sub := range []string{"version", "ps", "images", "events"} {
		if !seen[sub] {
			t.Errorf("no %q call after the switch: %q", sub, calls)
		}
	}
	for _, c := range f.CtxCalls() {
		if !strings.HasPrefix(c, "|") {
			t.Errorf("a podman call carried DOCKER_CONTEXT: %q", c)
		}
	}
	// A visible tab gets the connection too.
	if env := st.contextEnv(); env["CONTAINER_CONNECTION"] != "build-box" || env["DOCKER_CONTEXT"] != nil {
		t.Errorf("tab env = %v", env)
	}
	if l := lineWith(plainLines(m, 40, 80), "podman 5"); !strings.Contains(l, "build-box") {
		t.Errorf("engine line = %q", l)
	}
}

func TestPodmanMakeDefaultCard(t *testing.T) {
	m, f := podSession(t)
	st := m.services
	selectContext(t, st, "build-box")
	if cmd := press(m, "D"); cmd != nil || !m.confirmOpen {
		t.Fatal("D did not open the card first")
	}
	card := strings.Join(strings.Fields(ansi.Strip(m.confirm.View())), " ")
	if !strings.Contains(card, "system connection default build-box") || !strings.Contains(card, "default connection") {
		t.Fatalf("card = %s", card)
	}
	if n := len(callsWithPrefix(f.ConnCalls(), "|system connection default")); n != 0 {
		t.Fatal("ran before the card was answered")
	}
	pumpHost(t, m, m.handleConfirmSelect("use"))
	if c := callsWithPrefix(f.ConnCalls(), "|system connection default"); len(c) != 1 || c[0] != "|system connection default build-box" {
		t.Fatalf("default calls = %q", c)
	}
	if st.sessionContext != "" {
		t.Errorf("make default changed the session connection to %q", st.sessionContext)
	}
}

// A remote connection that fails: the note names the connection; the
// CONTEXTS rows still let the user go back.
func TestPodmanRemoteConnectionFailureNote(t *testing.T) {
	m, f := podSession(t)
	st := m.services
	f.Set(t, "fail-conn-build-box.err", "Error: unable to connect to Podman socket: ssh: handshake failed")
	selectContext(t, st, "build-box")
	pumpHost(t, m, press(m, "enter"))
	got := strings.Join(rowNames(st), ",")
	if !strings.Contains(got, "!connection build-box:") || !strings.Contains(got, "build-box") {
		t.Fatalf("no failure note: %s", got)
	}
	if !strings.Contains(got, "podman-machine-default") {
		t.Errorf("connections not listed while the remote fails: %s", got)
	}
}

// The AI cannot switch, make default, search or browse: every new
// palette id is never.
func TestCP12PaletteIDsAreNeverForAI(t *testing.T) {
	titles := map[string]string{}
	for _, it := range servicesPaletteItems() {
		titles[it.ID] = it.Title
	}
	for _, id := range []string{"containers-search", "containers-browse-tags", "containers-filter", "containers-context-use", "containers-context-default"} {
		if titles[id] == "" {
			t.Errorf("%s is not in the palette", id)
		}
		p, ok := aiActionPolicies[id]
		if !ok || p.Gate != aiGateNever {
			t.Errorf("%s policy = %+v (ok %v), want never", id, p, ok)
		}
	}
}

// ── Open in Databases from CONTAINERS (DB-12, verified for CP-12) ────────

func TestContainersRowOpenInDatabases(t *testing.T) {
	m := filterDemo(t)
	st := m.services
	if !m.servicesCursorTo(func(r svcRow) bool { return r.host == hostContainer && r.hostID == "9f3c2e1a" }) {
		t.Fatal("no postgres container row")
	}
	r, _ := m.servicesCurrentRow()
	m.openContainerMenu(r, 10, 10)
	if !strings.Contains(ansi.Strip(m.menu.View()), "Open in Databases") {
		t.Fatal("the container menu has no Open in Databases")
	}
	m.menuOpen = false
	_ = m.handleContainerRowMenuSelect("databases", r)
	if !m.promptOpen || !strings.Contains(ansi.Strip(m.prompt.View()), "localhost:5432") {
		t.Fatalf("no add-connection prompt for the container: open=%v", m.promptOpen)
	}
	// A non-database container has no such entry.
	m.promptOpen = false
	m.servicesCursorTo(func(r svcRow) bool { return r.host == hostContainer && r.hostID == "aabbccdd" })
	r, _ = m.servicesCurrentRow()
	m.openContainerMenu(r, 10, 10)
	if strings.Contains(ansi.Strip(m.menu.View()), "Open in Databases") {
		t.Error("nginx offers Open in Databases")
	}
	_ = st
	_ = context.Background
	_ = theme.CurrentIconMode
}

// CT-15: the "ai-card" demo opens the AI container-control card with the
// name and the short id (the demo is what the golden shows).
func TestContainersDemoAICard(t *testing.T) {
	withConfigDir(t)
	m0 := newTestModel(t)
	m0.w, m0.h = 120, 40
	m := &m0
	m.containersDemo("ai-card")
	if !m.confirmOpen {
		t.Fatal("no card")
	}
	card := cardText(m)
	if !strings.Contains(card, "Stop container postgres-1 (docker stop 9f3c2e1a9f3c)") || strings.Contains(card, strings.Repeat("9f3c2e1a", 8)) {
		t.Errorf("card: %s", card)
	}
}
