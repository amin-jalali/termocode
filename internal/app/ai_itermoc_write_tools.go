package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/agentdag"
	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/lazyre"
	"github.com/amin-jalali/termocode/internal/mcp"
)

// ── AI write tools for the Itermoc view (AH-8) ────────────────────────────
//
// profile_switch, profile_draft, model_slot_draft, service_connect,
// service_set_access and outside_agent_start. Each one goes through the
// chat's confirm card (Deny first, with the focus) and Deny changes
// nothing. A draft is never saved by the tool: Allow opens the form the
// user would open (the profile form, the slot form) with the draft in it,
// and only that form's own Save writes. No tool takes or returns a key,
// token or sign-in: an argument that looks like one is refused before the
// card, and signing in or setting a key stays in the Connect wizard and
// Set key. Remove, Sign out, Revoke and Trust have no tool: they are the
// user's decision. All six are mutating, so a read-only profile step does
// not get them, and outside agents never do (mcpServeAllowList).

// aiItermocWriteToolNames are the tools of this file, in list order.
var aiItermocWriteToolNames = []string{"profile_switch", "profile_draft", "model_slot_draft", "service_connect",
	"service_set_access", "outside_agent_start"}

// aiItermocWriteAllow is the Allow button's title on each tool's card (the
// button id stays "allow").
var aiItermocWriteAllow = map[string]string{
	"profile_switch":      "Switch",
	"profile_draft":       "Open in form",
	"model_slot_draft":    "Open in form",
	"service_connect":     "Open wizard",
	"service_set_access":  "Change",
	"outside_agent_start": "Start",
}

// aiItermocWriteSnap is what the tools check their arguments against
// before the card (Validate runs off the UI goroutine): a copy taken when
// the turn's tools are built.
type aiItermocWriteSnap struct {
	profiles     map[string]string     // name → source
	userProfiles map[string]ai.Profile // the user's own (a draft of that name is a change)
	active       string
	slots        map[string]bool // the known slot names
	userSlots    map[string]ai.ModelSlot
	parallel     int
}

func (m *Model) aiItermocWriteSnapshot() aiItermocWriteSnap {
	sn := aiItermocWriteSnap{profiles: map[string]string{}, userProfiles: map[string]ai.Profile{}, slots: map[string]bool{}, userSlots: map[string]ai.ModelSlot{},
		active: ai.ProfileDefault, parallel: ai.Settings{}.Parallel()}
	if m.ai == nil {
		for _, p := range ai.BuiltinProfiles() {
			sn.profiles[p.Name] = ai.ProfileSourceBuiltin
		}
		for _, n := range []string{ai.SlotMain, ai.SlotQuick, ai.SlotLocal} {
			sn.slots[n] = true
		}
		return sn
	}
	s := m.ai
	for _, n := range s.profiles.Names() {
		p, _ := s.profiles.Get(n)
		sn.profiles[n] = p.Source
		if p.Source == ai.ProfileSourceUser {
			sn.userProfiles[n] = p
		}
	}
	sn.active = s.activeProfile().Name
	for n, ok := range s.slotKnown {
		sn.slots[n] = ok
	}
	if s.slotKnown == nil {
		for _, n := range []string{ai.SlotMain, ai.SlotQuick, ai.SlotLocal} {
			sn.slots[n] = true
		}
	}
	for n, sl := range s.settings.Models {
		sn.userSlots[n] = sl
	}
	sn.parallel = s.settings.Parallel()
	return sn
}

// ── No secrets in arguments ──────────────────────────────────────────────

var aiSecretArgNameRe = lazyre.New(`(?i)key|token|secret|passw|bearer|cookie|credential|auth`)

// aiNoSecretsText is the refusal; it never repeats the value.
const aiNoSecretsText = "an argument looks like a key, token or password. Keys and sign-ins never pass through the AI: " +
	"leave the value out. The user sets a key with Set key (Itermoc, MODELS) and signs in in the Connect wizard"

