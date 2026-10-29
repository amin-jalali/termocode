package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func covSvcMenuText(m *Model) string { return ansi.Strip(m.menu.View()) }

// The REGISTRIES row menus offer what the keys do, and each item works.
func TestCovSvcRegistryMenus(t *testing.T) {
	m, f, _ := regSession(t, regConfig)
	st := m.services
	ghcr := selectRegistry(t, st, "ghcr.io")
	m.openRegistryMenu(ghcr, 5, 5)
	if v := covSvcMenuText(m); !strings.Contains(v, "Sign in again") || !strings.Contains(v, "Browse tags") {
		t.Errorf("host menu = %s", v)
	}
	m.handleServicesMenuSelect("pull")
	if !m.promptOpen || m.prompt.Value() != "ghcr.io/" {
		t.Errorf("pull prompt %v %q", m.promptOpen, m.prompt.Value())
	}
	m.promptOpen = false
	m.handleServicesMenuSelect("browse")
	if !m.promptOpen || m.promptKind != promptKindContainerBrowse || m.prompt.Value() != "ghcr.io/" {
		t.Errorf("browse prompt %q", m.prompt.Value())
	}
	m.promptOpen = false
	m.handleServicesMenuSelect("search")
	if !m.promptOpen || m.promptKind != promptKindContainerSearch {
		t.Error("search: no prompt")
	}
	m.promptOpen = false
	m.handleServicesMenuSelect("connect")
	if !m.promptOpen {
		t.Error("connect: no prompt")
	}
	m.promptOpen = false
	m.handleServicesMenuSelect("signout")
	if !m.confirmOpen || !strings.Contains(cardText(m), "logout ghcr.io") {
		t.Errorf("signout card = %s", cardText(m))
	}
	m.confirmOpen = false
	connect := selectRow(t, st, func(r svcRow) bool { return r.host == hostRegistryConnect })
	m.openRegistryMenu(connect, 5, 5)
	if v := covSvcMenuText(m); !strings.Contains(v, "Connect registry") {
		t.Errorf("connect menu = %s", v)
	}
	m.handleServicesMenuSelect("browse")
	if m.prompt.Value() != "" {
		t.Errorf("browse prefill = %q", m.prompt.Value())
	}
	m.promptOpen = false
	if m.handleServicesMenuSelect("nope") != nil {
		t.Error("unknown id did something")
	}
	// Keys: s searches, b browses, u on the section asks for any image.
	selectRegistry(t, st, "ghcr.io")
	press(m, "s")
	if m.promptKind != promptKindContainerSearch {
		t.Error("s: no search")
	}
	selectRegistry(t, st, "ghcr.io")
	press(m, "b")
	if m.promptKind != promptKindContainerBrowse || m.prompt.Value() != "ghcr.io/" {
		t.Errorf("b prompt = %q", m.prompt.Value())
	}
	selectRow(t, st, func(r svcRow) bool { return r.kind == svcRowSection && r.section == hostSection("registries") })
	press(m, "u")
	if m.promptKind != promptKindContainerPull || m.prompt.Value() != "" {
		t.Errorf("u prompt = %q", m.prompt.Value())
	}
	if len(f.Calls()) != 0 {
		t.Errorf("menus ran docker: %q", f.Calls())
	}
}

// Sign out from the palette with no registry selected opens the section
// and asks to pick; a host that is only a helper is not signed in.
func TestCovSvcRegistrySignOutPalette(t *testing.T) {
	m, _, _ := regSession(t, regConfig)
	st := m.services
	st.cursor = 0
	pumpHost(t, m, m.registrySignOutFromPalette())
	if !strings.Contains(regToast(m), "Pick a registry") {
		t.Errorf("toast = %q", regToast(m))
	}
	if r, ok := m.servicesCurrentRow(); !ok || r.host != hostRegistry {
		t.Errorf("cursor on %+v", r)
	}
	quay := selectRegistry(t, st, "quay.io")
	m.askRegistrySignOut(quay)
	if m.confirmOpen || !strings.Contains(regToast(m), "Not signed in to quay.io") {
		t.Errorf("quay: open=%v toast=%q", m.confirmOpen, regToast(m))
	}
	st.sections["registries"] = false
	st.cursor = 0
	m.registrySignOutFromPalette()
	if !strings.Contains(regToast(m), "REGISTRIES is hidden") {
		t.Errorf("hidden toast = %q", regToast(m))
	}
	if m.askRegistrySignOut(svcRow{idx: 99}) != nil {
		t.Error("a bad row did something")
	}
}

// A failed sign out is a toast with the engine's line.
func TestCovSvcRegistrySignOutFails(t *testing.T) {
	m, f, _ := regSession(t, regConfig)
	f.Set(t, "all.err", "logout failed: no credentials")
	selectRegistry(t, m.services, "ghcr.io")
	press(m, "D")
	m.confirmOpen = false
	pumpHost(t, m, func() tea.Cmd { c, _ := m.handleServicesConfirm("signout"); return c }())
	if !strings.Contains(regToast(m), "Could not sign out of ghcr.io") {
		t.Errorf("toast = %q", regToast(m))
	}
}

