package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/addai"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/profileform"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Itermoc: AI PROVIDERS (AIP-4) ────────────────────────────────────────
//
// The models section lists the connected AIs, one row each:
//
//	✓ Anthropic                 4 models
//	    · claude-opus-5-5             (Enter opens the row)
//	    Updated 2 min ago
//	    Refresh (r)
//	    Disconnect… (D)
//	✓ OpenRouter         via environment
//	  from config.json
//	! gpt                         no key
//	  + Add AI…
//
// A row comes from ai/providers.json (or the list derived from the setup)
// plus a key that only an environment variable gives ("via environment":
// no Disconnect, the environment owns it). The model ids come from the
// model cache, read once into memory (never per frame). The user's own
// config.json slots stay as rows after the AIs (the expert path).
//
// Refresh fetches the list again (aiListModels). Disconnect asks first
// (Keep first), deletes the key of an API-key AI through the secret
// store, the providers.json entry and the cache entry. It never runs a
// CLI: a CLI's own sign-in stays (Q2). "+ Add AI…" (a) is the Add AI card.

// The rows of a provider.
const (
	// imRowProvider is a connected AI; name is itermocProvName(kind).
	imRowProvider imRowKind = 100 + iota
	// imRowProvAct is Refresh or Disconnect… under an open provider;
	// name is itermocProvName(kind) + ":refresh" / ":disconnect".
	imRowProvAct
)

// confirmKindAIProviderDisconnect is the "Disconnect Anthropic?" card.
const confirmKindAIProviderDisconnect confirmKindEnum = 2311

// itermocLoadModelsCache reads the model cache (a var: tests count the
// reads).
var itermocLoadModelsCache = ai.LoadModelsCache

// itermocProvModelRows is how many model ids an open row lists; the page
// (o) shows all of them.
const itermocProvModelRows = 8

// imProvider is one connected AI as the view shows it. Nothing secret:
// no key, and a server only as scheme://host.
type imProvider struct {
	kind, name  string // name: "Anthropic", or the title the user gave a server
	method, cli string
	label       string // how it is connected: "API key", "via environment" …
	cost        string // "your plan", "API · pay per use", "local · free"
	host        string // a local or URL server: scheme://host[:port]
	models      []string
	at          int64 // when the models were fetched (unix seconds; 0 never)
	envOnly     bool  // the key comes only from an environment variable
	busy        bool  // a refresh runs
	err         string
	signedOut   bool // a sign-in AI whose CLI is signed out
}

// itermocProvName is the row name of kind's provider. A slot name cannot
// hold ":", so it never meets a slot row.
func itermocProvName(kind string) string { return "ai:" + kind }

// itermocProvKind is the kind of a provider row or of one of its action
// rows ("" = not one).
func itermocProvKind(name string) string {
	rest, ok := strings.CutPrefix(name, "ai:")
	if !ok {
		return ""
	}
	kind, _, _ := strings.Cut(rest, ":")
	return kind
}

// itermocProviderTitle is how the view names kind.
func itermocProviderTitle(kind string) string {
	if e, ok := addai.EntryOf(kind); ok {
		return e.Name
	}
	return ai.KindLabel(kind)
}

// modelsCacheNow is the model cache, read from disk once (and again after
// a reload).
func (s *aiState) modelsCacheNow() ai.ModelsCache {
	if s.modelsCache == nil {
		c, _ := itermocLoadModelsCache()
		if c == nil {
			c = ai.ModelsCache{}
		}
		s.modelsCache = c
	}
	return s.modelsCache
}