// aiLooksSecret reports text that holds a key or token: what ai.Redact
// hides, or a word that starts with a token prefix (ai.TokenPrefixes)
// however it is glued to the text around it.
func aiLooksSecret(s string) bool {
	if ai.Redact(s) != s {
		return true
	}
	words := strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("\"'`=:,;()[]{}<>", r)
	})
	for _, w := range words {
		for _, p := range ai.TokenPrefixes {
			if strings.HasPrefix(w, p) && len(w) >= len(p)+ai.TokenMinTail {
				return true
			}
		}
	}
	return false
}

// aiArgsSecretError refuses arguments named like a secret (api_key,
// token …) and values that look like one, anywhere in args.
func aiArgsSecretError(args map[string]any) error {
	bad := false
	var walk func(k string, v any)
	walk = func(k string, v any) {
		if bad {
			return
		}
		if k != "" && aiSecretArgNameRe.MatchString(k) {
			bad = true
			return
		}
		switch x := v.(type) {
		case string:
			bad = aiLooksSecret(x)
		case map[string]any:
			for kk, vv := range x {
				walk(kk, vv)
			}
		case []any:
			for _, vv := range x {
				walk("", vv)
			}
		}
	}
	for k, v := range args {
		walk(k, v)
	}
	if bad {
		return errors.New(aiNoSecretsText)
	}
	return nil
}

// aiCardText is model text for a card: control characters out, one line,
// at most n runes.
func aiCardText(s string, n int) string { return aiOneLineClip(aiCleanText(s), n) }

// ── profile_switch ───────────────────────────────────────────────────────

func (sn aiItermocWriteSnap) checkSwitch(a map[string]any) error {
	if err := aiArgsSecretError(a); err != nil {
		return err
	}
	name := strings.TrimSpace(ai.ArgString(a, "name"))
	if name == "" {
		return errors.New("name is required (a profile from profiles_list)")
	}
	if _, ok := sn.profiles[name]; !ok {
		return fmt.Errorf("there is no profile %q; profiles_list names them", aiCardText(name, 40))
	}
	if name == sn.active {
		return fmt.Errorf("%s is already the active profile", name)
	}
	return nil
}

// aiSecretCardText is the card text of a call with a secret in its
// args: it never names the value (Validate refuses the call).
const aiSecretCardText = "Refused: an argument looks like a key, token or password."

func (sn aiItermocWriteSnap) describeSwitch(a map[string]any) string {
	if aiArgsSecretError(a) != nil {
		return aiSecretCardText
	}
	name := aiCardText(ai.ArgString(a, "name"), 40)
	out := "Switch the AI profile from " + sn.active + " to " + name + ". It applies from your next message."
	if sn.profiles[name] == ai.ProfileSourceProject {
		out += "\n\nIt is a project profile: termocode asks you to trust it first."
	}
	return out
}

// aiToolProfileSwitch switches like the profile picker does: a project
// profile the user has not trusted opens the trust card instead.
func (m *Model) aiToolProfileSwitch(name string) (string, tea.Cmd, error) {
	s := m.aiS()
	s.reloadProfiles()
	p, ok := s.profiles.Get(name)
	if !ok {
		return "", nil, fmt.Errorf("there is no profile %q any more", name)
	}
	if m.aiProfileTrust(p) != aiTrustUse {
		cmd := m.askAIProfileTrust(p, aiTrustAfterSwitch, "")
		return "The project profile " + name + " needs the user's trust first; a card asks them now. " +
			"It is not active yet.", cmd, nil
	}
	cmd := m.setAIProfile(name)
	return "Switched the AI profile to " + name + ". It applies from the user's next message.", cmd, nil
}

// ── profile_draft ────────────────────────────────────────────────────────

// aiDraftStrings is args[key] as a list of strings.
func aiDraftStrings(v any) []string {
	var out []string
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	case []string:
		out = append(out, x...)
	}
	return out
}

// aiDraftNumber is a JSON number (or a numeric string).
func aiDraftNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(x, "$")), 64)
		return f, err == nil
	}
	return 0, false
}