// Remote tags: browse lists them under REGISTRIES; the row keys and the
// menu reload, copy, pull and forget; a 404 and a private repo say so.
func TestCovSvcRemoteTags(t *testing.T) {
	m, _, _ := regSession(t, regConfig)
	_, host := tagServer(t, []string{"v1", "v2"})
	st := m.services
	runServicesCmds(t, m, m.onRemotePrompt(promptKindContainerBrowse, host+"/acme/web"))
	if got := strings.Join(rowNames(st), ","); !strings.Contains(got, "v1") || !strings.Contains(got, "v2") {
		t.Fatalf("rows = %s", got)
	}
	if m.onRemotePrompt(promptKindContainerBrowse, "  ") != nil {
		t.Error("empty browse did something")
	}
	repo := selectRow(t, st, func(r svcRow) bool { return r.host == hostRemoteRepo })
	var clip []string
	defer SetClipboardSink(func(s string) { clip = append(clip, s) })()
	press(m, "y")
	if len(clip) != 1 || !strings.Contains(clip[0], "acme/web") {
		t.Errorf("copy = %q", clip)
	}
	selectRow(t, st, func(r svcRow) bool { return r.host == hostRemoteRepo })
	runServicesCmds(t, m, press(m, "r"))
	selectRow(t, st, func(r svcRow) bool { return r.host == hostRemoteTag && strings.HasSuffix(r.imageRef, ":v2") })
	press(m, "enter")
	if !m.promptOpen || !strings.HasSuffix(m.prompt.Value(), ":v2") {
		t.Errorf("tag enter prompt = %q", m.prompt.Value())
	}
	m.promptOpen = false
	// The menu.
	m.openRemoteMenu(repo, 3, 3)
	if v := covSvcMenuText(m); !strings.Contains(v, "Reload tags") || !strings.Contains(v, "Remove from list") {
		t.Errorf("repo menu = %s", v)
	}
	m.services.menuRow = repo
	runServicesCmds(t, m, m.handleServicesMenuSelect("reload"))
	m.handleServicesMenuSelect("copy")
	m.handleServicesMenuSelect("pull")
	if !m.promptOpen {
		t.Error("menu pull: no prompt")
	}
	m.promptOpen = false
	tag := selectRow(t, st, func(r svcRow) bool { return r.host == hostRemoteTag })
	m.openRemoteMenu(tag, 3, 3)
	if strings.Contains(covSvcMenuText(m), "Reload") {
		t.Error("a tag row offers Reload")
	}
	m.services.menuRow = repo
	m.handleServicesMenuSelect("forget")
	if got := strings.Join(rowNames(st), ","); strings.Contains(got, "acme/web") {
		t.Errorf("forget left: %s", got)
	}
	// Not found and private.
	runServicesCmds(t, m, m.onRemotePrompt(promptKindContainerBrowse, host+"/private/app"))
	runServicesCmds(t, m, m.onRemotePrompt(promptKindContainerBrowse, "127.0.0.1:1/none/app"))
	got := strings.Join(rowNames(st), ",")
	if !strings.Contains(got, "needs sign-in") || !strings.Contains(got, "cannot reach the registry") {
		t.Errorf("private row: %s", got)
	}
}

// Search: results open a picker; a pick asks pull or tags; no results
// and a failed search are toasts; a bad term is refused.
func TestCovSvcSearch(t *testing.T) {
	m, f, _ := regSession(t, regConfig)
	b, err := os.ReadFile(filepath.Join("..", "containers", "testdata", "docker27", "search.json"))
	if err != nil {
		t.Fatal(err)
	}
	f.Set(t, "search.json", string(b))
	pumpHost(t, m, m.onRemotePrompt(promptKindContainerSearch, "nginx"))
	if !m.pickerOpen || m.pickerKind != pickerKindContainerSearch {
		t.Fatalf("no picker: %q", regToast(m))
	}
	if v := ansi.Strip(m.picker.View()); !strings.Contains(v, "bitnami/nginx") || strings.Contains(v, "\x1b[31m") {
		t.Errorf("picker = %s", v)
	}
	m.pickerOpen = false
	if m.onSearchPicked("x") != nil || m.onSearchPicked("99") != nil {
		t.Error("a bad pick did something")
	}
	m.onSearchPicked("0")
	if !m.confirmOpen || !strings.Contains(cardText(m), "official image") {
		t.Fatalf("result card = %s", cardText(m))
	}
	covSvcConfirm(t, m, "pull")
	if !m.promptOpen || m.prompt.Value() != "nginx" {
		t.Errorf("pull prompt = %q", m.prompt.Value())
	}
	m.promptOpen = false
	m.onSearchPicked("1")
	if covSvcConfirm(t, m, "tags") == nil {
		t.Error("tags gave no command")
	}
	m.onSearchPicked("1")
	if covSvcConfirm(t, m, "cancel") != nil {
		t.Error("cancel did something")
	}
	if covSvcConfirm(t, m, "pull") != nil {
		t.Error("an answer with no pick did something")
	}
	f.Set(t, "search.json", "")
	pumpHost(t, m, m.runSearch("zzznothing"))
	if !strings.Contains(regToast(m), "No results for zzznothing") {
		t.Errorf("empty toast = %q", regToast(m))
	}
	f.Set(t, "search.err", "Error response from daemon: Get https://index.docker.io: timeout")
	pumpHost(t, m, m.runSearch("nginx"))
	if !strings.Contains(regToast(m), "Search failed") {
		t.Errorf("fail toast = %q", regToast(m))
	}
	m.runSearch("-rm")
	if !strings.Contains(regToast(m), "Cannot search") {
		t.Errorf("bad term toast = %q", regToast(m))
	}
}
