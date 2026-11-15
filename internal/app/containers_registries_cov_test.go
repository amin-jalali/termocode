package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
)

// ctrRegSession is regSession with an engine detected (Binary set) and
// a session in a plain secret store.
func ctrRegSession(t *testing.T) *Model {
	t.Helper()
	m, f, _ := regSession(t, regConfig)
	m.services.containerEngine = containers.Engine{Binary: f.Docker(), Kind: "docker", Reachable: true}
	m.services.containerDetected = true
	return m
}

// The saved-token card after the user name: Use logs in with the saved
// token; a store without it asks for the token again.
func TestCovContainerRegistrySavedTokenCard(t *testing.T) {
	m := ctrRegSession(t)
	withSecretSetting(t, "plain", false)
	s, _ := m.secretStore()
	if err := s.Set(registrySecretName("ghcr.io"), "saved-tok"); err != nil {
		t.Fatal(err)
	}
	m.services.regPending = registryPending{host: "ghcr.io"}
	_ = m.onRegistryPrompt(promptKindRegistryUser, "amin")
	if !m.confirmOpen || m.confirmKind != confirmKindRegistryRemember || !strings.Contains(m.confirm.View(), "Use the saved token") {
		t.Fatal("no saved-token card")
	}
	done := ctrRun[registryLoginDoneMsg](t, m.answerRegistryRemember("use"))
	if done.err != nil || done.user != "amin" {
		t.Fatalf("%+v", done)
	}
	// The token went away: the prompt comes back with a warning.
	_ = s.Delete(registrySecretName("ghcr.io"))
	m.services.regPending = registryPending{host: "ghcr.io", user: "amin"}
	_ = m.answerRegistryRemember("use")
	if !m.promptOpen || m.promptKind != promptKindRegistryToken || !strings.Contains(ctrToast(m), "Saved token not available") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	// "new" goes to the token prompt.
	m.promptOpen = false
	if m.answerRegistryRemember("new") != nil || !m.promptOpen {
		t.Fatal("new: no prompt")
	}
	if m.registryStoreLabel() != "plain file" {
		t.Fatalf("label %q", m.registryStoreLabel())
	}
}

// With an open encrypted store the Remember card comes first; Remember
// then a good login saves what was typed; a locked store says so.
func TestCovContainerRegistryRemember(t *testing.T) {
	m := ctrRegSession(t)
	dir, _ := withSecretSetting(t, "age", false)
	writeAgeStore(t, dir)
	s, _ := configureSecretStore()
	if err := s.Unlock(secretsTestPass); err != nil {
		t.Fatal(err)
	}
	m.services.regPending = registryPending{host: "quay.io"}
	_ = m.onRegistryPrompt(promptKindRegistryUser, "amin")
	if !m.confirmOpen || !strings.Contains(m.confirm.View(), "Remember the token") || !strings.Contains(m.confirm.View(), "encrypted file") {
		t.Fatalf("no remember card: %s", m.confirm.View())
	}
	_ = m.answerRegistryRemember("remember")
	if !m.services.regPending.remember || !m.promptOpen {
		t.Fatal("remember not set")
	}
	done := ctrRun[registryLoginDoneMsg](t, m.onRegistryPrompt(promptKindRegistryToken, "typed-tok"))
	if done.err != nil || done.remember != "ok" {
		t.Fatalf("%+v", done)
	}
	if v, ok, _ := s.Get(registrySecretName("quay.io")); !ok || v != "typed-tok" {
		t.Fatalf("not saved: %q", v)
	}
	_ = m.onRegistryLoginDone(done)
	if !strings.Contains(ctrToast(m), "Token remembered in termocode") || m.services.registryUsers["quay.io"] != "amin" {
		t.Fatalf("toast %s", ctrToast(m))
	}
	// Locked between the card and the login.
	m.services.regPending = registryPending{host: "quay.io", user: "amin", remember: true}
	s.Lock()
	done = ctrRun[registryLoginDoneMsg](t, m.registryLogin("tok2"))
	if done.remember != "the store was locked" {
		t.Fatalf("%+v", done)
	}
	_ = m.onRegistryLoginDone(done)
	if !strings.Contains(ctrToast(m), "Token not remembered: the store was locked") {
		t.Fatalf("toast %s", ctrToast(m))
	}
}

