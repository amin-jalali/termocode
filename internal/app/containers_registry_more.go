package app

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Registries: catalog, private tags, delete tag, cloud hints (CTX-35) ──
//
// Private lists and deletes use the token the user saved in termocode's
// secret store (registry.<host>, from "Connect registry…" with
// Remember), only after a Cancel-first opt-in card for that host; the
// opt-in lasts for this session. The token goes to the registry's own
// token server over HTTPS; it is never shown, logged or given to the AI.
// Delete is by digest (every tag on that manifest goes) behind a
// Cancel-first card. Cloud registries get their CLI login command to copy;
// termocode never runs a cloud CLI.

const (
	confirmKindRegistryAuth      confirmKindEnum = 5343
	confirmKindRegistryDeleteTag confirmKindEnum = 5344
	confirmKindRegistryCloudHint confirmKindEnum = 5345

	promptKindRegistryCatalogHost promptKindEnum = 5343
	promptKindRegistryAuthUser    promptKindEnum = 5344

	pickerKindRegistryCatalog pickerKindEnum = 5343
)

// regAuthPending is the opt-in card's question.
type regAuthPending struct {
	host, user string
	retry      func(*Model) tea.Cmd
}

// regDeletePending is the delete card's target.
type regDeletePending struct {
	repo        containers.RemoteRepo
	tag, digest string
}

type registryCatalogMsg struct {
	host string
	cat  containers.Catalog
	err  error
}

type registryDigestMsg struct {
	repo        containers.RemoteRepo
	tag, digest string
	err         error
}

type registryDeleteDoneMsg struct {
	repo        containers.RemoteRepo
	tag, digest string
	err         error
}

// registryHasSavedToken: the secret store has registry.<host>.
func (m *Model) registryHasSavedToken(host string) bool {
	s, err := m.secretStore()
	if err != nil || s == nil {
		return false
	}
	name := registrySecretName(host)
	for _, n := range s.Names() {
		if n == name {
			return true
		}
	}
	return false
}

// registryAPIFor is the lister for host: with the saved login when the
// user opted in for host (and the store is open), else anonymous. The
// returned func runs in the command (a ${cmd:} reference may be slow).
func (m *Model) registryAPIFor(host string) func() (*containers.RegistryAPI, error) {
	st := m.services
	host = containers.RegistryDisplayHost(host)
	user := st.regAuthOK[host]
	saved := ""
	if user != "" {
		if s, err := m.secretStore(); err == nil && s != nil {
			if v, ok, err := s.Get(registrySecretName(host)); err == nil && ok {
				saved = v
			}
		}
	}
	root := st.root
	return func() (*containers.RegistryAPI, error) {
		api := newRegistryAPI()
		if saved == "" {
			return api, nil
		}
		v := saved
		if ref, err := secrets.ParseRef(saved); err == nil {
			if v, err = newRefResolver(root).Resolve(ref); err != nil {
				return nil, errors.New("the saved token reference did not resolve")
			}
		}
		ai.RegisterSecret(v)
		api.Login = &containers.RegistryLogin{User: user, Secret: v}
		return api, nil
	}
}

// askRegistryAuth is the opt-in card for host; retry runs after Use.
func (m *Model) askRegistryAuth(host, user string, retry func(*Model) tea.Cmd) tea.Cmd {
	host = containers.RegistryDisplayHost(host)
	m.services.regAuth = regAuthPending{host: host, user: user, retry: retry}
	h := sanitizeOutputLine(host)
	m.confirm = confirm.New("Use your saved token for "+h+"?",
		h+" wants a login for this. termocode can send the token it saved for "+h+" ("+registrySecretName(host)+
			") to the registry's own token server over HTTPS, for this session only. It is never shown or logged.",
		[]confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "use", Title: "Use saved token", Style: confirm.StylePrimary}})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindRegistryAuth
	return nil
}

func (m *Model) answerRegistryAuth(id string) tea.Cmd {
	st := m.services
	p := st.regAuth
	if id != "use" || p.host == "" {
		st.regAuth = regAuthPending{}
		return nil
	}
	if p.user == "" {
		m.prompt = prompt.New("Saved token · user", "User name for "+sanitizeOutputLine(p.host)+":", "")
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen, m.promptKind = true, promptKindRegistryAuthUser
		return nil
	}
	return m.withSecretsUnlocked(func(m *Model) tea.Cmd { return m.registryAuthOptIn() })
}

func (m *Model) onRegistryAuthUser(value string) tea.Cmd {
	user := strings.TrimSpace(value)
	if err := containers.ValidRegistryUser(user); err != nil {
		m.services.regAuth = regAuthPending{}
		return m.servicesToast(toast.Errr, "Not a user name", err.Error())
	}
	m.services.regAuth.user = user
	return m.withSecretsUnlocked(func(m *Model) tea.Cmd { return m.registryAuthOptIn() })
}

