package app

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/menu"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── REGISTRIES: list, connect, sign out, local images per host (CP-7) ────
//
// The list comes from the engine's config file (hosts and helper names
// only; stored auth values are never read). "Connect registry…" asks for
// a host, a user and a token, then runs `<engine> login <host> --username
// <user> --password-stdin`: the token goes to stdin only, never into
// argv, the environment, a file of ours, a log or a toast. The engine's
// own credential store keeps it.
//
// "Remember in termocode" (plan §3.6) is a card between the user prompt
// and the token prompt. It is offered only when m.encryptedSecretStore()
// (secrets_ui.go) is ok (an unlocked age file or the OS keyring, never
// the plain credentials file). After a good sign-in the token is saved
// in that store as registry.<host>; a later sign-in to the host offers
// the saved token (asking to unlock first when the store is locked) and
// still sends it with --password-stdin.
//
// None of this is offered to the AI (palette ids are aiNever).

const pickerKindRegistryProvider pickerKindEnum = 1700

const (
	promptKindRegistryHost promptKindEnum = 1700 + iota
	promptKindRegistryUser
	promptKindRegistryToken
)

// registryPending is the connect flow between its prompts: never the
// token.
type registryPending struct {
	host, user string
	remember   bool // save the token in the secret store after a good login
}

const confirmKindRegistryRemember confirmKindEnum = 1801

type registryLoginDoneMsg struct {
	host, user string
	err        error
	remember   string // "ok": saved in the store; else why it was not saved
}

// registryFoldKey is the fold key of a registry row (open = its images).
func registryFoldKey(configKey string) string { return "reg/" + configKey }

func (st *servicesState) registryExpanded(configKey string) bool {
	c, ok := st.collapsed[registryFoldKey(configKey)]
	return ok && !c
}

func (st *servicesState) anyRegistryExpanded() bool {
	for _, r := range st.registryRows {
		if st.registryExpanded(r.Host) {
			return true
		}
	}
	return false
}

// registrySectionBody: one row per host, the open ones with their local
// images, and "Connect registry…" last (also under a note).
func (st *servicesState) registrySectionBody(key string) []svcRow {
	hint := func(t string) svcRow { return svcRow{kind: svcRowHint, section: key, text: t} }
	connect := svcRow{kind: svcRowHostItem, section: key, host: hostRegistryConnect, hostID: "connect", name: "Connect registry…"}
	if err := st.containerErrors["registries"]; err != "" {
		head, detail, _ := strings.Cut(sanitizeOutputLine(errLine(err)), ": ")
		rows := []svcRow{{kind: svcRowNote, section: key, text: head}}
		if detail != "" {
			rows = append(rows, hint(detail))
		}
		return append(rows, hint("R to retry"), connect)
	}
	if !st.hostLoaded["registries"] {
		return []svcRow{hint("Loading…")}
	}
	var out []svcRow
	if len(st.registryRows) == 0 {
		out = append(out, hint("Not signed in yet"))
	}
	for i, reg := range st.registryRows {
		row := svcRow{kind: svcRowHostItem, section: key, host: hostRegistry, idx: i, hostID: reg.Host,
			name: sanitizeOutputLine(containers.RegistryDisplayHost(reg.Host)), current: reg.SignedIn}
		out = append(out, row)
		out = append(out, st.registryImageRows(row)...)
	}
	out = append(out, st.registryRemoteRows(key)...)
	return append(out, connect)
}

// registryImageName is a reference without its registry host (and
// without Docker Hub's "library/").
func registryImageName(ref string) string {
	first, rest, ok := strings.Cut(ref, "/")
	if ok && (strings.ContainsAny(first, ".:") || first == "localhost") {
		ref = rest
		if containers.RegistryDisplayHost(first) != "docker.io" {
			return ref
		}
	}
	return strings.TrimPrefix(ref, "library/")
}