// itermocProviders lists the connected AIs in the Add AI card's order,
// then the keys from the environment.
func itermocProviders(s *aiState) []imProvider {
	cache := s.modelsCacheNow()
	var out []imProvider
	add := func(p ai.ProviderEntry, envOnly bool) {
		ip := imProvider{kind: p.Kind, name: itermocProviderTitle(p.Kind), method: p.Method, cli: p.CLI,
			label: ai.MethodLabel(p), cost: ai.CostTag(p), host: aiSafeHost(p.BaseURL), envOnly: envOnly,
			busy: s.provBusy[p.Kind], err: s.provErr[p.Kind]}
		if p.Title != "" {
			ip.name = p.Title
		}
		if envOnly {
			ip.label = "via environment"
		}
		if p.Method == ai.MethodCLI && s.cliStates[p.CLI] == profileform.CLILoggedOut {
			ip.signedOut = true
		}
		if e, ok := cache[p.Kind]; ok {
			ip.at = e.At
			for _, mi := range e.Models {
				ip.models = append(ip.models, mi.ID)
			}
		}
		out = append(out, ip)
	}
	for _, kind := range itermocKindOrder(s.providers) {
		p := s.providers[kind]
		p.Kind = kind
		add(p, false)
	}
	for _, kind := range ai.EnvOnlyKinds(s.providers, os.Getenv) {
		add(ai.ProviderEntry{Kind: kind, Method: ai.MethodKey}, true)
	}
	return out
}

// itermocKindOrder is the kinds of ps in the Add AI card's order.
func itermocKindOrder(ps map[string]ai.ProviderEntry) []string {
	rank := map[string]int{}
	for i, e := range addai.Catalog() {
		rank[e.Kind] = i
	}
	kinds := make([]string, 0, len(ps))
	for k := range ps {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool {
		ri, iok := rank[kinds[i]]
		rj, jok := rank[kinds[j]]
		switch {
		case iok && jok:
			return ri < rj
		case iok != jok:
			return iok
		}
		return kinds[i] < kinds[j]
	})
	return kinds
}

func (sn itermocSnap) provider(kind string) (imProvider, bool) {
	for _, p := range sn.providers {
		if p.kind == kind {
			return p, true
		}
	}
	return imProvider{}, false
}

// userSlots are the slots of config.json ai_models: the rows after the AIs.
func (sn itermocSnap) userSlots() []imSlot {
	var out []imSlot
	for _, s := range sn.slots {
		if s.user {
			out = append(out, s)
		}
	}
	return out
}

// provUsers are the profiles with a step on kind (a provider step, or a
// CLI step of that kind's sign-in when the AI is connected that way).
func (sn itermocSnap) provUsers(kind string) []string {
	p, _ := sn.provider(kind)
	var out []string
	for _, pr := range sn.profiles {
		for _, st := range pr.p.Steps {
			if st.Provider == kind || p.method == ai.MethodCLI && st.CLI != "" && ai.CLIKind(st.CLI) == kind {
				out = append(out, pr.p.Name)
				break
			}
		}
	}
	return out
}

