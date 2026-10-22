package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// A Docker config with stored auth blobs: the view may show the hosts,
// never these values.
const (
	regBlobHub  = "aHViLXVzZXI6aHViLXBhc3N3b3JkLTEyMw=="
	regBlobGHCR = "Z2hjci1ibG9iLW5ldmVyLXNob3du"
	regConfig   = `{
  "auths": {
    "https://index.docker.io/v1/": {"auth": "` + regBlobHub + `"},
    "ghcr.io": {"auth": "` + regBlobGHCR + `"}
  },
  "credsStore": "desktop",
  "credHelpers": {"ghcr.io": "secretservice", "quay.io": "pass"}
}`
)

// regSession is a Containers view with only REGISTRIES open, a private
// HOME and DOCKER_CONFIG, and the fake docker on PATH. The engine is not
// detected: REGISTRIES reads a file and needs no daemon.
func regSession(t *testing.T, config string) (*Model, *servicestest.Fake, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	t.Setenv("DOCKER_CONTEXT", "")
	dockerConfig := filepath.Join(home, "dockercfg")
	if err := os.MkdirAll(dockerConfig, 0o700); err != nil {
		t.Fatal(err)
	}
	if config != "" {
		if err := os.WriteFile(filepath.Join(dockerConfig, "config.json"), []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("DOCKER_CONFIG", dockerConfig)
	f := hostFake(t)
	st := svcTestState()
	st.root = t.TempDir()
	st.sections = map[string]bool{}
	st.visible = true
	st.collapsed[hostSection("registries")] = false
	mm := svcTestModel(st, 100, 60)
	m := &mm
	pumpHost(t, m, m.containersRefresh("registries"))
	return m, f, home
}

func selectRow(t *testing.T, st *servicesState, match func(svcRow) bool) svcRow {
	t.Helper()
	for i, r := range st.rows() {
		if match(r) {
			st.cursor = i
			return r
		}
	}
	t.Fatalf("no such row:\n%s", svcRowsText(st))
	return svcRow{}
}

func selectRegistry(t *testing.T, st *servicesState, host string) svcRow {
	return selectRow(t, st, func(r svcRow) bool {
		return r.kind == svcRowHostItem && r.host == hostRegistry && r.name == host
	})
}

func regToast(m *Model) string { return strings.Join(m.toast.View(160, 40), " ") }

// submitPrompt is Enter on the open prompt, like the update loop.
func submitPrompt(t *testing.T, m *Model, value string) tea.Cmd {
	t.Helper()
	if !m.promptOpen {
		t.Fatalf("no prompt open to submit %q", value)
	}
	m.promptOpen = false
	return m.handlePromptSubmit(value)
}

// noSecretOnDisk walks dirs and fails when a file holds secret.
func noSecretOnDisk(t *testing.T, secret string, dirs ...string) {
	t.Helper()
	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			b, _ := os.ReadFile(p)
			if strings.Contains(string(b), secret) {
				t.Errorf("secret written to %s", p)
			}
			return nil
		})
	}
}

func TestRegistriesListFromDockerConfigWithoutAuthBlobs(t *testing.T) {
	defer func(mode theme.IconMode) { theme.CurrentIconMode = mode }(theme.CurrentIconMode)
	theme.CurrentIconMode = theme.IconModeUnicode
	m, f, _ := regSession(t, regConfig)
	st := m.services
	text := strings.Join(plainLines(m, 40, 60), "\n")
	for _, want := range []string{"REGISTRIES", "◉ docker.io", "◉ ghcr.io", "◦ quay.io", "Connect registry…"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, text)
		}
	}
	r := selectRegistry(t, st, "ghcr.io")
	l1, l2 := st.hostFooter(r)
	if l1 != "ghcr.io" || !strings.Contains(l2, "signed in") || !strings.Contains(l2, "secretservice") {
		t.Errorf("footer = %q / %q", l1, l2)
	}
	r = selectRegistry(t, st, "quay.io")
	if _, l2 := st.hostFooter(r); strings.Contains(l2, "signed in") || !strings.Contains(l2, "pass") {
		t.Errorf("helper-only footer = %q", l2)
	}
	all := text + l1 + l2
	for _, r := range st.rows() {
		a, b := st.hostFooter(r)
		all += a + b + r.name + r.hostID + r.text
	}
	for _, blob := range []string{regBlobHub, regBlobGHCR, "hub-password", "aHViLXVzZXI6"} {
		if strings.Contains(all, blob) {
			t.Fatalf("an auth value reached the view: %q", blob)
		}
	}
	if len(f.Calls()) != 0 {
		t.Errorf("listing registries called the engine: %q", f.Calls())
	}
}