// registryImageRows are the local images tagged for an open host.
func (st *servicesState) registryImageRows(r svcRow) []svcRow {
	if !st.registryExpanded(r.hostID) {
		return nil
	}
	hint := func(t string) []svcRow { return []svcRow{{kind: svcRowHint, section: r.section, text: "  " + t}} }
	if !st.hostLoaded["images"] {
		switch {
		case st.engineAbsent():
			return hint("no container engine")
		case st.containerDetected && !st.containerEngine.Reachable:
			return hint(st.engineTitle() + " is not running")
		}
		return hint("Loading images…")
	}
	host := containers.RegistryDisplayHost(r.hostID)
	var out []svcRow
	for i, im := range st.imageRows {
		for _, ref := range im.RepoTags {
			if strings.Contains(ref, "<none>") || containers.ImageRegistryHost(ref) != host {
				continue
			}
			out = append(out, svcRow{kind: svcRowHostItem, section: r.section, host: hostRegistryImage, idx: i,
				hostID: ref, imageRef: ref, name: sanitizeOutputLine(registryImageName(ref)), depth: 1})
		}
	}
	if len(out) == 0 {
		return hint("no local images")
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func (st *servicesState) registryRowLook(r svcRow) (string, theme.Color256, theme.Color256) {
	switch r.host {
	case hostRegistryConnect:
		return theme.GlyphNew.String(), theme.TextSecondary, theme.TextSecondary
	case hostRegistry:
		if r.current {
			return theme.GlyphSvcUp.String(), theme.GitAdded, theme.TextPrimary
		}
		return theme.GlyphSvcStopped.String(), theme.TextDim, theme.TextSecondary
	}
	return "", 0, theme.TextPrimary
}

func (st *servicesState) registryFooter(r svcRow) (string, string) {
	switch r.host {
	case hostRegistryConnect:
		return "sign in to a registry", "the token stays hidden"
	case hostRegistryImage:
		size := ""
		if r.idx >= 0 && r.idx < len(st.imageRows) {
			size = " · " + st.imageRows[r.idx].Size
		}
		return sanitizeOutputLine(r.imageRef), "u pull" + size
	case hostRegistry:
		if r.idx < 0 || r.idx >= len(st.registryRows) {
			return "", ""
		}
		reg := st.registryRows[r.idx]
		l1 := r.name
		if u := st.registryUsers[r.name]; u != "" {
			l1 += " · " + sanitizeOutputLine(u)
		}
		helper := reg.CredHelper
		if helper == "" {
			helper = reg.CredsStore
		}
		if !reg.SignedIn {
			if helper != "" {
				return l1, "helper " + sanitizeOutputLine(helper) + " · Enter connects"
			}
			return l1, "Enter connects"
		}
		if helper != "" {
			return l1, "signed in · " + sanitizeOutputLine(helper)
		}
		return l1, "signed in"
	}
	return "", ""
}

// registryEngineName is "docker" / "podman" for messages.
func (st *servicesState) registryEngineName() string {
	if st.containerEngine.Kind != "" {
		return st.containerEngine.Kind
	}
	return "docker"
}

// registryEngine is the engine for login and logout. REGISTRIES needs no
// daemon, so the engine may not be detected yet: then the CLI is looked
// up on PATH. The session's DOCKER_CONTEXT is kept.
func (st *servicesState) registryEngine() (containers.Engine, bool) {
	e := st.containerEngine
	if e.Binary == "" {
		names := []string{"docker", "podman"}
		if pref := preferredContainerEngine(); pref != "auto" {
			names = []string{pref}
		}
		for _, n := range names {
			if p, err := exec.LookPath(n); err == nil {
				e = containers.Engine{Binary: p, Kind: n}
				break
			}
		}
		if e.Binary == "" {
			return e, false
		}
	}
	e.X, e.Dir, e.Context = servicesExec, st.root, st.sessionContext
	return e, true
}

// selectedRegistry is the registry row under the cursor.
func (m *Model) selectedRegistry() (svcRow, containers.Registry, bool) {
	r, ok := m.servicesCurrentRow()
	st := m.services
	if !ok || r.kind != svcRowHostItem || r.host != hostRegistry || r.idx < 0 || r.idx >= len(st.registryRows) {
		return svcRow{}, containers.Registry{}, false
	}
	return r, st.registryRows[r.idx], true
}

// ── Connect ──────────────────────────────────────────────────────────────

// startRegistryConnect opens the host picker, or (host given) goes
// straight to the user prompt.
func (m *Model) startRegistryConnect(host string) {
	st := m.services
	st.regPending = registryPending{}
	if host != "" {
		st.regPending.host = host
		m.askRegistryUser()
		return
	}
	items := []picker.Item{
		{ID: "docker.io", Title: "docker.io", Hint: "the default public registry"},
		{ID: "ghcr.io", Title: "ghcr.io"},
		{ID: "registry.gitlab.com", Title: "registry.gitlab.com"},
		{ID: "other", Title: "Other host…", Hint: "host or host:port"},
	}
	m.picker = picker.NewItems(" Connect registry ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen, m.pickerKind = true, pickerKindRegistryProvider
}

// onRegistryPicked asks for the host, prefilled with the picked one.
func (m *Model) onRegistryPicked(id string) tea.Cmd {
	value := id
	if id == "other" {
		value = ""
	}
	m.services.regPending = registryPending{}
	m.prompt = prompt.New("Connect registry · host", "Registry host:", value)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindRegistryHost
	return nil
}

func (m *Model) askRegistryUser() {
	host := m.services.regPending.host
	m.prompt = prompt.New("Connect registry · user", "User name for "+sanitizeOutputLine(host)+":",
		m.services.registryUsers[containers.RegistryDisplayHost(host)])
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindRegistryUser
}

func (m *Model) askRegistryToken() {
	m.prompt = prompt.New("Connect registry · token", "Token or password:", "")
	m.prompt.SetMasked(true)
	m.prompt.SetNote("Paste a token, or a reference like ${env:NAME}. It stays hidden. termocode never saves it in plain text.")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen, m.promptKind = true, promptKindRegistryToken
}

// registrySecretName is the store name for a host's token:
// registry.<host>, lower case, [a-z0-9_.-] only.
func registrySecretName(host string) string {
	h := strings.ToLower(containers.RegistryDisplayHost(host))
	var b strings.Builder
	for _, r := range h {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		case r == '.':
			if s := b.String(); s != "" && !strings.HasSuffix(s, ".") {
				b.WriteByte('.')
			}
		default:
			b.WriteByte('-')
		}
	}
	return "registry." + strings.Trim(b.String(), ".-")
}

// registryAfterUser runs after the user name: a saved token is offered
// first, else "Remember in termocode" when an encrypted store is open,
// else the token prompt.
func (m *Model) registryAfterUser() tea.Cmd {
	st := m.services
	host := sanitizeOutputLine(st.regPending.host)
	name := registrySecretName(st.regPending.host)
	if s, err := m.secretStore(); err == nil && s != nil {
		for _, n := range s.Names() {
			if n == name {
				m.confirm = confirm.New("Use the saved token for "+host+"?",
					"termocode saved a token for this registry. It is sent to "+st.registryEngineName()+" login only.",
					[]confirm.Button{{ID: "use", Title: "Use saved token", Style: confirm.StylePrimary}, {ID: "new", Title: "Type a new token"}})
				m.confirm.SetSize(m.w, m.h)
				m.confirmOpen, m.confirmKind = true, confirmKindRegistryRemember
				return nil
			}
		}
	}
	if _, ok := m.encryptedSecretStore(); ok {
		m.confirm = confirm.New("Remember the token in termocode?",
			"After a good sign-in to "+host+" the token is saved in your "+m.registryStoreLabel()+" and offered next time. Not saved = you type it each time.",
			[]confirm.Button{{ID: "remember", Title: "Remember", Style: confirm.StylePrimary}, {ID: "once", Title: "Not now"}})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen, m.confirmKind = true, confirmKindRegistryRemember
		return nil
	}
	m.askRegistryToken()
	return nil
}

func (m *Model) registryStoreLabel() string {
	if s, err := m.secretStore(); err == nil && s != nil {
		return secretsBackendLabel(s)
	}
	return "secret store"
}

// answerRegistryRemember handles the remember / saved-token card.
func (m *Model) answerRegistryRemember(id string) tea.Cmd {
	st := m.services
	switch id {
	case "remember":
		st.regPending.remember = true
	case "use":
		return m.withSecretsUnlocked(func(m *Model) tea.Cmd {
			s, err := m.secretStore()
			var v string
			ok := false
			if err == nil {
				v, ok, err = s.Get(registrySecretName(m.services.regPending.host))
			}
			if err != nil || !ok || v == "" {
				m.askRegistryToken()
				return m.servicesToast(toast.Warn, "Saved token not available", "Type the token again.")
			}
			return m.registryLogin(v)
		})
	}
	m.askRegistryToken()
	return nil
}

// clearPrompt drops the last prompt's text (a typed token must not stay
// in the model).
func (m *Model) clearPrompt() { m.prompt = prompt.New("", "", "") }

// onRegistryPrompt is Enter on one of the connect prompts.
func (m *Model) onRegistryPrompt(kind promptKindEnum, value string) tea.Cmd {
	st := m.services
	switch kind {
	case promptKindRegistryHost:
		host := strings.TrimSpace(value)
		if err := containers.ValidRegistryHost(host); err != nil {
			return m.servicesToast(toast.Errr, "Not a registry host", err.Error())
		}
		st.regPending = registryPending{host: host}
		m.askRegistryUser()
		return nil
	case promptKindRegistryUser:
		user := strings.TrimSpace(value)
		if err := containers.ValidRegistryUser(user); err != nil {
			return m.servicesToast(toast.Errr, "Not a user name", err.Error())
		}
		if st.regPending.host == "" {
			return nil
		}
		st.regPending.user = user
		return m.registryAfterUser()
	case promptKindRegistryToken:
		return m.registryLogin(value)
	}
	return nil
}

// registryLogin runs the login with the typed token or the value its
// reference points at. The value is registered for redaction before the
// call; it lives only in this closure.
func (m *Model) registryLogin(secret string) tea.Cmd {
	st := m.services
	pending := st.regPending
	st.regPending = registryPending{}
	m.clearPrompt()
	if pending.host == "" || pending.user == "" || secret == "" {
		return nil
	}
	var remember secrets.Store
	if pending.remember {
		remember, _ = m.encryptedSecretStore()
	}
	host := sanitizeOutputLine(pending.host)
	ref, refErr := secrets.ParseRef(secret)
	isRef := refErr == nil
	if refErr != nil && !errors.Is(refErr, secrets.ErrNotRef) {
		// The parser's words may quote part of what was typed: say it
		// in fixed words instead.
		return m.servicesToast(toast.Errr, "Could not sign in to "+host, "Not a valid reference: use ${env:NAME}, ${dotenv:NAME} or ${cmd:…}")
	}
	if !isRef {
		ai.RegisterSecret(secret)
	}
	e, ok := st.registryEngine()
	if !ok {
		return m.servicesToast(toast.Errr, "Could not sign in to "+host, "No container engine (Docker or Podman) on PATH")
	}
	root := st.root
	return func() tea.Msg {
		password := secret
		if isRef {
			v, err := newRefResolver(root).Resolve(ref)
			if err != nil {
				return registryLoginDoneMsg{host: pending.host, user: pending.user, err: err}
			}
			ai.RegisterSecret(v)
			password = v
		}
		err := e.Login(context.Background(), pending.host, pending.user, password)
		done := registryLoginDoneMsg{host: pending.host, user: pending.user, err: err}
		if err == nil && pending.remember {
			// What was typed is saved (a reference stays a reference).
			done.remember = "ok"
			if remember == nil {
				done.remember = "the store was locked"
			} else if serr := remember.Set(registrySecretName(pending.host), secret); serr != nil {
				done.remember = "could not save it"
			}
		}
		return done
	}
}

func (m *Model) onRegistryLoginDone(msg registryLoginDoneMsg) tea.Cmd {
	st := m.services
	host := sanitizeOutputLine(msg.host)
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Could not sign in to "+host, sanitizeOutputLine(errLine(ai.Redact(msg.err.Error()))))
	}
	if st.registryUsers == nil {
		st.registryUsers = map[string]string{}
	}
	st.registryUsers[containers.RegistryDisplayHost(msg.host)] = msg.user
	st.touch()
	detail := ""
	switch msg.remember {
	case "":
	case "ok":
		detail = "Token remembered in termocode"
	default:
		detail = "Token not remembered: " + msg.remember
	}
	return tea.Batch(m.servicesToast(toast.Info, "Signed in to "+host+" as "+sanitizeOutputLine(msg.user), detail),
		m.containersRefresh("registries"))
}

// registryPromptClosed is Esc on a connect prompt: nothing runs and the
// typed text is dropped.
func (m *Model) registryPromptClosed() tea.Cmd {
	m.services.regPending = registryPending{}
	m.clearPrompt()
	return m.servicesToast(toast.Info, "Connect cancelled", "")
}

// ── Sign out ─────────────────────────────────────────────────────────────

// askRegistrySignOut is a card with the exact `<engine> logout <host>`
// (the host as the config file names it).
func (m *Model) askRegistrySignOut(r svcRow) tea.Cmd {
	st := m.services
	if r.idx < 0 || r.idx >= len(st.registryRows) {
		return nil
	}
	reg := st.registryRows[r.idx]
	if !reg.SignedIn {
		return m.servicesToast(toast.Info, "Not signed in to "+r.name, "Enter connects")
	}
	if err := containers.ValidRegistryHost(reg.Host); err != nil {
		return m.servicesToast(toast.Errr, "Cannot sign out", err.Error())
	}
	e, ok := st.registryEngine()
	if !ok {
		return m.servicesToast(toast.Errr, "Cannot sign out", "No container engine (Docker or Podman) on PATH")
	}
	args := []string{e.Binary, "logout", reg.Host}
	m.confirm = confirm.New("Sign out of "+r.name+"?",
		"The engine forgets the saved login for this host. Local images stay; private pulls and pushes need a new sign-in.\n\nCommand: "+svcShellLine(args),
		[]confirm.Button{{ID: "cancel", Title: "Cancel"}, {ID: "signout", Title: "Sign out", Style: confirm.StyleDestructive}})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen, m.confirmKind = true, confirmKindContainerHostAction
	m.pendingHostArgs, m.pendingHostOperation, m.pendingHostTarget = args, "logout", "registries"
	return nil
}

func (m *Model) onRegistrySignedOut(msg hostActionDoneMsg) tea.Cmd {
	key := strings.TrimPrefix(msg.target, "logout ")
	host := containers.RegistryDisplayHost(key)
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Could not sign out of "+sanitizeOutputLine(host), sanitizeOutputLine(errLine(msg.err.Error())))
	}
	delete(m.services.registryUsers, host)
	return tea.Batch(m.servicesToast(toast.Info, "Signed out of "+sanitizeOutputLine(host), ""), m.containersRefresh("registries"))
}