func (m *Model) registryAuthOptIn() tea.Cmd {
	st := m.services
	p := st.regAuth
	st.regAuth = regAuthPending{}
	if p.host == "" || p.user == "" {
		return nil
	}
	if st.regAuthOK == nil {
		st.regAuthOK = map[string]string{}
	}
	st.regAuthOK[p.host] = p.user
	if p.retry != nil {
		return p.retry(m)
	}
	return nil
}

// registryNeedsLogin answers ErrNeedsSignIn: the opt-in card when a token
// is saved for host, else a toast that says how to get one.
func (m *Model) registryNeedsLogin(host string, retry func(*Model) tea.Cmd) tea.Cmd {
	host = containers.RegistryDisplayHost(host)
	st := m.services
	if st.regAuthOK[host] == "" && m.registryHasSavedToken(host) {
		return m.askRegistryAuth(host, st.registryUsers[host], retry)
	}
	return m.servicesToast(toast.Info, sanitizeOutputLine(host)+" needs a login",
		"Connect registry… with Remember saves a token termocode can use")
}

// ── Catalog ──────────────────────────────────────────────────────────────

func (m *Model) askCatalog(prefill string) {
	m.prompt = prompt.New("Browse registry catalog", "Registry host (like localhost:5000 or ghcr.io):", prefill)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindRegistryCatalogHost
}

func (m *Model) catalogFromPalette() tea.Cmd {
	prefill := ""
	if r, _, ok := m.selectedRegistry(); ok {
		prefill = r.name
	}
	m.askCatalog(prefill)
	return nil
}

func (m *Model) loadCatalog(host string) tea.Cmd {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil
	}
	mk := m.registryAPIFor(host)
	return func() tea.Msg {
		api, err := mk()
		if err != nil {
			return registryCatalogMsg{host: host, err: err}
		}
		cat, err := api.Catalog(context.Background(), host)
		return registryCatalogMsg{host: host, cat: cat, err: err}
	}
}

func (m *Model) onRegistryCatalog(msg registryCatalogMsg) tea.Cmd {
	h := sanitizeOutputLine(msg.host)
	switch {
	case errors.Is(msg.err, containers.ErrNeedsSignIn):
		host := msg.host
		return m.registryNeedsLogin(host, func(m *Model) tea.Cmd { return m.loadCatalog(host) })
	case msg.err != nil:
		return m.servicesToast(toast.Info, "No catalog from "+h, sanitizeOutputLine(errLine(msg.err.Error())))
	case len(msg.cat.Repos) == 0:
		return m.servicesToast(toast.Info, h+" lists no repositories", "")
	}
	m.services.regCatalogHost = msg.cat.Host
	items := make([]picker.Item, 0, len(msg.cat.Repos))
	for _, r := range msg.cat.Repos {
		items = append(items, picker.Item{ID: r, Title: r, Hint: "browse tags"})
	}
	title := " " + h + " catalog "
	if msg.cat.Truncated {
		title = " " + h + " catalog (first " + itoa(len(items)) + ") "
	}
	m.picker = picker.NewItems(title, items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen, m.pickerKind = true, pickerKindRegistryCatalog
	return nil
}

func (m *Model) onCatalogPicked(repo string) tea.Cmd {
	host := m.services.regCatalogHost
	if host == "" || repo == "" {
		return nil
	}
	return m.browseRemote(host+"/"+repo, false)
}

// ── Delete tag ───────────────────────────────────────────────────────────

// registryDeleteStart reads the tag's digest (with the login) for the card.
func (m *Model) registryDeleteStart(r svcRow) tea.Cmd {
	ref := r.imageRef
	i := strings.LastIndexByte(ref, ':')
	if r.host != hostRemoteTag || i < 0 || i < strings.LastIndexByte(ref, '/') {
		return nil
	}
	repo, err := containers.ParseRemoteRepo(ref[:i])
	if err != nil {
		return m.servicesToast(toast.Info, "Not an image repository", sanitizeOutputLine(err.Error()))
	}
	tag := ref[i+1:]
	if m.services.regAuthOK[repo.Host] == "" {
		return m.registryNeedsLogin(repo.Host, func(m *Model) tea.Cmd { return m.registryDeleteStart(r) })
	}
	mk := m.registryAPIFor(repo.Host)
	return func() tea.Msg {
		api, err := mk()
		if err != nil {
			return registryDigestMsg{repo: repo, tag: tag, err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		d, err := api.ManifestDigest(ctx, repo, tag)
		return registryDigestMsg{repo: repo, tag: tag, digest: d, err: err}
	}
}

func (m *Model) onRegistryDigest(msg registryDigestMsg) tea.Cmd {
	name := sanitizeOutputLine(msg.repo.Ref() + ":" + msg.tag)
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot read "+name, sanitizeOutputLine(errLine(ai.Redact(msg.err.Error()))))
	}
	m.services.regDelete = regDeletePending{repo: msg.repo, tag: msg.tag, digest: msg.digest}
	m.confirm = confirm.New("Delete "+name+" from the registry?",
		"The registry deletes the manifest "+msg.digest+". Every tag that points to it goes too, for everyone who pulls from "+
			sanitizeOutputLine(msg.repo.Host)+". Local images stay. This cannot be undone.",
		[]confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "delete", Title: "Delete from registry", Style: confirm.StyleDestructive}})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindRegistryDeleteTag
	return nil
}