// aiDraftProfile is the profile the arguments describe.
func aiDraftProfile(a map[string]any) (ai.Profile, error) {
	p := ai.Profile{Name: strings.TrimSpace(ai.ArgString(a, "name")), Title: strings.TrimSpace(ai.ArgString(a, "title")),
		Source: ai.ProfileSourceUser}
	if v, ok := a["budget_usd"]; ok && v != nil {
		b, ok := aiDraftNumber(v)
		if !ok {
			return p, errors.New("budget_usd must be a number of dollars, like 0.5")
		}
		p.BudgetUSD, p.BudgetSet = b, true
	}
	if c, ok := a["categories"].(map[string]any); ok {
		cats := map[string]string{}
		for k, v := range c {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				cats[k] = strings.TrimSpace(s)
			}
		}
		if len(cats) > 0 {
			p.Categories = cats
		}
	}
	steps, _ := a["steps"].([]any)
	if len(steps) == 0 {
		return p, errors.New("steps is required: a list of steps, each with a title and a prompt")
	}
	for i, raw := range steps {
		st, ok := raw.(map[string]any)
		if !ok {
			return p, fmt.Errorf("step %d is not an object", i+1)
		}
		step := ai.ProfileStep{ID: strings.TrimSpace(ai.ArgString(st, "id")), Title: strings.TrimSpace(ai.ArgString(st, "title")),
			Slot: strings.TrimSpace(ai.ArgString(st, "slot")), Category: strings.TrimSpace(ai.ArgString(st, "category")),
			System: strings.TrimSpace(ai.ArgString(st, "prompt")), Plan: ai.ArgBool(st, "planner"), Deps: aiDraftStrings(st["after"])}
		step.Tools.Preset = strings.TrimSpace(ai.ArgString(st, "tools"))
		p.Steps = append(p.Steps, step)
	}
	return p, nil
}

func (sn aiItermocWriteSnap) checkDraft(a map[string]any) (ai.Profile, error) {
	if err := aiArgsSecretError(a); err != nil {
		return ai.Profile{}, err
	}
	p, err := aiDraftProfile(a)
	if err != nil {
		return p, err
	}
	if err := ai.ValidProfileName(p.Name); err != nil {
		return p, err
	}
	if src, ok := sn.profiles[p.Name]; ok && src != ai.ProfileSourceUser {
		return p, fmt.Errorf("%q is a %s profile; pick a new name (the user's own profiles may be replaced)", p.Name, aiProfileSourceText(src))
	}
	if err := ai.ValidateProfile(p, sn.slots, sn.parallel); err != nil {
		return p, fmt.Errorf("%v (models_list names the slots; model_slot_draft adds one)", err)
	}
	return p, nil
}

// aiDraftLines is a profile as card lines (the same facts the preview
// card shows).
func aiDraftLines(p ai.Profile) []string {
	var out []string
	if p.Title != "" {
		out = append(out, "title: "+aiCardText(p.Title, 60))
	}
	if p.BudgetUSD > 0 {
		out = append(out, "budget: $"+strconv.FormatFloat(p.BudgetUSD, 'f', -1, 64))
	}
	for _, c := range []string{agentdag.CategoryQuick, agentdag.CategoryDeep} {
		if sl := p.Categories[c]; sl != "" {
			out = append(out, c+" steps use "+aiCardText(sl, 30))
		}
	}
	for i, st := range p.Steps {
		title := st.Title
		if title == "" {
			title = st.ID
		}
		tools := ai.StepTools(st)
		line := fmt.Sprintf("%d · %s · %s · tools %s", i+1, aiCardText(title, 40), aiCardText(p.StepLabel(st), 30), tools)
		if st.Plan {
			line += " · planner"
		}
		if len(st.Deps) > 0 {
			line += " · after " + aiCardText(strings.Join(st.Deps, ", "), 30)
		}
		out = append(out, line)
		if head := aiFirstLine(st.System); head != "" {
			out = append(out, "    "+aiCardText(head, 70))
		}
	}
	return out
}