// missingProviders are the kinds profile steps name that are not
// connected, with the profiles that use each, in kind order.
func (sn itermocSnap) missingProviders() (kinds []string, users map[string][]string) {
	users = map[string][]string{}
	for _, pr := range sn.profiles {
		for _, st := range pr.p.Steps {
			if st.Provider == "" {
				continue
			}
			if _, ok := sn.provider(st.Provider); ok {
				continue
			}
			if us := users[st.Provider]; len(us) == 0 || us[len(us)-1] != pr.p.Name {
				users[st.Provider] = append(users[st.Provider], pr.p.Name)
			}
		}
	}
	for k := range users {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	return kinds, users
}

// providerNeeds are the AIs that wait for the user: a sign-in AI whose
// CLI is signed out, and an AI a profile step names that is not connected.
func (sn itermocSnap) providerNeeds() []imNeed {
	var out []imNeed
	g, tok := imGlyph("needs")
	for _, p := range sn.providers {
		if !p.signedOut {
			continue
		}
		out = append(out, imNeed{row: imRow{kind: imRowProvider, sec: imSecModels, name: itermocProvName(p.kind)}, glyph: g, tok: tok,
			name: p.name, what: ai.CLITitle(p.cli) + " is signed out · your plan cannot answer",
			fix: imPill{verb: "addai:" + p.kind + ":" + ai.MethodCLI, label: "Sign in", key: "i", warn: true}})
	}
	kinds, users := sn.missingProviders()
	for _, kind := range kinds {
		out = append(out, imNeed{row: imRow{kind: imRowProfile, sec: imSecProfiles, name: users[kind][0]}, glyph: g, tok: tok,
			name: itermocProviderTitle(kind), what: "not connected · used by " + strings.Join(users[kind], ", "),
			fix: imPill{verb: "addai:" + kind, label: "Add", key: "a", warn: true}})
	}
	return out
}

// ── Rows ─────────────────────────────────────────────────────────────────

// providerItems are the rows of the section: the AIs (an open one with
// its models and verbs), then the config.json slots.
func (st *itermocState) providerItems() []imRow {
	sn := st.snap
	sec := imSecModels
	var out []imRow
	for _, p := range sn.providers {
		if !st.matches(append([]string{p.name, p.kind, p.label}, p.models...)...) {
			continue
		}
		out = append(out, imRow{kind: imRowProvider, sec: sec, name: itermocProvName(p.kind)})
		if st.open[p.kind] {
			out = append(out, providerOpenRows(p)...)
		}
	}
	var slots []imRow
	for _, s := range sn.userSlots() {
		if st.matches(s.name, s.provider, s.model, "config.json") {
			slots = append(slots, imRow{kind: imRowSlot, sec: sec, name: s.name})
		}
	}
	if len(slots) > 0 {
		out = append(out, imRow{kind: imRowNote, sec: sec, text: "from config.json"})
		out = append(out, slots...)
	}
	if len(sn.providers) == 0 && len(sn.userSlots()) == 0 && st.filter == "" {
		out = append(out, imRow{kind: imRowNote, sec: sec, text: "No AI connected yet"})
	}
	return out
}

// providerOpenRows are the lines under an open provider row.
func providerOpenRows(p imProvider) []imRow {
	sec, name := imSecModels, itermocProvName(p.kind)
	var out []imRow
	for i, id := range p.models {
		if i == itermocProvModelRows && len(p.models) > i+1 {
			out = append(out, imRow{kind: imRowNote, sec: sec, text: "+" + strconv.Itoa(len(p.models)-i) + " more · o shows all"})
			break
		}
		out = append(out, imRow{kind: imRowNote, sec: sec, text: imText("· ") + id})
	}
	if len(p.models) == 0 {
		out = append(out, imRow{kind: imRowNote, sec: sec, text: "No models yet · r loads them"})
	}
	out = append(out, imRow{kind: imRowNote, sec: sec, text: providerUpdated(p)})
	out = append(out, imRow{kind: imRowProvAct, sec: sec, name: name + ":refresh"})
	if !p.envOnly {
		out = append(out, imRow{kind: imRowProvAct, sec: sec, name: name + ":disconnect"})
	}
	return out
}

// providerUpdated says when the models were fetched, or that it runs or
// failed.
func providerUpdated(p imProvider) string {
	switch {
	case p.busy:
		return imText("◐ Loading the models…")
	case p.err != "":
		return "! Models not refreshed · " + p.err
	case p.at == 0:
		return "Models not loaded yet"
	}
	return "Updated " + imAgo(clock.Now().Sub(time.Unix(p.at, 0)))
}

// providerCount is "4 models" / "1 model".
func providerCount(n int) string {
	if n == 1 {
		return "1 model"
	}
	return strconv.Itoa(n) + " models"
}

// providerWord is the one word of a provider row.
func providerWord(p imProvider) (string, theme.Color256) {
	switch {
	case p.signedOut:
		return "signed out", theme.DiagWarning
	case p.err != "":
		return "not refreshed", theme.DiagWarning
	case p.envOnly:
		return "via environment", theme.TextMuted
	}
	return providerCount(len(p.models)), theme.TextMuted
}

// providerGlyph is the mark of a provider row: ✓, or ! when it waits.
func providerGlyph(p imProvider) (string, theme.Color256) {
	if p.signedOut || p.err != "" {
		return imGlyph("needs")
	}
	return imGlyph("ok")
}

// providerActText is the label of a Refresh / Disconnect row.
func providerActText(name string) string {
	if strings.HasSuffix(name, ":disconnect") {
		return imText("Disconnect… (D)")
	}
	return "Refresh (r)"
}

func itermocProviderLine(b *imBuilder, p imProvider, open bool) {
	g, tok := providerGlyph(p)
	word, wtok := providerWord(p)
	b.add("  ", theme.TextMuted, false)
	b.add(g+" ", tok, false)
	b.addNameWord(p.name, theme.TextPrimary, open, word, wtok)
}

// ── Verbs ────────────────────────────────────────────────────────────────

// toggleProvider opens or closes a provider row.
func (st *itermocState) toggleProvider(kind string) {
	if st.open == nil {
		st.open = map[string]bool{}
	}
	st.open[kind] = !st.open[kind]
	st.touch()
}

// itermocProvVerb runs a verb of a provider row or of one of its rows.
// ok=false: not a provider verb.
func (m *Model) itermocProvVerb(verb string, row imRow) (tea.Cmd, bool) {
	if rest, ok := strings.CutPrefix(verb, "addai:"); ok {
		kind, method, _ := strings.Cut(rest, ":")
		return m.openAddAIOn(kind, method), true
	}
	if verb == "addai" {
		return m.openAddAI(), true
	}
	if row.kind != imRowProvider && row.kind != imRowProvAct {
		return nil, false
	}
	kind := itermocProvKind(row.name)
	switch verb {
	case "refresh":
		return m.itermocProvRefresh(kind, true), true
	case "disconnect":
		return m.openItermocProvDisconnect(kind), true
	case "toggle":
		m.itermocS().toggleProvider(kind)
		return nil, true
	}
	return nil, false
}

// itermocProvEnter is Enter (or a click) on a provider row or one of its
// verb rows.
func (m *Model) itermocProvEnter(row imRow) tea.Cmd {
	if row.kind == imRowProvAct {
		verb := "refresh"
		if strings.HasSuffix(row.name, ":disconnect") {
			verb = "disconnect"
		}
		cmd, _ := m.itermocProvVerb(verb, row)
		return cmd
	}
	m.itermocS().toggleProvider(itermocProvKind(row.name))
	return nil
}

// openAddAIOn opens the Add AI card on kind: at method when given (a
// signed-out AI's Sign in lands on its sign-in step), else at the AI's
// way choice (or its only way). An unknown kind shows the list.
func (m *Model) openAddAIOn(kind, method string) tea.Cmd {
	cmd := m.openAddAI()
	if !m.addAIOpen {
		return cmd // the secret store asks for its passphrase first
	}
	var c tea.Cmd
	m.addAI, c = m.addAI.StartAt(kind, method)
	return tea.Batch(cmd, c)
}

// itermocProvEntry is kind's entry (an environment key is an API key
// entry).
func (m *Model) itermocProvEntry(kind string) (ai.ProviderEntry, bool) {
	s := m.aiS()
	if p, ok := s.providers[kind]; ok {
		p.Kind = kind
		return p, true
	}
	if _, ok := m.itermocS().snap.provider(kind); ok {
		return ai.ProviderEntry{Kind: kind, Method: ai.MethodKey}, true
	}
	return ai.ProviderEntry{}, false
}

// itermocModelsMsg is a refreshed model list (or why not).
type itermocModelsMsg struct {
	kind   string
	models []ai.ModelInfo
	fail   string
}

// itermocProvRefresh fetches kind's models again off the Update goroutine.
// A sign-in AI has no list command: its list is the CLI's built-in one.
// show marks the row busy ("Loading the models…") until the answer; the
// quiet background refresh does not (a caller may drop its command).
func (m *Model) itermocProvRefresh(kind string, show bool) tea.Cmd {
	s := m.aiS()
	p, ok := m.itermocProvEntry(kind)
	if !ok || s.provBusy[kind] {
		return nil
	}
	if show {
		if s.provBusy == nil {
			s.provBusy = map[string]bool{}
		}
		s.provBusy[kind] = true
		delete(s.provErr, kind)
		m.itermocRefreshNow()
	}
	if p.Method == ai.MethodCLI {
		models := ai.CLIModels(p.CLI)
		return func() tea.Msg { return itermocModelsMsg{kind: kind, models: models} }
	}
	cfg := ai.Config{Kind: kind, BaseURL: p.BaseURL}
	if s.slots != nil {
		model := ai.DefaultModel(kind)
		if model == "" {
			model = "models"
		}
		if c, ok := s.slots.Config(ai.SyntheticSlot(kind, model)); ok {
			cfg = c
		}
	}
	name := itermocProviderTitle(kind)
	return func() tea.Msg {
		ai.RegisterSecret(cfg.APIKey) // before the request: any error text is redacted
		ctx, cancel := context.WithTimeout(context.Background(), addAICheckTimeout)
		defer cancel()
		ms, err := aiListModels(ctx, cfg)
		if err != nil {
			recordError("Refresh models " + kind + ": " + err.Error()) // the log gets the detail (redacted)
			fail := "could not reach " + name
			if ai.KeyRejected(err) {
				fail = "the key was not accepted"
			}
			return itermocModelsMsg{kind: kind, fail: fail}
		}
		return itermocModelsMsg{kind: kind, models: ms}
	}
}

// itermocAutoRefresh fetches, in the background, the lists older than
// ai.ModelsRefreshAfter (Itermoc opens).
func (m *Model) itermocAutoRefresh() tea.Cmd {
	s := m.aiS()
	cache := s.modelsCacheNow()
	now := clock.Now()
	var cmds []tea.Cmd
	for _, p := range itermocProviders(s) {
		if cache.DueRefresh(p.kind, now) {
			cmds = append(cmds, m.itermocProvRefresh(p.kind, false))
		}
	}
	return tea.Batch(cmds...)
}

// handleItermocProvMsg takes a refreshed list. ok=false: not ours.
func (m *Model) handleItermocProvMsg(msg tea.Msg) (tea.Cmd, bool) {
	r, ok := msg.(itermocModelsMsg)
	if !ok {
		return nil, false
	}
	s := m.aiS()
	delete(s.provBusy, r.kind)
	if r.fail != "" {
		if s.provErr == nil {
			s.provErr = map[string]string{}
		}
		s.provErr[r.kind] = r.fail
	} else {
		delete(s.provErr, r.kind)
		cache := s.modelsCacheNow()
		cache.Put(r.kind, r.models, clock.Now())
		_ = ai.SaveModelsCache(cache) // only a cache: the memory copy is what the view shows
	}
	m.itermocRefreshNow()
	return nil, true
}

// ── Disconnect… ──────────────────────────────────────────────────────────

// openItermocProvDisconnect asks before an AI goes: Keep is first and
// focused, and the card names the profiles that use it. An environment
// key has no Disconnect (the environment owns it).
func (m *Model) openItermocProvDisconnect(kind string) tea.Cmd {
	st := m.itermocS()
	p, ok := st.snap.provider(kind)
	if !ok || p.envOnly {
		return nil
	}
	var msg string
	switch p.method {
	case ai.MethodKey:
		msg = "The " + p.name + " key leaves the secret store. Its models leave the model lists."
		if v := ai.EnvKeyVar(kind); v != "" && os.Getenv(v) != "" {
			msg += "\n" + v + " is still set: the key still works from the environment."
		}
	case ai.MethodCLI:
		msg = "termocode forgets it. " + ai.CLITitle(p.cli) + " stays signed in on this machine; sign out there if you want to."
	case ai.MethodLocal:
		msg = p.name + " keeps running on this machine; termocode only forgets it."
	default:
		msg = "termocode forgets the server URL. The server itself is not changed."
	}
	if users := st.snap.provUsers(kind); len(users) > 0 {
		msg += "\nProfiles that use it: " + strings.Join(users, ", ") + ". Their steps on it stop until you connect it again."
	}
	st.provDisconnect = kind
	m.confirm = confirm.Risky("Disconnect "+p.name+"?", msg,
		confirm.Button{ID: "keep", Title: "Keep"},
		confirm.Button{ID: "disconnect", Title: "Disconnect", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindAIProviderDisconnect
	return nil
}

// answerItermocProvDisconnect runs the card's button (Esc is Keep).
func (m *Model) answerItermocProvDisconnect(id string) tea.Cmd {
	st := m.itermocS()
	kind := st.provDisconnect
	st.provDisconnect = ""
	if id != "disconnect" || kind == "" {
		return nil
	}
	name := itermocProviderTitle(kind)
	if p, ok := st.snap.provider(kind); ok {
		name = p.name
	}
	if err := m.disconnectProvider(kind); err != nil {
		return m.aiToastErr(name+" not disconnected", err)
	}
	delete(st.open, kind)
	m.aiS().reload()
	m.syncAIInline()
	m.itermocRefreshNow()
	return m.aiInfo(name + " disconnected")
}

// disconnectProvider removes kind: its API key (secret store), its
// providers.json entry and its cache entry. A CLI is never run.
func (m *Model) disconnectProvider(kind string) error {
	s := m.aiS()
	entry := s.providers[kind]
	if entry.Method == ai.MethodKey {
		path, err := ai.CredentialsPath()
		if err != nil {
			return err
		}
		creds, err := ai.LoadCredentials(path)
		if err != nil {
			return err
		}
		if _, ok := creds.Keys[kind]; ok {
			delete(creds.Keys, kind)
			if err := ai.SaveCredentials(path, creds); err != nil {
				return err
			}
		}
	}
	path, err := ai.ProvidersPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		// The list was derived from the setup: write it, so the others stay.
		if err := ai.SaveProviders(path, s.providers); err != nil {
			return err
		}
	}
	if err := ai.RemoveProvider(path, kind); err != nil {
		return err
	}
	cache := s.modelsCacheNow()
	cache.Drop(kind)
	_ = ai.SaveModelsCache(cache)
	return nil
}

// ── The page ─────────────────────────────────────────────────────────────

// providerPageContent is the page of a connected AI: its models, when
// they were fetched, how it is connected and what it costs, the profiles
// that use it; Refresh and Disconnect….
func providerPageContent(sn itermocSnap, row imRow) imContent {
	p, _ := sn.provider(itermocProvKind(row.name))
	c := imContent{icon: imIconModels.String(), name: p.name, sub: p.label + " · " + p.cost, hint: "↑↓ pick in the list · r refresh · ? keys",
		hintNarrow: "Esc back to list · r refresh · ? keys"}
	c.right, c.rightTok = imText("✓")+" connected", theme.AccentGreen
	if theme.CurrentIconMode == theme.IconModeASCII {
		c.right = "v connected"
	}
	switch {
	case p.signedOut:
		c.right, c.rightTok = "! signed out", theme.AccentAmber
		c.card = &imCard{title: "! " + ai.CLITitle(p.cli) + " is signed out", lines: []string{
			"Steps on your plan cannot answer.",
			"Sign in again; the sign-in happens in your browser.",
		}, pills: []imPill{{verb: "addai:" + p.kind + ":" + ai.MethodCLI, label: "Sign in", key: "i", warn: true}}}
	case p.err != "":
		c.right, c.rightTok = "! not refreshed", theme.AccentAmber
		c.card = &imCard{title: "! The models were not refreshed", lines: []string{
			"Why: " + p.err + ".",
			"The last list stays. Press r to try again.",
		}}
	case p.envOnly:
		c.right = imText("✓") + " via environment"
		if theme.CurrentIconMode == theme.IconModeASCII {
			c.right = "v via environment"
		}
	}
	models := imFactSec{title: "MODELS", right: providerUpdated(p), chips: p.models}
	if len(p.models) == 0 {
		models.lines = []string{"No models yet. r loads them."}
	}
	users := sn.provUsers(p.kind)
	usedBy := "no profile"
	if len(users) > 0 {
		usedBy = strings.Join(users, ", ")
	}
	how := p.label
	if p.envOnly {
		how = ai.EnvKeyVar(p.kind) + " (environment)"
	}
	set := imFactSec{title: "SETTINGS", rows: [][2]string{{"connected", how}, {"cost", p.cost}}}
	if p.host != "" {
		set.rows = append(set.rows, [2]string{"server", p.host})
	}
	set.rows = append(set.rows, [2]string{"used by", usedBy})
	note := "Keys stay in the secret store. A sign-in stays with its CLI."
	if p.envOnly {
		note = "The key comes from the environment: unset " + ai.EnvKeyVar(p.kind) + " to stop using it."
	}
	set.lines = []string{"", note}
	c.facts = []imFactSec{models, set}
	c.pills = []imPill{{verb: "refresh", label: "Refresh", key: "r", primary: !p.signedOut}}
	if !p.envOnly {
		c.pills = append(c.pills, imPill{verb: "disconnect", label: "Disconnect…", key: "D"})
	}
	return c
}