// registryForPalette: the selected registry, else REGISTRIES opens and a
// toast says what to pick.
func (m *Model) registrySignOutFromPalette() tea.Cmd {
	if r, _, ok := m.selectedRegistry(); ok {
		return m.askRegistrySignOut(r)
	}
	st := m.services
	if !st.sectionShown("registries") {
		return m.servicesToast(toast.Info, "REGISTRIES is hidden", "m shows it")
	}
	st.collapsed[hostSection("registries")] = false
	st.touch()
	for i, r := range st.rows() {
		if r.kind == svcRowHostItem && r.host == hostRegistry && r.current {
			st.cursor = i
			break
		}
	}
	return tea.Batch(m.containersRefresh("registries"), m.servicesToast(toast.Info, "Pick a registry", "D signs out"))
}

// ── Images per host ──────────────────────────────────────────────────────

// toggleRegistry opens or closes a host's images; the first open lists
// the images (the engine is found first when needed).
func (m *Model) toggleRegistry(r svcRow) tea.Cmd {
	st := m.services
	open := !st.registryExpanded(r.hostID)
	st.collapsed[registryFoldKey(r.hostID)] = !open
	st.touch()
	if open {
		return m.registryImagesLoad()
	}
	return nil
}

func (m *Model) registryImagesLoad() tea.Cmd {
	st := m.services
	if st.hostLoaded["images"] || st.engineAbsent() {
		return nil
	}
	if !st.containerDetected {
		st.registryWantImages = true
		return m.containersDetectWithWatch(true)
	}
	if !st.containerEngine.Reachable {
		return nil
	}
	return m.containersList("images")
}