// aiLineDiff marks lines: "  " the same, "- " only in old, "+ " only in
// new (a longest common subsequence; profiles are short).
func aiLineDiff(old, fresh []string) []string {
	n, k := len(old), len(fresh)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, k+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := k - 1; j >= 0; j-- {
			if old[i] == fresh[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []string
	i, j := 0, 0
	for i < n || j < k {
		switch {
		case i < n && j < k && old[i] == fresh[j]:
			out = append(out, "  "+old[i])
			i, j = i+1, j+1
		case i < n && (j == k || lcs[i+1][j] >= lcs[i][j+1]): // the old line first, as in a diff
			out = append(out, "- "+old[i])
			i++
		default:
			out = append(out, "+ "+fresh[j])
			j++
		}
	}
	return out
}

// describeDraft is the card: what the draft is, as a diff against the
// user's profile of that name when there is one.
func (sn aiItermocWriteSnap) describeDraft(a map[string]any) string {
	p, err := sn.checkDraft(a)
	if err != nil {
		return "Draft profile (invalid: " + aiCardText(err.Error(), 80) + ")"
	}
	var b strings.Builder
	var lines []string
	if old, ok := sn.userProfiles[p.Name]; ok {
		fmt.Fprintf(&b, "Change your profile %s. Nothing is saved yet: Open in form shows the change in the profile form, "+
			"and only its Save writes profiles.json.\n\n", p.Name)
		lines = aiLineDiff(aiDraftLines(old), aiDraftLines(p))
	} else {
		fmt.Fprintf(&b, "New profile %s. Nothing is saved yet: Open in form shows the draft in the profile form, "+
			"and only its Save writes profiles.json.\n\n", p.Name)
		for _, l := range aiDraftLines(p) {
			lines = append(lines, "+ "+l)
		}
	}
	b.WriteString(strings.Join(lines, "\n"))
	return b.String()
}

// aiToolProfileDraft opens the profile form with the draft. It never
// writes: the form's Save shows the preview card, and its Save writes.
func (m *Model) aiToolProfileDraft(p ai.Profile) (string, tea.Cmd, error) {
	if m.profileFormOpen {
		return "", nil, errors.New("the user has a profile form open; ask them to finish it first")
	}
	s := m.aiS()
	s.reloadProfiles()
	editing := false
	if q, ok := s.profiles.Get(p.Name); ok {
		if q.Source != ai.ProfileSourceUser {
			return "", nil, fmt.Errorf("%q is a %s profile now; pick a new name", p.Name, aiProfileSourceText(q.Source))
		}
		editing = true
	}
	cmd := m.openAIProfileForm(p.Name, p, editing)
	return "The profile form is open with the draft of " + p.Name + ". Nothing is saved: the user checks it and presses " +
		"Save (or Cancel). Do not send the draft again; wait for the user.", cmd, nil
}

// ── model_slot_draft ─────────────────────────────────────────────────────

func aiDraftSlot(a map[string]any) (string, ai.ModelSlot) {
	return strings.TrimSpace(ai.ArgString(a, "name")), ai.ModelSlot{
		Provider: strings.ToLower(strings.TrimSpace(ai.ArgString(a, "provider"))),
		Model:    strings.TrimSpace(ai.ArgString(a, "model")),
		BaseURL:  strings.TrimSpace(ai.ArgString(a, "base_url")),
	}
}

func (sn aiItermocWriteSnap) checkSlot(a map[string]any) error {
	if err := aiArgsSecretError(a); err != nil {
		return err
	}
	name, sl := aiDraftSlot(a)
	if name == ai.SlotMain {
		return errors.New(`"main" is the chat's own model; the user changes it with AI: Configure Provider…`)
	}
	if err := ai.CheckModelSlot(name, sl); err != nil {
		return err
	}
	if err := ai.CheckSlotBaseURL(sl.BaseURL); err != nil {
		return err
	}
	if sl.BaseURL != "" {
		if u, err := url.Parse(sl.BaseURL); err == nil && (u.RawQuery != "" || u.Fragment != "") {
			return errors.New("base_url must not hold a query or fragment; a key goes to the secret store with Set key")
		}
	}
	return nil
}

// aiSlotWhat is "openai · gpt-5-mini" (the default model named).
func aiSlotWhat(sl ai.ModelSlot) string {
	what := sl.Provider
	switch {
	case sl.Model != "":
		what += " · " + sl.Model
	case ai.DefaultModel(sl.Provider) != "":
		what += " · " + ai.DefaultModel(sl.Provider) + " (default)"
	}
	if h := aiSafeHost(sl.BaseURL); h != "" {
		what += " · " + h
	}
	return what
}

// aiSlotKeyText says what the slot needs after Save; never a key.
func aiSlotKeyText(kind string) string {
	switch kind {
	case ai.KindOllama, ai.KindLMStudio:
		return "A local model needs no key."
	case ai.KindCompatible:
		return "If the server needs a key, you set it after Save (Set key); it goes to the secret store."
	case ai.KindClaudeOAuth:
		return "It needs a Claude sign-in; you sign in after Save."
	}
	label := ai.KindLabel(kind)
	return "It needs " + aOrAn(label) + " " + label + " key; you set it after Save (Set key). The AI never sees keys."
}

func (sn aiItermocWriteSnap) describeSlot(a map[string]any) string {
	if err := sn.checkSlot(a); err != nil {
		return "Model slot draft (invalid: " + aiCardText(err.Error(), 80) + ")"
	}
	name, sl := aiDraftSlot(a)
	name = aiCardText(name, 30)
	sl.Model = aiCardText(sl.Model, 60)
	var b strings.Builder
	if old, ok := sn.userSlots[name]; ok {
		fmt.Fprintf(&b, "Change model slot %s. Nothing is saved yet: Open in form shows it in the slot form, and only its Save "+
			"writes config.json.\n\n- %s\n+ %s", name, aiSlotWhat(old), aiSlotWhat(sl))
	} else {
		fmt.Fprintf(&b, "Add model slot %s → %s. Nothing is saved yet: Open in form shows it in the slot form, and only its "+
			"Save writes config.json.", name, aiSlotWhat(sl))
	}
	b.WriteString("\n\n" + aiSlotKeyText(sl.Provider))
	return b.String()
}

// aiToolSlotDraft opens the slot form with the draft (Add, or Edit of the
// user's slot of that name). It never writes.
func (m *Model) aiToolSlotDraft(name string, sl ai.ModelSlot) (string, tea.Cmd, error) {
	if m.connectOpen || m.addAIOpen {
		return "", nil, errors.New("the user has a card open; ask them to finish it first")
	}
	m.aiS()
	st := m.itermocS()
	switch _, mine := m.imUserSlot(name); {
	case mine:
		st.form = &imSlotForm{edit: true, name: name}
	case name == ai.SlotQuick || name == ai.SlotLocal:
		st.form = &imSlotForm{edit: true, name: name, override: true}
	default:
		st.form = &imSlotForm{}
	}
	cmd := m.showItermocSlotForm(sl.Provider, sl.Model, sl.BaseURL, name)
	return "The slot form is open with " + name + " → " + aiSlotWhat(sl) + ". Nothing is saved: the user checks it and " +
		"presses Save, then sets the key themselves if one is needed. Wait for the user.", cmd, nil
}

// ── service_connect ──────────────────────────────────────────────────────

// aiPresetNames are the services the Connect wizard knows.
func aiPresetNames() []string {
	var out []string
	for _, p := range mcp.Presets() {
		out = append(out, p.Name)
	}
	return out
}

func checkServiceConnect(a map[string]any) error {
	if err := aiArgsSecretError(a); err != nil {
		return err
	}
	name := strings.TrimSpace(ai.ArgString(a, "service"))
	if name == "" {
		return errors.New("service is required")
	}
	if _, ok := mcp.PresetByName(name); ok || len(mcpServerEntries(name)) > 0 {
		return nil
	}
	return fmt.Errorf("unknown service %q: the wizard knows %s, and the services in mcp.json (mcp_services_list). "+
		"For another server the user picks Custom URL in AI: Connect a Service…", aiCardText(name, 40), strings.Join(aiPresetNames(), ", "))
}

func describeServiceConnect(a map[string]any) string {
	if aiArgsSecretError(a) != nil {
		return aiSecretCardText
	}
	name := strings.TrimSpace(ai.ArgString(a, "service"))
	title := aiCardText(name, 40)
	if p, ok := mcp.PresetByName(name); ok {
		title = p.Title
	}
	return "Open the Connect wizard for " + title + "? You sign in yourself; the AI gets no token."
}

// aiToolServiceConnect opens the wizard as the Connect picker or the
// row's Sign in would: the user signs in, the AI never sees a token.
func (m *Model) aiToolServiceConnect(name string) (string, tea.Cmd, error) {
	if len(mcpServerEntries(name)) > 0 {
		return "The Connect wizard for " + name + " is open; the user signs in there. You get no token. " +
			"Afterwards mcp_services_list shows the state.", m.mcpConnectServer(name), nil
	}
	p, ok := mcp.PresetByName(name)
	if !ok {
		return "", nil, fmt.Errorf("unknown service %q", name)
	}
	start := func(m *Model) tea.Cmd { return m.mcpConnectEnv(p.Name, p.Name) }
	cmd, wait := m.secretsOpenFirst(start)
	if !wait {
		cmd = start(m)
	}
	return "The Connect wizard for " + p.Title + " is opening; the user signs in there. You get no token. " +
		"Afterwards mcp_services_list shows the state.", cmd, nil
}

// ── service_set_access ───────────────────────────────────────────────────

// aiServiceAccessNow is a service's access switch: has=false when it has
// none (not a known service with a read-only endpoint or scopes).
func aiServiceAccessNow(name string) (readOnly, has bool) {
	p, ok := mcpPresetFor(name)
	entries := mcpServerEntries(name)
	if !ok || len(entries) == 0 || (p.ReadOnlyURL == "" && p.WriteScopes == nil) {
		return false, false
	}
	return mcpEntryReadOnly(entries[0], p), true
}

func checkServiceAccess(a map[string]any) error {
	if err := aiArgsSecretError(a); err != nil {
		return err
	}
	name := strings.TrimSpace(ai.ArgString(a, "name"))
	if _, ok := a["writes"]; !ok {
		return errors.New("writes is required: true allows writes, false makes the service read only")
	}
	if name == "" {
		return errors.New("name is required (a service from mcp_services_list)")
	}
	ro, has := aiServiceAccessNow(name)
	if !has {
		return fmt.Errorf("%s has no read-only switch (only known services such as GitHub and Linear have one)", aiCardText(name, 40))
	}
	writes := ai.ArgBool(a, "writes")
	if writes == !ro {
		if writes {
			return fmt.Errorf("%s already allows writes", name)
		}
		return fmt.Errorf("%s is already read only", name)
	}
	return nil
}

func describeServiceAccess(a map[string]any) string {
	if aiArgsSecretError(a) != nil {
		return aiSecretCardText
	}
	name := aiCardText(ai.ArgString(a, "name"), 40)
	if ai.ArgBool(a, "writes") {
		return "Allow writes for " + name + "? Its tools may then create, comment and change; each write still asks you first. " +
			"It may need a new sign-in."
	}
	return "Make " + name + " read only? Its tools can then only read."
}

// ── outside_agent_start ──────────────────────────────────────────────────

// aiAgentIDs are the outside agents the tool can start.
func aiAgentIDs() []string {
	var out []string
	for _, a := range agents.Registry {
		out = append(out, a.ID)
	}
	return out
}

func checkAgentStart(a map[string]any) error {
	if err := aiArgsSecretError(a); err != nil {
		return err
	}
	ag, ok := agents.ByID(strings.TrimSpace(ai.ArgString(a, "agent")))
	if !ok {
		return fmt.Errorf("agent must be one of %s", strings.Join(aiAgentIDs(), ", "))
	}
	if _, err := agentLookPath(ag.Bin); err != nil {
		return fmt.Errorf("%s is not installed (%s is not on PATH); the user installs it first (guide: guides/agents.md)", ag.Title, ag.Bin)
	}
	return nil
}

func describeAgentStart(a map[string]any) string {
	ag, ok := agents.ByID(strings.TrimSpace(ai.ArgString(a, "agent")))
	if !ok {
		return "Start an outside agent (invalid)"
	}
	out := "Start " + ag.Title + " in a new terminal tab (it runs " + ag.Bin + ")"
	switch ag.MCP.Kind {
	case agents.MCPFlag, agents.MCPProjectFile:
		out += "? It can use termocode's tools; the risky ones still ask you."
	default:
		out += "? It gets no termocode tools by itself (" + ag.MCP.NoteTitle + ")."
	}
	return out
}

// aiStartOutsideAgent starts a as "Agent: Start …" in the palette does
// (aiStartOutsideAgentHook replaces it in a test).
func aiStartOutsideAgent(m *Model, a agents.Agent) (tea.Cmd, error) {
	if aiStartOutsideAgentHook != nil {
		return aiStartOutsideAgentHook(m, a)
	}
	if m.nvim == nil {
		return nil, errors.New("the terminal is not ready")
	}
	return m.startAgent(a), nil
}

var aiStartOutsideAgentHook func(m *Model, a agents.Agent) (tea.Cmd, error)

// ── The tools ────────────────────────────────────────────────────────────

// aiProfileStepProps is the JSON schema of one profile step (profile_draft,
// agent_run_start).
func aiProfileStepProps() map[string]any {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	return map[string]any{
		"id":       str("step id (optional; step-1, step-2 … when left out)"),
		"title":    str("what the step does, e.g. Review"),
		"slot":     str("model slot from models_list (optional; main when left out)"),
		"category": str("instead of slot, the kind of work (OMX-16): quick, deep, visual, writing, architect (read-only), ultrabrain, or a category from ai_categories"),
		"tools":    map[string]any{"type": "string", "enum": []string{agentdag.PresetNone, agentdag.PresetReadOnly, agentdag.PresetAll}, "description": "which tools the step gets (default all)"},
		"prompt":   str("the step's instructions"),
		"planner":  map[string]any{"type": "boolean", "description": "the step plans the run (at most one)"},
		"after":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "step ids this step waits for (needs ai_parallel_max 2 or more)"},
	}
}

// aiItermocWriteTools are the write tools of the Itermoc view.
func (m *Model) aiItermocWriteTools(call aiIDECaller) []ai.Tool {
	sn := m.aiItermocWriteSnapshot()
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	stepProps := aiProfileStepProps()
	return []ai.Tool{
		{
			Name: "profile_switch",
			Description: "Switch the chat's AI profile (see profiles_list); it applies from the user's next message. " +
				"The user confirms on a card. A project profile the user has not trusted yet asks the user first.",
			Params:   map[string]any{"name": str("profile name from profiles_list")},
			Required: []string{"name"},
			// No Always allow class: every call asks (owner 2026-10-26).
			Mutating: true,
			Validate: sn.checkSwitch,
			Describe: sn.describeSwitch,
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				if err := sn.checkSwitch(a); err != nil {
					return "", err
				}
				name := strings.TrimSpace(ai.ArgString(a, "name"))
				return call(ctx, func(m *Model) (string, tea.Cmd, error) { return m.aiToolProfileSwitch(name) })
			},
		},
		{
			Name: "profile_draft",
			Description: "Draft an AI profile (a pipeline of steps) for the user. The card shows the draft (a diff when it " +
				"changes one of the user's own profiles); Allow opens the profile form with it, and nothing is saved until " +
				"the user presses Save there. Use slots from models_list. Never put keys in it.",
			Params: map[string]any{
				"name":       str("profile name (letters, digits, - _ .); a built-in or project name is refused"),
				"title":      str("a short title (optional)"),
				"budget_usd": map[string]any{"type": "number", "description": "pause the run once it cost this much (optional)"},
				"categories": map[string]any{"type": "object", "description": "slots for quick and deep steps (optional)",
					"properties": map[string]any{"quick": str("slot for quick steps"), "deep": str("slot for deep steps")}},
				"steps": map[string]any{"type": "array", "description": "the steps in order",
					"items": map[string]any{"type": "object", "properties": stepProps, "required": []string{"title"}}},
			},
			Required: []string{"name", "steps"},
			Mutating: true,
			Validate: func(a map[string]any) error { _, err := sn.checkDraft(a); return err },
			Describe: sn.describeDraft,
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				p, err := sn.checkDraft(a)
				if err != nil {
					return "", err
				}
				return call(ctx, func(m *Model) (string, tea.Cmd, error) { return m.aiToolProfileDraft(p) })
			},
		},
		{
			Name: "model_slot_draft",
			Description: "Draft a model slot (a named provider and model that profile steps can use) for the user. The card " +
				"shows it; Allow opens the slot form with it, and nothing is saved until the user presses Save there. Never " +
				"a key: the user sets it after Save.",
			Params: map[string]any{
				"name":     str("slot name, e.g. gpt (not main)"),
				"provider": map[string]any{"type": "string", "enum": ai.Kinds, "description": "the provider"},
				"model":    str("model id (optional; the provider's default)"),
				"base_url": str("server address for openai-compatible, ollama or lmstudio (optional; no user, password or query)"),
			},
			Required: []string{"name", "provider"},
			Mutating: true,
			Validate: sn.checkSlot,
			Describe: sn.describeSlot,
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				if err := sn.checkSlot(a); err != nil {
					return "", err
				}
				name, sl := aiDraftSlot(a)
				return call(ctx, func(m *Model) (string, tea.Cmd, error) { return m.aiToolSlotDraft(name, sl) })
			},
		},
		{
			Name: "service_connect",
			Description: "Open the Connect wizard for an MCP service (" + strings.Join(aiPresetNames(), ", ") + ", or one in " +
				"mcp.json) so the user can sign in. The user confirms first and signs in themselves; you never get a token.",
			Params:   map[string]any{"service": str("service name, e.g. github")},
			Required: []string{"service"},
			Mutating: true,
			Validate: checkServiceConnect,
			Describe: describeServiceConnect,
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				if err := checkServiceConnect(a); err != nil {
					return "", err
				}
				name := strings.TrimSpace(ai.ArgString(a, "service"))
				return call(ctx, func(m *Model) (string, tea.Cmd, error) { return m.aiToolServiceConnect(name) })
			},
		},
		{
			Name: "service_set_access",
			Description: "Allow writes for a connected MCP service, or make it read only (known services with a read-only " +
				"endpoint or scopes, such as GitHub and Linear). The user confirms first; more access may need a new sign-in.",
			Params: map[string]any{
				"name":   str("service name from mcp_services_list"),
				"writes": map[string]any{"type": "boolean", "description": "true allows writes; false makes it read only"},
			},
			Required: []string{"name", "writes"},
			Mutating: true,
			Validate: checkServiceAccess,
			Describe: describeServiceAccess,
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				if err := checkServiceAccess(a); err != nil {
					return "", err
				}
				name, writes := strings.TrimSpace(ai.ArgString(a, "name")), ai.ArgBool(a, "writes")
				return call(ctx, func(m *Model) (string, tea.Cmd, error) {
					cmd := m.mcpSetWrites(name, writes)
					if ro, _ := aiServiceAccessNow(name); ro == writes {
						return "", cmd, fmt.Errorf("%s did not change", name)
					}
					if writes {
						return name + " allows writes now; each write still asks the user. If it needs a new sign-in, " +
							"the wizard is open for the user.", cmd, nil
					}
					return name + " is read only now.", cmd, nil
				})
			},
		},
		{
			Name: "outside_agent_start",
			Description: "Start an outside coding agent (" + strings.Join(aiAgentIDs(), ", ") + ") in a new terminal tab, " +
				"with termocode's tools when the agent supports it. The user confirms first.",
			Params: map[string]any{
				"agent": map[string]any{"type": "string", "enum": aiAgentIDs(), "description": "which agent"},
			},
			Required: []string{"agent"},
			// No Always allow class: every call asks (owner 2026-10-26).
			Mutating: true,
			Validate: checkAgentStart,
			Describe: describeAgentStart,
			Run: func(ctx context.Context, a map[string]any) (string, error) {
				if err := checkAgentStart(a); err != nil {
					return "", err
				}
				ag, _ := agents.ByID(strings.TrimSpace(ai.ArgString(a, "agent")))
				return call(ctx, func(m *Model) (string, tea.Cmd, error) {
					cmd, err := aiStartOutsideAgent(m, ag)
					if err != nil {
						return "", nil, err
					}
					return ag.Title + " is starting in a terminal tab.", cmd, nil
				})
			},
		},
	}
}

// aiItermocWriteTool reports a tool of this file.
func aiItermocWriteTool(name string) bool {
	i := sort.SearchStrings(aiItermocWriteSorted, name)
	return i < len(aiItermocWriteSorted) && aiItermocWriteSorted[i] == name
}

var aiItermocWriteSorted = func() []string {
	out := append([]string(nil), aiItermocWriteToolNames...)
	sort.Strings(out)
	return out
}()