// The prompts check what was typed; a login with nothing pending, a bad
// reference or no engine runs nothing.
func TestCovContainerRegistryPromptChecks(t *testing.T) {
	m := ctrRegSession(t)
	_ = m.onRegistryPrompt(promptKindRegistryHost, "bad host!")
	if !strings.Contains(ctrToast(m), "Not a registry host") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	if cmd := m.onRegistryPrompt(promptKindRegistryHost, "acme.azurecr.io"); cmd != nil || !m.confirmOpen || m.confirmKind != confirmKindRegistryCloudHint {
		t.Fatal("no cloud hint")
	}
	_ = m.onRegistryPrompt(promptKindRegistryUser, "bad user")
	if !strings.Contains(ctrToast(m), "Not a user name") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	m.services.regPending = registryPending{}
	if m.onRegistryPrompt(promptKindRegistryUser, "amin") != nil {
		t.Fatal("user with no host")
	}
	if m.onRegistryPrompt(promptKindContainerTag, "x") != nil {
		t.Fatal("other prompt")
	}
	if m.registryLogin("tok") != nil {
		t.Fatal("login with nothing pending")
	}
	m.services.regPending = registryPending{host: "ghcr.io", user: "amin"}
	_ = m.registryLogin("${nope")
	if !strings.Contains(ctrToast(m), "Not a valid reference") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	// No engine on PATH.
	t.Setenv("PATH", t.TempDir())
	m.services.containerEngine = containers.Engine{}
	m.services.regPending = registryPending{host: "ghcr.io", user: "amin"}
	_ = m.registryLogin("tok")
	if !strings.Contains(ctrToast(m), "No container engine") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	if _, ok := m.services.registryEngine(); ok {
		t.Fatal("engine without a binary")
	}
	if m.services.registryEngineName() != "docker" {
		t.Fatal("default name")
	}
}

// A ${cmd:} reference that fails gives the error in the done message.
func TestCovContainerRegistryLoginRefFails(t *testing.T) {
	m := ctrRegSession(t)
	m.services.regPending = registryPending{host: "ghcr.io", user: "amin"}
	done := ctrRun[registryLoginDoneMsg](t, m.registryLogin("${cmd:false}"))
	if done.err == nil {
		t.Fatal("no error")
	}
}

// Sign out: the checks before the card, the card, and the answer.
func TestCovContainerRegistrySignOut(t *testing.T) {
	m := ctrRegSession(t)
	st := m.services
	if m.askRegistrySignOut(svcRow{idx: 99}) != nil {
		t.Fatal("row out of range")
	}
	r := selectRegistry(t, st, "quay.io")
	_ = m.askRegistrySignOut(r)
	if !strings.Contains(ctrToast(m), "Not signed in to quay.io") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	r = selectRegistry(t, st, "ghcr.io")
	st.registryRows[r.idx].Host = "bad host!"
	_ = m.askRegistrySignOut(r)
	if !strings.Contains(ctrToast(m), "Cannot sign out") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	st.registryRows[r.idx].Host = "ghcr.io"
	// From the palette with the row selected: the card.
	_ = m.registrySignOutFromPalette()
	if !m.confirmOpen || m.pendingHostOperation != "logout" {
		t.Fatal("no sign-out card")
	}
	m.confirmOpen = false
	_ = m.onRegistrySignedOut(hostActionDoneMsg{target: "logout ghcr.io", err: errors.New("x")})
	if !strings.Contains(ctrToast(m), "Could not sign out of ghcr.io") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	// Not on a row: REGISTRIES opens and says to pick one.
	st.cursor = 0
	_ = m.registrySignOutFromPalette()
	if !strings.Contains(ctrToast(m), "Pick a registry") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	st.sections["registries"] = false
	st.cursor = 0
	st.touch()
	_ = m.registrySignOutFromPalette()
	if !strings.Contains(ctrToast(m), "REGISTRIES is hidden") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	st.sections["registries"] = true
	st.touch()
	// No engine at all.
	t.Setenv("PATH", t.TempDir())
	st.containerEngine, st.containerDetected = containers.Engine{}, false
	st.touch()
	r = selectRegistry(t, st, "ghcr.io")
	_ = m.askRegistrySignOut(r)
	if !strings.Contains(ctrToast(m), "No container engine") {
		t.Fatalf("toast %s", ctrToast(m))
	}
}