func (m *Model) answerRegistryDeleteTag(id string) tea.Cmd {
	st := m.services
	p := st.regDelete
	st.regDelete = regDeletePending{}
	if id != "delete" || p.digest == "" {
		return nil
	}
	mk := m.registryAPIFor(p.repo.Host)
	return func() tea.Msg {
		api, err := mk()
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			err = api.DeleteManifest(ctx, p.repo, p.digest)
		}
		return registryDeleteDoneMsg{repo: p.repo, tag: p.tag, digest: p.digest, err: err}
	}
}

func (m *Model) onRegistryDeleteDone(msg registryDeleteDoneMsg) tea.Cmd {
	name := sanitizeOutputLine(msg.repo.Ref() + ":" + msg.tag)
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Could not delete "+name, sanitizeOutputLine(errLine(ai.Redact(msg.err.Error()))))
	}
	key := msg.repo.Ref()
	var reload tea.Cmd
	if rt := m.services.remoteTags[key]; rt != nil {
		rt.open = true
		reload = m.loadRemoteTags(key)
	}
	return tea.Batch(m.servicesToast(toast.Info, "Deleted "+name, "manifest "+msg.digest), reload)
}

// ── Cloud registry login hints ───────────────────────────────────────────

// cloudHintItems are the cloud rows of the Connect registry picker.
func cloudHintItems() []picker.Item {
	return []picker.Item{
		{ID: "cloud:aws", Title: "AWS ECR…", Hint: "sign in with the aws CLI"},
		{ID: "cloud:azure", Title: "Azure Container Registry…", Hint: "sign in with the az CLI"},
		{ID: "cloud:gcp", Title: "Google Artifact Registry…", Hint: "sign in with gcloud"},
	}
}

var cloudHintTemplates = map[string]containers.LoginHint{
	"aws": {Provider: "AWS ECR", Command: "aws ecr get-login-password --region <region> | docker login --username AWS --password-stdin <account>.dkr.ecr.<region>.amazonaws.com",
		Note: "The token from the AWS CLI is good for 12 hours."},
	"azure": {Provider: "Azure Container Registry", Command: "az acr login --name <registry>",
		Note: "Signs the engine in with your Azure CLI login."},
	"gcp": {Provider: "Google Artifact Registry", Command: "gcloud auth configure-docker <region>-docker.pkg.dev",
		Note: "Adds the gcloud credential helper for this host to the engine's config."},
}

// showCloudHint is the card with the CLI login command to copy. host ""
// = a template from the picker (no token sign-in offered).
func (m *Model) showCloudHint(h containers.LoginHint, host string) tea.Cmd {
	cmd := h.Command
	if e := m.services.registryEngineName(); e != "docker" {
		cmd = strings.ReplaceAll(cmd, "docker login", e+" login")
	}
	m.services.regHint, m.services.regHintHost = cmd, host
	body := h.Provider + " signs in with its own CLI. Run this in a terminal (termocode does not run it):\n\n" + cmd + "\n\n" + h.Note
	buttons := []confirm.Button{{ID: "cancel", Title: "Cancel"}}
	if host != "" {
		buttons = append(buttons, confirm.Button{ID: "token", Title: "Use a token instead…"})
	}
	buttons = append(buttons, confirm.Button{ID: "copy", Title: "Copy command", Style: confirm.StylePrimary})
	m.confirm = confirm.New("Sign in to "+h.Provider, body, buttons)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindRegistryCloudHint
	return nil
}

func (m *Model) answerCloudHint(id string) tea.Cmd {
	st := m.services
	cmd, host := st.regHint, st.regHintHost
	st.regHint, st.regHintHost = "", ""
	switch id {
	case "copy":
		if cmd == "" {
			return nil
		}
		m.copyToClipboard(cmd)
		return m.servicesToast(toast.Info, "Copied the login command", "run it in a terminal, then R refreshes")
	case "token":
		if host != "" {
			st.regPending = registryPending{host: host}
			m.askRegistryUser()
		}
	}
	return nil
}

// registryHostHint runs after the host prompt: a cloud host with a CLI
// login gets the hint card; ok=false goes on to the user prompt.
func (m *Model) registryHostHint(host string) (tea.Cmd, bool) {
	h, ok := containers.CloudLoginHint(host)
	if !ok {
		return nil, false
	}
	if h.Command == "" {
		m.services.regHostNote = h.Note
		return nil, false
	}
	return m.showCloudHint(h, host), true
}
