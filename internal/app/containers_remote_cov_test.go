package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
)

// ctrRemoteRow is the REGISTRIES row of the browsed repo ref.
func ctrRemoteRow(t *testing.T, m *Model, host hostKind, ref string) svcRow {
	t.Helper()
	return selectRow(t, m.services, func(r svcRow) bool {
		return r.kind == svcRowHostItem && r.host == host && r.imageRef == ref
	})
}

// Browse a repo of the loopback registry: the tags load, the row keys
// pull, copy, reload, fold and forget; the menu does the same.
func TestCovContainerRemoteBrowseAndKeys(t *testing.T) {
	m, _, _ := regSession(t, regConfig)
	reg := newCtrRegistry(t, 0)
	var copied []string
	defer SetClipboardSink(func(s string) { copied = append(copied, s) })()
	ref := reg.host + "/acme/api"
	msg := ctrRun[remoteTagsMsg](t, m.onRemotePrompt(promptKindContainerBrowse, ref))
	_ = m.onRemoteTags(msg)
	r := ctrRemoteRow(t, m, hostRemoteRepo, ref)
	if l1, l2 := m.services.remoteFooter(r); !strings.Contains(l1, "acme/api") || !strings.Contains(l2, "remote tags") {
		t.Fatalf("footer %q %q", l1, l2)
	}
	tag := ctrRemoteRow(t, m, hostRemoteTag, ref+":1.0")
	if _, l2 := m.services.remoteFooter(tag); !strings.Contains(l2, "remote tag") {
		t.Fatal(l2)
	}
	for _, k := range []string{"u", "enter"} {
		m.promptOpen = false
		if _, ok := m.remoteRowKey(k, tag); !ok || !m.promptOpen || m.promptKind != promptKindContainerPull {
			t.Fatalf("%s on a tag: no pull prompt", k)
		}
	}
	if _, ok := m.remoteRowKey("y", tag); !ok || len(copied) != 1 || copied[0] != ref+":1.0" {
		t.Fatalf("copied %v", copied)
	}
	if cmd, ok := m.remoteRowKey("r", r); !ok || cmd == nil {
		t.Fatal("r: no reload")
	}
	if _, ok := m.remoteRowKey("zz", r); ok {
		t.Fatal("unknown key")
	}
	// Menu.
	m.openRemoteMenu(tag, 3, 3)
	m.openRemoteMenu(r, 3, 3)
	if !m.menuOpen {
		t.Fatal("no menu")
	}
	_ = m.handleRemoteMenuSelect("copy", tag)
	m.promptOpen = false
	_ = m.handleRemoteMenuSelect("pull", tag)
	if !m.promptOpen || len(copied) != 2 {
		t.Fatal("menu pull/copy")
	}
	if m.handleRemoteMenuSelect("reload", r) == nil {
		t.Fatal("menu reload")
	}
	_ = m.handleRemoteMenuSelect("delete-tag", tag)
	if !strings.Contains(ctrToast(m), "needs a login") {
		t.Fatalf("delete-tag toast %s", ctrToast(m))
	}
	if m.handleRemoteMenuSelect("dance", r) != nil || !isRemoteHostKind(hostRemoteTag) || isRemoteHostKind(hostImage) {
		t.Fatal("menu other")
	}
	_, _ = m.remoteRowKey("D", tag)
	// Fold, then forget from the menu and the key.
	m.services.remoteTags[ref].loading = false
	_ = m.toggleRemote(r)
	if m.services.remoteTags[ref].open {
		t.Fatal("not folded")
	}
	_ = m.handleRemoteMenuSelect("forget", r)
	if _, ok := m.services.remoteTags[ref]; ok {
		t.Fatal("not forgotten")
	}
	if m.toggleRemote(r) != nil {
		t.Fatal("toggle a forgotten repo")
	}
	_, _ = m.remoteRowKey("x", r)
	if l1, _ := m.services.remoteFooter(r); l1 != "" {
		t.Fatal("footer of a forgotten repo")
	}
	if m.onRemotePrompt(promptKindContainerBrowse, "  ") != nil || m.onRemotePrompt(promptKindContainerTag, "x") != nil {
		t.Fatal("empty browse / other prompt")
	}
}