// The row menu offers each action; every pick goes to its prompt.
func TestCovContainerRegistryMenu(t *testing.T) {
	m := ctrRegSession(t)
	st := m.services
	r := selectRegistry(t, st, "ghcr.io")
	r.current = true
	m.openRegistryMenu(r, 5, 5)
	if !m.menuOpen {
		t.Fatal("no menu")
	}
	m.openRegistryMenu(svcRow{host: hostRegistryImage}, 5, 5)
	m.openRegistryMenu(svcRow{host: hostRegistryConnect}, 5, 5)
	for _, c := range []struct {
		id   string
		row  svcRow
		kind promptKindEnum
	}{
		{"connect", r, promptKindRegistryUser},
		{"pull", r, promptKindContainerPull},
		{"pull", svcRow{host: hostRegistryImage, imageRef: "ghcr.io/a/b:1"}, promptKindContainerPull},
		{"browse", r, promptKindContainerBrowse},
		{"catalog", r, promptKindRegistryCatalogHost},
	} {
		m.promptOpen = false
		_ = m.handleRegistryMenuSelect(c.id, c.row)
		if !m.promptOpen || m.promptKind != c.kind {
			t.Fatalf("%s: prompt %v kind %v", c.id, m.promptOpen, m.promptKind)
		}
	}
	m.pickerOpen = false
	_ = m.handleRegistryMenuSelect("connect", svcRow{host: hostRegistryConnect})
	if !m.pickerOpen {
		t.Fatal("connect without a host: no picker")
	}
	m.promptOpen = false
	_ = m.handleRegistryMenuSelect("search", r)
	if !m.promptOpen {
		t.Fatal("no search prompt")
	}
	_ = m.handleRegistryMenuSelect("signout", r)
	if !m.confirmOpen {
		t.Fatal("no sign-out card")
	}
	if m.handleRegistryMenuSelect("dance", r) != nil {
		t.Fatal("unknown id")
	}
	if !isRegistryHostKind(hostRegistryConnect) || isRegistryHostKind(hostVolume) {
		t.Fatal("host kinds")
	}
	if registryPullPrefix("docker.io") != "" || registryPullPrefix("ghcr.io") != "ghcr.io/" {
		t.Fatal("pull prefix")
	}
	// u on the section asks a pull with nothing prefilled.
	selectRow(t, st, func(r svcRow) bool { return r.kind == svcRowSection && r.section == hostSection("registries") })
	m.promptOpen = false
	_ = ctrKey(m, "u")
	if !m.promptOpen || m.promptKind != promptKindContainerPull {
		t.Fatal("u on the section")
	}
}

// Per-host images: the hints while not loaded and the footers.
func TestCovContainerRegistryImagesAndFooters(t *testing.T) {
	m := ctrRegSession(t)
	st := m.services
	r := selectRegistry(t, st, "ghcr.io")
	st.collapsed[registryFoldKey(r.hostID)] = false
	if !st.anyRegistryExpanded() {
		t.Fatal("expanded")
	}
	st.hostLoaded["images"] = false
	st.touch()
	if !strings.Contains(svcHintText(st), "Loading images") {
		t.Fatalf("hints: %s", svcHintText(st))
	}
	st.containerEngine.Reachable = false
	st.touch()
	if !strings.Contains(svcHintText(st), "is not running") {
		t.Fatalf("hints: %s", svcHintText(st))
	}
	if m.registryImagesLoad() != nil {
		t.Fatal("loaded from a stopped engine")
	}
	st.containerEngine.Reachable = true
	if m.registryImagesLoad() == nil {
		t.Fatal("no images load")
	}
	st.hostLoaded["images"] = true
	st.imageRows = nil
	st.touch()
	if !strings.Contains(svcHintText(st), "no local images") {
		t.Fatalf("hints: %s", svcHintText(st))
	}
	st.imageRows = []containers.Image{{ID: "sha256:1", RepoTags: []string{"ghcr.io/acme/api:1"}, Size: "9MB"}}
	st.touch()
	if _, l2 := st.registryFooter(svcRow{host: hostRegistryImage, idx: 0, imageRef: "ghcr.io/acme/api:1"}); l2 != "u pull · 9MB" {
		t.Fatalf("image footer %q", l2)
	}
	if l1, _ := st.registryFooter(svcRow{host: hostRegistry, idx: 99}); l1 != "" {
		t.Fatal("out of range")
	}
	if l1, _ := st.registryFooter(svcRow{host: hostVolume}); l1 != "" {
		t.Fatal("other kind")
	}
	st.registryRows = append(st.registryRows, containers.Registry{Host: "plain.io"}, containers.Registry{Host: "in.io", SignedIn: true})
	if _, l2 := st.registryFooter(svcRow{host: hostRegistry, idx: len(st.registryRows) - 2, name: "plain.io"}); l2 != "Enter connects" {
		t.Fatalf("plain footer %q", l2)
	}
	if _, l2 := st.registryFooter(svcRow{host: hostRegistry, idx: len(st.registryRows) - 1, name: "in.io"}); l2 != "signed in" {
		t.Fatalf("signed-in footer %q", l2)
	}
	for i := range st.registryRows {
		st.collapsed[registryFoldKey(st.registryRows[i].Host)] = true
	}
	if st.anyRegistryExpanded() {
		t.Fatal("still expanded")
	}
	// No engine at all.
	st.containerDetected, st.containerEngine = true, containers.Engine{}
	st.collapsed[registryFoldKey(r.hostID)] = false
	st.hostLoaded["images"] = false
	if rows := st.registryImageRows(r); len(rows) != 1 || !strings.Contains(rows[0].text, "no container engine") {
		t.Fatalf("hints: %+v", rows)
	}
}
