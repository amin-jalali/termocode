package app

import (
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/containers"
)

const ctrRegToken = "reg-TOKEN-never-shown"

// ctrRegistry is a TLS registry on loopback: with auth on, every call
// wants HTTP Basic amin/ctrRegToken; the catalog lists three repos, a
// HEAD on a manifest gives its digest and DELETE accepts.
type ctrRegistry struct {
	host     string
	auth     atomic.Bool
	deletes  atomic.Int32
	noDelete atomic.Bool
}

func newCtrRegistry(t *testing.T, maxTags int) *ctrRegistry {
	t.Helper()
	reg := &ctrRegistry{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reg.auth.Load() {
			if u, p, ok := r.BasicAuth(); !ok || u != "amin" || p != ctrRegToken {
				w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}
		switch {
		case r.URL.Path == "/v2/_catalog":
			b, _ := json.Marshal(map[string]any{"repositories": []string{"acme/api", "acme/web", "acme/db"}})
			_, _ = w.Write(b)
		case strings.Contains(r.URL.Path, "/manifests/") && r.Method == http.MethodHead:
			w.Header().Set("Docker-Content-Digest", "sha256:"+strings.Repeat("a", 64))
		case strings.Contains(r.URL.Path, "/manifests/") && r.Method == http.MethodDelete:
			if reg.noDelete.Load() {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			reg.deletes.Add(1)
			w.WriteHeader(http.StatusAccepted)
		case strings.HasSuffix(r.URL.Path, "/tags/list"):
			_, _ = w.Write([]byte(`{"name":"acme/api","tags":["1.0"]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	old := newRegistryAPI
	newRegistryAPI = func() *containers.RegistryAPI {
		return &containers.RegistryAPI{RootCAs: pool, Timeout: 5 * time.Second, MaxTags: maxTags}
	}
	t.Cleanup(func() { newRegistryAPI = old })
	u, _ := url.Parse(srv.URL)
	reg.host = u.Host
	return reg
}

// ctrSaveToken puts value in a plain secret store as registry.<host>.
func ctrSaveToken(t *testing.T, m *Model, host, value string) {
	t.Helper()
	withSecretSetting(t, "plain", false)
	s, err := m.secretStore()
	if err != nil || s == nil {
		t.Fatalf("store: %v", err)
	}
	if err := s.Set(registrySecretName(host), value); err != nil {
		t.Fatal(err)
	}
}

func ctrRun[T any](t *testing.T, cmd tea.Cmd) T {
	t.Helper()
	if cmd == nil {
		t.Fatalf("no command for %T", *new(T))
	}
	return ctrFind[T](t, ctrMsgs(cmd))
}

// The catalog prompt prefills the selected registry, lists the repos in a
// picker (the first N when capped) and a pick browses that repo's tags.
func TestCovContainerRegistryCatalogAnonymous(t *testing.T) {
	m, _ := ctrModel(t)
	reg := newCtrRegistry(t, 2)
	m.services.registryRows = []containers.Registry{{Host: "ghcr.io"}}
	m.services.sections["registries"] = true
	m.services.collapsed[hostSection("registries")] = false
	m.services.touch()
	if !m.servicesCursorTo(func(r svcRow) bool { return r.host == hostRegistry && r.kind == svcRowHostItem }) {
		t.Fatalf("no registry row:\n%s", svcRowsText(m.services))
	}
	_ = m.catalogFromPalette()
	if !m.promptOpen || m.promptKind != promptKindRegistryCatalogHost {
		t.Fatal("no catalog prompt")
	}
	if m.loadCatalog("  ") != nil {
		t.Fatal("an empty host loaded")
	}
	msg := ctrRun[registryCatalogMsg](t, m.loadCatalog(reg.host))
	if msg.err != nil || len(msg.cat.Repos) != 2 || !msg.cat.Truncated {
		t.Fatalf("%+v", msg)
	}
	_ = m.onRegistryCatalog(msg)
	if !m.pickerOpen || m.pickerKind != pickerKindRegistryCatalog || !strings.Contains(m.picker.View(), "first 2") {
		t.Fatalf("picker: %v", m.pickerOpen)
	}
	if m.onCatalogPicked("") != nil {
		t.Fatal("an empty pick browsed")
	}
	if m.onCatalogPicked("acme/api") == nil || m.services.remoteTags[reg.host+"/acme/api"] == nil {
		t.Fatalf("no browse: %v", m.services.remoteTags)
	}
	// Errors and an empty catalog only toast.
	_ = m.onRegistryCatalog(registryCatalogMsg{host: "x.io", err: containers.ErrRepoNotFound})
	if !strings.Contains(ctrToast(m), "No catalog from x.io") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.onRegistryCatalog(registryCatalogMsg{host: "x.io", cat: containers.Catalog{Host: "x.io"}})
	if !strings.Contains(ctrToast(m), "x.io lists no repositories") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	m.pickerOpen = false
	_ = m.onRegistryCatalog(registryCatalogMsg{host: "x.io", cat: containers.Catalog{Host: "x.io", Repos: []string{"a"}}})
	if !m.pickerOpen || strings.Contains(m.picker.View(), "first") {
		t.Fatal("untruncated title")
	}
}

// A registry that wants a login: with no saved token a toast says how to
// get one; with one, the Cancel-first card asks, the user prompt checks
// the name, and after Use the catalog loads with the saved token.
func TestCovContainerRegistryCatalogNeedsLogin(t *testing.T) {
	m, _ := ctrModel(t)
	reg := newCtrRegistry(t, 0)
	reg.auth.Store(true)
	withSecretSetting(t, "plain", false)
	msg := ctrRun[registryCatalogMsg](t, m.loadCatalog(reg.host))
	_ = m.onRegistryCatalog(msg)
	if m.confirmOpen || !strings.Contains(ctrToast(m), "needs a login") {
		t.Fatalf("no-token toast: %s err=%v", ctrToast(m), msg.err)
	}
	ctrSaveToken(t, m, reg.host, ctrRegToken)
	if !m.registryHasSavedToken(reg.host) || m.registryHasSavedToken("other.io") {
		t.Fatal("saved token lookup")
	}
	_ = m.onRegistryCatalog(msg)
	if !m.confirmOpen || m.confirmKind != confirmKindRegistryAuth {
		t.Fatal("no opt-in card")
	}
	// No user known: Use asks for one; a bad name stops it.
	if m.answerRegistryAuth("use") != nil || !m.promptOpen || m.promptKind != promptKindRegistryAuthUser {
		t.Fatal("no user prompt")
	}
	_ = m.onRegistryAuthUser("bad user")
	if !strings.Contains(ctrToast(m), "Not a user name") || m.services.regAuth.host != "" {
		t.Fatalf("toast %s", ctrToast(m))
	}
	// Again with a good name: the retry loads the catalog with the login.
	_ = m.onRegistryCatalog(msg)
	_ = m.answerRegistryAuth("use")
	cmd := m.onRegistryAuthUser(" amin ")
	if m.services.regAuthOK[reg.host] != "amin" {
		t.Fatalf("opt-in %v", m.services.regAuthOK)
	}
	got := ctrRun[registryCatalogMsg](t, cmd)
	if got.err != nil || len(got.cat.Repos) != 3 {
		t.Fatalf("with login: %+v", got)
	}
	// A known user goes straight to the opt-in.
	m.services.regAuthOK = nil
	m.services.registryUsers = map[string]string{reg.host: "amin"}
	_ = m.onRegistryCatalog(msg)
	if cmd := m.answerRegistryAuth("use"); cmd == nil || m.services.regAuthOK[reg.host] != "amin" {
		t.Fatal("known user: no opt-in")
	}
	// An empty pending opt-in does nothing.
	if m.registryAuthOptIn() != nil {
		t.Fatal("empty opt-in ran")
	}
	m.services.regAuth = regAuthPending{host: "h", user: "u"}
	if m.registryAuthOptIn() != nil || m.services.regAuthOK["h"] != "u" {
		t.Fatal("opt-in without retry")
	}
}

// A saved ${env:} reference resolves to the login; one that fails to
// resolve sends no login (the registry still wants one).
func TestCovContainerRegistrySavedReference(t *testing.T) {
	m, _ := ctrModel(t)
	reg := newCtrRegistry(t, 0)
	reg.auth.Store(true)
	ctrSaveToken(t, m, reg.host, "${cmd:false}")
	m.services.regAuthOK = map[string]string{reg.host: "amin"}
	if got := ctrRun[registryCatalogMsg](t, m.loadCatalog(reg.host)); !errors.Is(got.err, containers.ErrNeedsSignIn) {
		t.Fatalf("failing ref: %+v", got)
	}
	ctrSaveToken(t, m, reg.host, "${env:CTR_REG_TOKEN}")
	t.Setenv("CTR_REG_TOKEN", ctrRegToken)
	if got := ctrRun[registryCatalogMsg](t, m.loadCatalog(reg.host)); got.err != nil || len(got.cat.Repos) != 3 {
		t.Fatalf("ref: %+v", got)
	}
}

// Delete tag: the row asks for the login first, then reads the digest,
// the Cancel-first card deletes by digest and the tags list reloads.
func TestCovContainerRegistryDeleteTag(t *testing.T) {
	m, _ := ctrModel(t)
	reg := newCtrRegistry(t, 0)
	reg.auth.Store(true)
	ctrSaveToken(t, m, reg.host, ctrRegToken)
	m.services.registryUsers = map[string]string{reg.host: "amin"}
	row := svcRow{host: hostRemoteTag, imageRef: reg.host + "/acme/api:1.0"}
	if m.registryDeleteStart(svcRow{host: hostRemoteTag, imageRef: "noTag"}) != nil {
		t.Fatal("a ref with no tag")
	}
	_ = m.registryDeleteStart(svcRow{host: hostRemoteTag, imageRef: "Bad Repo:1"})
	if !strings.Contains(ctrToast(m), "Not an image repository") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.registryDeleteStart(row)
	if !m.confirmOpen || m.confirmKind != confirmKindRegistryAuth {
		t.Fatal("no opt-in first")
	}
	d := ctrRun[registryDigestMsg](t, m.answerRegistryAuth("use"))
	if d.err != nil || !strings.HasPrefix(d.digest, "sha256:") {
		t.Fatalf("digest %+v", d)
	}
	_ = m.onRegistryDigest(d)
	if m.answerRegistryDeleteTag("delete") == nil {
		t.Fatal("no delete")
	}
	_ = m.onRegistryDigest(d)
	done := ctrRun[registryDeleteDoneMsg](t, m.answerRegistryDeleteTag("delete"))
	if done.err != nil || reg.deletes.Load() != 1 {
		t.Fatalf("delete %+v n=%d", done, reg.deletes.Load())
	}
	key := d.repo.Ref()
	m.services.remoteTags = map[string]*remoteTags{key: {repo: d.repo, ref: key}}
	if m.onRegistryDeleteDone(done) == nil || !m.services.remoteTags[key].open || !strings.Contains(ctrToast(m), "Deleted") {
		t.Fatalf("reload: toast %s", ctrToast(m))
	}
	// The registry refuses: the error toasts without the token.
	reg.noDelete.Store(true)
	_ = m.onRegistryDigest(d)
	done = ctrRun[registryDeleteDoneMsg](t, m.answerRegistryDeleteTag("delete"))
	_ = m.onRegistryDeleteDone(done)
	if done.err == nil || !strings.Contains(ctrToast(m), "Could not delete") || strings.Contains(ctrToast(m), ctrRegToken) {
		t.Fatalf("refused: %s", ctrToast(m))
	}
	_ = m.onRegistryDigest(registryDigestMsg{repo: d.repo, tag: "1.0", err: containers.ErrNeedsSignIn})
	if !strings.Contains(ctrToast(m), "Cannot read") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	// A reference that does not resolve sends no login: digest and delete fail.
	ctrSaveToken(t, m, reg.host, "${cmd:false}")
	if got := ctrRun[registryDigestMsg](t, m.registryDeleteStart(row)); !errors.Is(got.err, containers.ErrNeedsSignIn) {
		t.Fatalf("digest: %v", got.err)
	}
	m.services.regDelete = regDeletePending{repo: d.repo, tag: "1.0", digest: d.digest}
	if got := ctrRun[registryDeleteDoneMsg](t, m.answerRegistryDeleteTag("delete")); got.err == nil {
		t.Fatal("no error")
	}
}

// Cloud hints: the card shows the CLI line (podman when that is the
// engine), Copy copies it, the token button goes on to the user prompt;
// a plain host goes straight on and ghcr.io keeps only a note.
func TestCovContainerRegistryCloudHint(t *testing.T) {
	m, _ := ctrModel(t)
	var copied []string
	defer SetClipboardSink(func(s string) { copied = append(copied, s) })()
	m.services.containerEngine.Kind = "podman"
	cmd, ok := m.registryHostHint("123.dkr.ecr.eu-west-1.amazonaws.com")
	if !ok || cmd != nil || !m.confirmOpen || !strings.Contains(m.services.regHint, "podman login") {
		t.Fatalf("ecr hint: %q", m.services.regHint)
	}
	if len(m.confirm.Buttons()) != 3 {
		t.Fatalf("buttons %+v", m.confirm.Buttons())
	}
	_ = m.answerCloudHint("copy")
	if len(copied) != 1 || !strings.Contains(copied[0], "aws ecr get-login-password --region eu-west-1") || !strings.Contains(ctrToast(m), "Copied the login command") {
		t.Fatalf("copied %v", copied)
	}
	if m.answerCloudHint("copy") != nil || len(copied) != 1 {
		t.Fatal("copied an empty command")
	}
	_, _ = m.registryHostHint("acme.azurecr.io")
	m.promptOpen = false
	_ = m.answerCloudHint("token")
	if !m.promptOpen || m.services.regPending.host != "acme.azurecr.io" {
		t.Fatalf("token: prompt %v pending %+v", m.promptOpen, m.services.regPending)
	}
	if _, ok := m.registryHostHint("ghcr.io"); ok || m.services.regHostNote == "" {
		t.Fatal("ghcr.io: no note")
	}
	if _, ok := m.registryHostHint("reg.example.com"); ok {
		t.Fatal("a plain host got a hint")
	}
	if items := cloudHintItems(); len(items) != 3 {
		t.Fatalf("items %v", items)
	}
}