func TestRegistriesMalformedConfigIsANote(t *testing.T) {
	m, _, _ := regSession(t, `{"auths": "`+regBlobGHCR+`"`)
	text := strings.Join(plainLines(m, 40, 60), "\n")
	if !strings.Contains(text, "REGISTRIES") || !strings.Contains(text, "config.json") {
		t.Fatalf("no note for a broken config:\n%s", text)
	}
	if strings.Contains(text, regBlobGHCR) {
		t.Fatal("the note shows a stored value")
	}
	// Connecting still works from a broken file.
	if !strings.Contains(text, "Connect registry…") {
		t.Errorf("no Connect row under the note:\n%s", text)
	}
}

func TestRegistriesEmptyConfigOffersConnect(t *testing.T) {
	m, _, _ := regSession(t, "")
	text := strings.Join(plainLines(m, 40, 60), "\n")
	if !strings.Contains(text, "Not signed in yet") || !strings.Contains(text, "Connect registry…") {
		t.Fatalf("empty REGISTRIES:\n%s", text)
	}
}

// The whole connect flow: picker → host → user → masked token →
// `login <host> --username <u> --password-stdin`. The token is only in
// the fake's login.stdin: not in argv.log, toasts, the screen, the
// prompt, the session file or any file under HOME.
func TestRegistryConnectSecretOnlyOnStdin(t *testing.T) {
	m, f, home := regSession(t, regConfig)
	st := m.services
	const secret = "ghp_secretTokenValue_0123456789"
	selectRow(t, st, func(r svcRow) bool { return r.kind == svcRowSection && r.section == hostSection("registries") })
	pumpHost(t, m, press(m, "n"))
	if !m.pickerOpen || m.pickerKind != pickerKindRegistryProvider {
		t.Fatalf("n on REGISTRIES did not open the registry picker (open=%v kind=%v)", m.pickerOpen, m.pickerKind)
	}
	m.pickerOpen = false
	pumpHost(t, m, m.onRegistryPicked("ghcr.io"))
	if !m.promptOpen || m.promptKind != promptKindRegistryHost || !strings.Contains(ansi.Strip(m.prompt.View()), "ghcr.io") {
		t.Fatalf("host prompt not prefilled: %s", ansi.Strip(m.prompt.View()))
	}
	pumpHost(t, m, submitPrompt(t, m, "ghcr.io"))
	if !m.promptOpen || m.promptKind != promptKindRegistryUser {
		t.Fatal("no user prompt after the host")
	}
	pumpHost(t, m, submitPrompt(t, m, "amin-jalali"))
	if !m.promptOpen || m.promptKind != promptKindRegistryToken {
		t.Fatal("no token prompt after the user")
	}
	box := ansi.Strip(m.prompt.View())
	if !strings.Contains(box, "${env:NAME}") {
		t.Errorf("token prompt does not offer references:\n%s", box)
	}
	for _, r := range secret {
		m.prompt, _ = m.prompt.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if box := ansi.Strip(m.prompt.View()); strings.Contains(box, secret) || strings.Contains(box, "secretToken") || !strings.Contains(box, "•") {
		t.Fatalf("token prompt is not masked:\n%s", box)
	}
	if len(callsWithPrefix(f.Calls(), "login")) != 0 {
		t.Fatal("login ran before Enter")
	}
	pumpHost(t, m, submitPrompt(t, m, secret))
	if got := callsWithPrefix(f.Calls(), "login"); len(got) != 1 || got[0] != "login ghcr.io --username amin-jalali --password-stdin" {
		t.Fatalf("login calls = %q", got)
	}
	stdin, err := os.ReadFile(filepath.Join(f.Data, "login.stdin"))
	if err != nil || string(stdin) != secret+"\n" {
		t.Fatalf("login.stdin = %q (%v), want the token", stdin, err)
	}
	argv, _ := os.ReadFile(filepath.Join(f.Data, "argv.log"))
	ctxlog, _ := os.ReadFile(filepath.Join(f.Data, "ctx.log"))
	if strings.Contains(string(argv)+string(ctxlog), secret) {
		t.Fatal("the token is in argv.log / ctx.log")
	}
	toastText := regToast(m)
	if !strings.Contains(toastText, "Signed in to ghcr.io as amin-jalali") || strings.Contains(toastText, secret) {
		t.Fatalf("toast = %q", toastText)
	}
	if strings.Contains(ansi.Strip(m.prompt.View()), secret) {
		t.Fatal("the closed prompt still holds the token")
	}
	screenText := strings.Join(plainLines(m, 60, 60), "\n")
	if strings.Contains(screenText, secret) {
		t.Fatal("the token is on screen")
	}
	r := selectRegistry(t, st, "ghcr.io")
	if l1, _ := st.hostFooter(r); !strings.Contains(l1, "amin-jalali") {
		t.Errorf("footer does not name the user: %q", l1)
	}
	m.servicesSaveSession()
	noSecretOnDisk(t, secret, home, st.root)
}

func TestRegistryConnectResolvesEnvReference(t *testing.T) {
	m, f, home := regSession(t, regConfig)
	const secret = "env-token-value-987654321"
	t.Setenv("REG_TOKEN_TEST", secret)
	m.startRegistryConnect("")
	m.pickerOpen = false
	pumpHost(t, m, m.onRegistryPicked("other"))
	if !m.promptOpen || m.promptKind != promptKindRegistryHost {
		t.Fatal("Other host… did not ask for the host")
	}
	pumpHost(t, m, submitPrompt(t, m, "registry.example.com:5000"))
	pumpHost(t, m, submitPrompt(t, m, "ci-bot"))
	pumpHost(t, m, submitPrompt(t, m, "${env:REG_TOKEN_TEST}"))
	stdin, _ := os.ReadFile(filepath.Join(f.Data, "login.stdin"))
	if string(stdin) != secret+"\n" {
		t.Fatalf("login.stdin = %q, want the resolved value", stdin)
	}
	if got := callsWithPrefix(f.Calls(), "login"); len(got) != 1 || got[0] != "login registry.example.com:5000 --username ci-bot --password-stdin" {
		t.Fatalf("login calls = %q", got)
	}
	if strings.Contains(strings.Join(f.Calls(), "\n"), secret) || strings.Contains(regToast(m), secret) {
		t.Fatal("the resolved value leaked")
	}
	noSecretOnDisk(t, secret, home)
}

func TestRegistryConnectBadReferenceRunsNothing(t *testing.T) {
	m, f, _ := regSession(t, regConfig)
	t.Setenv("REG_EMPTY_TEST", "")
	for _, ref := range []string{"${env:REG_EMPTY_TEST}", "${nope:x}", "${p4ssw0rdPart:rest}"} {
		m.startRegistryConnect("")
		m.pickerOpen = false
		pumpHost(t, m, m.onRegistryPicked("ghcr.io"))
		pumpHost(t, m, submitPrompt(t, m, "ghcr.io"))
		pumpHost(t, m, submitPrompt(t, m, "amin"))
		m.toast = toast.Model{}
		pumpHost(t, m, submitPrompt(t, m, ref))
		if n := len(callsWithPrefix(f.Calls(), "login")); n != 0 {
			t.Fatalf("%s: login ran %d times", ref, n)
		}
		if strings.HasPrefix(ref, "${env:") {
			// A valid reference that points at nothing: the login fails.
			if !strings.Contains(regToast(m), "Could not sign in") {
				t.Errorf("%s: toast = %q", ref, regToast(m))
			}
			continue
		}
		// SS-5: a malformed reference is refused in the prompt itself; it
		// stays open with a fixed message that never quotes what was typed.
		if !m.promptOpen || m.promptKind != promptKindRegistryToken || m.prompt.Error() == "" {
			t.Errorf("%s: prompt open=%v kind=%v error=%q", ref, m.promptOpen, m.promptKind, m.prompt.Error())
		}
		if strings.Contains(m.prompt.Error(), "p4ssw0rd") || strings.Contains(regToast(m), "p4ssw0rd") {
			t.Errorf("%s: message quotes what was typed", ref)
		}
	}
}

// A refused login shows the engine's words with the token scrubbed.
func TestRegistryConnectErrorScrubsToken(t *testing.T) {
	m, f, _ := regSession(t, regConfig)
	const secret = "wrong-password-abcdef-42"
	f.Set(t, "login.err", "Error response from daemon: Get \"https://ghcr.io/v2/\": denied: "+secret+"\n")
	m.startRegistryConnect("")
	m.pickerOpen = false
	pumpHost(t, m, m.onRegistryPicked("ghcr.io"))
	pumpHost(t, m, submitPrompt(t, m, "ghcr.io"))
	pumpHost(t, m, submitPrompt(t, m, "amin"))
	pumpHost(t, m, submitPrompt(t, m, secret))
	toastText := regToast(m)
	if !strings.Contains(toastText, "Could not sign in to ghcr.io") || !strings.Contains(toastText, "denied") {
		t.Fatalf("toast = %q", toastText)
	}
	if strings.Contains(toastText, secret) {
		t.Fatalf("toast shows the token: %q", toastText)
	}
}

// Esc on any step runs nothing and forgets what was typed.
func TestRegistryConnectEscRunsNothing(t *testing.T) {
	m, f, _ := regSession(t, regConfig)
	m.startRegistryConnect("")
	m.pickerOpen = false
	pumpHost(t, m, m.onRegistryPicked("ghcr.io"))
	pumpHost(t, m, submitPrompt(t, m, "ghcr.io"))
	pumpHost(t, m, submitPrompt(t, m, "amin"))
	for _, r := range "half-typed-secret-1" {
		m.prompt, _ = m.prompt.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m.promptOpen = false
	pumpHost(t, m, m.handleClonePromptClose())
	if n := len(f.Calls()); n != 0 {
		t.Fatalf("Esc ran %q", f.Calls())
	}
	if m.services.regPending != (registryPending{}) {
		t.Errorf("pending connect kept: %+v", m.services.regPending)
	}
	if strings.Contains(ansi.Strip(m.prompt.View()), "half-typed") || m.prompt.View() != "" && strings.Contains(m.prompt.View(), "•") {
		t.Error("the closed prompt still holds typed text")
	}
}

// Enter on a ◦ host connects to that host (no picker).
func TestRegistryEnterOnHelperOnlyHostConnects(t *testing.T) {
	m, _, _ := regSession(t, regConfig)
	selectRegistry(t, m.services, "quay.io")
	pumpHost(t, m, press(m, "enter"))
	if !m.promptOpen || m.promptKind != promptKindRegistryUser || m.services.regPending.host != "quay.io" {
		t.Fatalf("Enter on quay.io: prompt=%v kind=%v pending=%+v", m.promptOpen, m.promptKind, m.services.regPending)
	}
	m.promptOpen = false
	selectRow(t, m.services, func(r svcRow) bool { return r.kind == svcRowHostItem && r.host == hostRegistryConnect })
	pumpHost(t, m, press(m, "enter"))
	if !m.pickerOpen || m.pickerKind != pickerKindRegistryProvider {
		t.Fatal("Enter on Connect registry… did not open the picker")
	}
}

// D on a host: a card with the exact `logout <host>`; Esc / Cancel runs
// nothing; Sign out runs exactly that.
func TestRegistrySignOutCard(t *testing.T) {
	m, f, _ := regSession(t, regConfig)
	selectRegistry(t, m.services, "ghcr.io")
	pumpHost(t, m, press(m, "D"))
	if !m.confirmOpen {
		t.Fatal("D did not open a card")
	}
	card := cardText(m)
	if !strings.Contains(card, "logout ghcr.io") || !strings.Contains(card, "Sign out") {
		t.Fatalf("card = %s", card)
	}
	m.confirmOpen = false
	pumpHost(t, m, func() tea.Cmd { c, _ := m.handleServicesConfirm("cancel"); return c }())
	if len(f.Calls()) != 0 {
		t.Fatalf("Cancel ran %q", f.Calls())
	}
	selectRegistry(t, m.services, "ghcr.io")
	pumpHost(t, m, press(m, "D"))
	m.confirmOpen = false
	pumpHost(t, m, func() tea.Cmd { c, _ := m.handleServicesConfirm("signout"); return c }())
	if got := f.Calls(); len(got) != 1 || got[0] != "logout ghcr.io" {
		t.Fatalf("sign out calls = %q", got)
	}
	if !strings.Contains(regToast(m), "Signed out of ghcr.io") {
		t.Errorf("toast = %q", regToast(m))
	}
	// Docker Hub signs out with its config key, as docker wrote it.
	selectRegistry(t, m.services, "docker.io")
	pumpHost(t, m, press(m, "D"))
	if !strings.Contains(cardText(m), "logout https://index.docker.io/v1/") {
		t.Errorf("hub card = %s", cardText(m))
	}
}

// Login and logout carry the session's DOCKER_CONTEXT.
func TestRegistryCallsHonourSessionContext(t *testing.T) {
	m, f, _ := regSession(t, regConfig)
	m.services.sessionContext = "prod"
	m.startRegistryConnect("")
	m.pickerOpen = false
	pumpHost(t, m, m.onRegistryPicked("ghcr.io"))
	pumpHost(t, m, submitPrompt(t, m, "ghcr.io"))
	pumpHost(t, m, submitPrompt(t, m, "amin"))
	pumpHost(t, m, submitPrompt(t, m, "context-token-123"))
	selectRegistry(t, m.services, "ghcr.io")
	pumpHost(t, m, press(m, "D"))
	m.confirmOpen = false
	pumpHost(t, m, func() tea.Cmd { c, _ := m.handleServicesConfirm("signout"); return c }())
	var got []string
	for _, c := range f.CtxCalls() {
		if strings.Contains(c, "login") || strings.Contains(c, "logout") {
			got = append(got, c)
		}
	}
	if len(got) != 2 || got[0] != "prod|login ghcr.io --username amin --password-stdin" || got[1] != "prod|logout ghcr.io" {
		t.Fatalf("ctx calls = %q", got)
	}
}

// Under a host: the local images tagged for it; u pulls by name.
func TestRegistryLocalImagesAndPullByName(t *testing.T) {
	st := svcHostState()
	st.collapsed[hostSection("registries")] = false
	st.hostLoaded["registries"], st.hostLoaded["images"] = true, true
	st.registryRows = []containers.Registry{{Host: "https://index.docker.io/v1/", SignedIn: true}, {Host: "ghcr.io", SignedIn: true}}
	st.imageRows = []containers.Image{
		{ID: "sha256:1", RepoTags: []string{"postgres:16"}},
		{ID: "sha256:2", RepoTags: []string{"ghcr.io/acme/shop:latest", "ghcr.io/acme/shop:1.2"}},
		{ID: "sha256:3", RepoTags: []string{"<none>:<none>"}},
	}
	mm := svcTestModel(st, 100, 80)
	m := &mm
	r := selectRegistry(t, st, "ghcr.io")
	pumpHost(t, m, press(m, "enter"))
	var under []string
	for _, row := range st.rows() {
		if row.kind == svcRowHostItem && row.host == hostRegistryImage {
			under = append(under, row.name)
		}
	}
	if strings.Join(under, ",") != "acme/shop:1.2,acme/shop:latest" {
		t.Fatalf("images under ghcr.io = %q\n%s", under, svcRowsText(st))
	}
	selectRegistry(t, st, "docker.io")
	pumpHost(t, m, press(m, "enter"))
	found := false
	for _, row := range st.rows() {
		if row.kind == svcRowHostItem && row.host == hostRegistryImage && row.name == "postgres:16" {
			found = true
		}
		if row.host == hostRegistryImage && strings.Contains(row.name, "<none>") {
			t.Error("a dangling image is listed under a registry")
		}
	}
	if !found {
		t.Fatalf("postgres:16 not under docker.io:\n%s", svcRowsText(st))
	}
	// u on an image row: the pull prompt with its full name.
	selectRow(t, st, func(row svcRow) bool { return row.host == hostRegistryImage && row.name == "acme/shop:latest" })
	pumpHost(t, m, press(m, "u"))
	if !m.promptOpen || m.promptKind != promptKindContainerPull || !strings.Contains(ansi.Strip(m.prompt.View()), "ghcr.io/acme/shop:latest") {
		t.Fatalf("u on an image: prompt=%v kind=%v\n%s", m.promptOpen, m.promptKind, ansi.Strip(m.prompt.View()))
	}
	// u on the host: the pull prompt starts with "<host>/".
	m.promptOpen = false
	selectRegistry(t, st, r.name)
	pumpHost(t, m, press(m, "u"))
	if !m.promptOpen || m.promptKind != promptKindContainerPull || !strings.Contains(ansi.Strip(m.prompt.View()), "ghcr.io/") {
		t.Fatalf("u on a host: %s", ansi.Strip(m.prompt.View()))
	}
}

// Expanding a host before IMAGES ever loaded lists the images once.
func TestRegistryExpandLoadsImagesOnDemand(t *testing.T) {
	m, f, _ := regSession(t, regConfig)
	selectRegistry(t, m.services, "docker.io")
	pumpHost(t, m, press(m, "enter"))
	if f.Count("images") != 1 {
		t.Fatalf("images listed %d times: %q", f.Count("images"), f.Calls())
	}
	if !m.services.hostLoaded["images"] {
		t.Fatal("images not loaded after expanding a registry")
	}
}

func TestRegistryPaletteAndAIPolicy(t *testing.T) {
	titles := map[string]string{}
	for _, e := range PaletteCatalog() {
		titles[e.ID] = e.Title
	}
	if titles["containers-registry-connect"] != "Containers: Connect Registry…" || titles["containers-registry-signout"] != "Containers: Sign Out of Registry…" {
		t.Errorf("palette titles = %q / %q", titles["containers-registry-connect"], titles["containers-registry-signout"])
	}
	for _, id := range []string{"containers-registry-connect", "containers-registry-signout"} {
		if p, ok := aiActionPolicies[id]; !ok || p.Gate != aiGateNever || p.Reason == "" {
			t.Errorf("%s: AI policy %+v ok=%v, want never with a reason", id, p, ok)
		}
	}
	st := svcHostState()
	st.registryRows = []containers.Registry{{Host: "ghcr.io", SignedIn: true}}
	mm := svcTestModel(st, 100, 40)
	for _, id := range []string{"containers-registry-connect", "containers-registry-signout"} {
		if _, err := mm.dispatchContainerAIAction(id, "ghcr.io"); err == nil {
			t.Errorf("the AI action dispatcher accepted %s", id)
		}
	}
	if mm.confirmOpen || mm.promptOpen || mm.pickerOpen {
		t.Error("an AI dispatch opened a card or prompt")
	}
}

func TestRegistryPaletteConnectOpensPicker(t *testing.T) {
	m, _, _ := regSession(t, regConfig)
	cmd, ok := m.dispatchServicesPalette("containers-registry-connect")
	if !ok {
		t.Fatal("palette id not handled")
	}
	pumpHost(t, m, cmd)
	if !m.pickerOpen || m.pickerKind != pickerKindRegistryProvider {
		t.Fatal("palette Connect did not open the picker")
	}
}