// registryPullPrefix is what the pull prompt starts with on a host row.
func registryPullPrefix(host string) string {
	if host == "docker.io" {
		return ""
	}
	return host + "/"
}

// ── Keys, menu ───────────────────────────────────────────────────────────

// registryKey handles a key on a REGISTRIES row (ok=false: not ours).
func (m *Model) registryKey(key string, r svcRow) (tea.Cmd, bool) {
	if r.hostSectionID() != "registries" {
		return nil, false
	}
	switch key {
	case "enter", "l":
		switch r.host {
		case hostRegistryConnect:
			m.startRegistryConnect("")
			return nil, true
		case hostRegistry:
			if _, reg, ok := m.selectedRegistry(); ok && !reg.SignedIn {
				m.startRegistryConnect(containers.RegistryDisplayHost(reg.Host))
				return nil, true
			}
			return m.toggleRegistry(r), true
		}
	case "n":
		m.startRegistryConnect("")
		return nil, true
	case "D":
		if r.host == hostRegistry {
			return m.askRegistrySignOut(r), true
		}
	case "u":
		switch {
		case r.host == hostRegistryImage:
			m.askImagePull(r.imageRef)
			return nil, true
		case r.host == hostRegistry:
			m.askImagePull(registryPullPrefix(r.name))
			return nil, true
		case r.kind == svcRowSection:
			m.askImagePull("")
			return nil, true
		}
	}
	return nil, false
}