// The tag answers: not found, needs sign-in (with a saved token the
// opt-in card), another error, a stale answer and no tags.
func TestCovContainerRemoteTagAnswers(t *testing.T) {
	m, _, _ := regSession(t, regConfig)
	reg := newCtrRegistry(t, 0)
	ref := reg.host + "/acme/api"
	start := func() remoteTagsMsg {
		m.services.remoteTags = nil
		return ctrRun[remoteTagsMsg](t, m.browseRemote(ref, false))
	}
	msg := start()
	msg.err = containers.ErrRepoNotFound
	_ = m.onRemoteTags(msg)
	if rt := m.services.remoteTags[ref]; !strings.Contains(rt.err, "not found on") {
		t.Fatalf("%+v", rt)
	}
	r := ctrRemoteRow(t, m, hostRemoteRepo, ref)
	if _, l2 := m.services.remoteFooter(r); !strings.Contains(l2, "not found") {
		t.Fatal(l2)
	}
	msg = start()
	msg.err = errors.New("tls: bad\nmore")
	_ = m.onRemoteTags(msg)
	if rt := m.services.remoteTags[ref]; rt.err != "tls: bad" {
		t.Fatalf("%+v", rt)
	}
	msg = start()
	if _, l2 := m.services.remoteFooter(ctrRemoteRow(t, m, hostRemoteRepo, ref)); l2 != "asking the registry…" {
		t.Fatal(l2)
	}
	msg.gen++
	_ = m.onRemoteTags(msg)
	if rt := m.services.remoteTags[ref]; rt.loaded {
		t.Fatal("stale answer applied")
	}
	if m.onRemoteTags(msg) != nil {
		t.Fatal("answer for a list that is not loading")
	}
	msg = start()
	msg.list = containers.TagList{}
	_ = m.onRemoteTags(msg)
	if !strings.Contains(svcHintText(m.services), "no tags") {
		t.Fatalf("hints %s", svcHintText(m.services))
	}
	// A saved token: the opt-in card.
	ctrSaveToken(t, m, reg.host, ctrRegToken)
	msg = start()
	msg.err = containers.ErrNeedsSignIn
	_ = m.onRemoteTags(msg)
	if !m.confirmOpen || m.confirmKind != confirmKindRegistryAuth {
		t.Fatal("no opt-in card")
	}
	// Loading again while one runs cancels the first and asks anew.
	_ = m.loadRemoteTags(ref)
	if m.loadRemoteTags(ref) == nil || len(m.services.containerCancels) == 0 {
		t.Fatal("second load")
	}
	m.services.visible = false
	m.services.remoteTags[ref].loading = false
	if m.loadRemoteTags(ref) != nil {
		t.Fatal("hidden view loaded")
	}
	saved := m.services
	m.services = nil
	if m.onRemoteTags(msg) != nil || m.onContainerSearch(containerSearchMsg{}) != nil {
		t.Fatal("nil services")
	}
	m.services = saved
}