func (m *Model) openRegistryMenu(r svcRow, x, y int) {
	m.services.menuRow = r
	var items []menu.Item
	switch r.host {
	case hostRegistry:
		items = []menu.Item{
			{ID: "connect", Title: "Sign in again…", Hint: "n"},
			{ID: "pull", Title: "Pull image…", Hint: "u"},
			{ID: "browse", Title: "Browse tags…", Hint: "b"},
			{ID: "search", Title: "Search Docker Hub…", Hint: "s"},
		}
		if r.current {
			items = append(items, menu.Item{Sep: true}, menu.Item{ID: "signout", Title: "Sign out…", Hint: "D"})
		}
	case hostRegistryImage:
		items = []menu.Item{{ID: "pull", Title: "Pull again", Hint: "u"}}
	default:
		items = []menu.Item{{ID: "connect", Title: "Connect registry…", Hint: "n"},
			{ID: "browse", Title: "Browse tags…", Hint: "b"},
			{ID: "search", Title: "Search Docker Hub…", Hint: "s"}}
	}
	m.menu = menu.New(items, x, y)
	m.menu.SetScreenSize(m.w, m.h)
	m.menuOpen, m.menuKind = true, menuKindServices
}

func (m *Model) handleRegistryMenuSelect(id string, r svcRow) tea.Cmd {
	switch id {
	case "connect":
		host := ""
		if r.host == hostRegistry {
			host = r.name
		}
		m.startRegistryConnect(host)
	case "pull":
		if r.host == hostRegistryImage {
			m.askImagePull(r.imageRef)
		} else {
			m.askImagePull(registryPullPrefix(r.name))
		}
	case "signout":
		return m.askRegistrySignOut(r)
	case "browse":
		prefill := ""
		if r.host == hostRegistry {
			prefill = registryPullPrefix(r.name)
		}
		m.askBrowse(prefill)
	case "search":
		m.askSearch()
	}
	return nil
}

func isRegistryHostKind(k hostKind) bool {
	return k == hostRegistry || k == hostRegistryImage || k == hostRegistryConnect
}

// errRegistryAI is the refusal for the model.
func errRegistryAI(id string) error {
	return fmt.Errorf("%s is not available to the model: signing in to and out of registries is the user's", id)
}