// Search: the prompt checks the term, the fake docker answers, the
// result card pulls or browses; no results and errors toast.
func TestCovContainerRemoteSearch(t *testing.T) {
	m, f, _ := regSession(t, regConfig)
	f.Set(t, "search.json", `{"Name":"nginx","Description":"","StarCount":"10","IsOfficial":"[OK]"}`)
	sel := selectRow(t, m.services, func(r svcRow) bool { return r.kind == svcRowSection && r.section == hostSection("registries") })
	if _, ok := m.remoteRowKey("f", sel); !ok || !m.promptOpen || m.promptKind != promptKindContainerSearch {
		t.Fatal("no search prompt")
	}
	m.promptOpen = false
	if _, ok := m.remoteRowKey("b", sel); !ok || !m.promptOpen || m.promptKind != promptKindContainerBrowse {
		t.Fatal("no browse prompt")
	}
	_ = m.onRemotePrompt(promptKindContainerSearch, "bad term!")
	if !strings.Contains(ctrToast(m), "Cannot search") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	res := ctrRun[containerSearchMsg](t, m.onRemotePrompt(promptKindContainerSearch, "nginx"))
	_ = m.runSearch("nginx") // a second search cancels the first
	_ = m.onContainerSearch(res)
	if !m.pickerOpen || m.pickerKind != pickerKindContainerSearch {
		t.Fatalf("no results picker: %+v", res)
	}
	if m.onSearchPicked("x") != nil || m.onSearchPicked("9") != nil {
		t.Fatal("bad pick")
	}
	_ = m.onSearchPicked("0")
	if !m.confirmOpen || !strings.Contains(m.confirm.View(), "No description.") || !strings.Contains(m.confirm.View(), "official image") {
		t.Fatalf("card %s", m.confirm.View())
	}
	m.promptOpen = false
	_ = m.answerSearchResult("pull")
	if !m.promptOpen {
		t.Fatal("pull: no prompt")
	}
	if m.answerSearchResult("tags") != nil {
		t.Fatal("no pick left: browsed")
	}
	_ = m.onSearchPicked("0")
	if m.answerSearchResult("tags") == nil {
		t.Fatal("tags: no browse")
	}
	_ = m.onContainerSearch(containerSearchMsg{term: "zzz"})
	if !strings.Contains(ctrToast(m), "No results for zzz") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	t.Setenv("PATH", t.TempDir())
	m.services.containerEngine = containers.Engine{}
	_ = m.runSearch("nginx")
	if !strings.Contains(ctrToast(m), "No container engine") {
		t.Fatalf("toast %s", ctrToast(m))
	}
}

// b on an IMAGES group or tag row browses its repo; the dangling group
// cannot; b again folds it; the palette falls back to the prompt.
func TestCovContainerRemoteFromImages(t *testing.T) {
	m, _ := ctrModel(t)
	ctrImageRow(t, m)
	r, _ := m.servicesCurrentRow()
	if repo, ok := m.imageBrowseTarget(r); !ok || repo != "postgres" {
		t.Fatalf("target %q", repo)
	}
	if _, ok := m.imageBrowseTarget(svcRow{kind: svcRowHostItem, host: hostImage, imageRef: "<none>:<none>"}); ok {
		t.Fatal("dangling tag")
	}
	if _, ok := m.imageBrowseTarget(svcRow{kind: svcRowGroup, section: "grp/images/" + danglingRepo}); ok {
		t.Fatal("dangling group")
	}
	if repo, ok := m.imageBrowseTarget(svcRow{kind: svcRowGroup, section: "grp/images/redis"}); !ok || repo != "redis" {
		t.Fatal("group target")
	}
	if m.browseFromPalette() == nil {
		t.Fatal("palette on an image row: no browse")
	}
	key := remoteKey("postgres")
	m.services.remoteTags[key].open = true
	if _, ok := m.remoteRowKey("b", r); !ok || m.services.remoteTags[key].open {
		t.Fatal("b did not fold")
	}
	m.services.cursor = 0
	m.promptOpen = false
	_ = m.browseFromPalette()
	if !m.promptOpen || m.promptKind != promptKindContainerBrowse {
		t.Fatal("palette off a row: no prompt")
	}
	if remoteKey("Bad Repo") != "" {
		t.Fatal("bad repo key")
	}
	_ = m.browseRemote("Bad Repo", false)
	if !strings.Contains(ctrToast(m), "Not an image repository") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	if m.servicesCursorTo(func(svcRow) bool { return false }) {
		t.Fatal("cursor moved to nothing")
	}
}
