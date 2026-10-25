package app

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/theme"
)

// imVerb runs one verb of the actions menu the way a pick does.
func imVerb(m *Model, kind imRowKind, name, verb string) {
	m.itermocS().menuRow = imRow{kind: kind, sec: imSecOf(kind), name: name}
	m.pickerOpen, m.pickerKind = false, pickerKindItermocActions // the app closes the menu first
	m.handleItermocPickerSelect(verb)
}

func imSecOf(kind imRowKind) string {
	switch kind {
	case imRowProfile:
		return imSecProfiles
	case imRowSlot:
		return imSecModels
	case imRowService:
		return imSecServices
	}
	return imSecAgents
}

// Spec: Duplicate… asks for a name and copies THAT profile (not the active
// one) after the preview card.
func TestItermocDuplicateCopiesTheRowsProfile(t *testing.T) {
	m := itermocTestModel(t, 70, 40)
	imVerb(m, imRowProfile, "review", "duplicate")
	if !m.promptOpen || m.promptKind != promptKindAIProfileNew {
		t.Fatal("no name prompt")
	}
	m.promptOpen = false
	m.onAIProfileNewName("review-copy")
	if !m.confirmOpen || m.confirmKind != confirmKindAIProfileSave {
		t.Fatal("no save preview card")
	}
	pending := m.ai.profileSave
	if pending == nil || pending.name != "review-copy" || len(pending.profile.Steps) != 2 {
		t.Fatalf("the copy is not of review: %+v", pending)
	}
	if m.ai.profNewFrom != "" {
		t.Error("the copy source stays set")
	}
}

// Spec: Trust… on a project profile opens the trust card with Deny first.
func TestItermocTrustVerb(t *testing.T) {
	m := itermocTestModel(t, 70, 40)
	imVerb(m, imRowProfile, "house", "trust")
	if !m.confirmOpen || m.confirmKind != confirmKindAIProfileTrust {
		t.Fatal("no trust card")
	}
	if bs := m.confirm.Buttons(); len(bs) == 0 || bs[0].ID != "deny" {
		t.Errorf("buttons %+v", bs)
	}
	// A built-in profile has no Trust verb.
	_, acts := m.itermocActionsFor(imRow{kind: imRowProfile, sec: imSecProfiles, name: "review"})
	for _, a := range acts {
		if a.id == "trust" {
			t.Error("Trust offered for a built-in profile")
		}
	}
}

// Spec: Runs opens AGENTS, Edit JSON opens profiles.json.
func TestItermocRunsAndJSON(t *testing.T) {
	m := itermocTestModel(t, 70, 40)
	imVerb(m, imRowProfile, "review", "runs")
	if !m.ai.agents.on {
		t.Error("Runs did not open AGENTS")
	}
	imVerb(m, imRowProfile, "review", "json")
	path, _ := ai.ProfilesPath()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("profiles.json was not created for editing: %v", err)
	}
}

// Spec: Set key opens the masked key prompt of the slot's provider; the key
// goes to the credentials, the view only says it was saved (no model picker,
// no provider switch), and the key text never shows.
func TestItermocSetKeyRoute(t *testing.T) {
	imIcons(t, theme.IconModeUnicode)
	m := itermocTestModel(t, 70, 40)
	m.ai.slotFake = map[string]aiSlot{
		ai.SlotMain: {cfg: ai.Config{Kind: ai.KindOllama, Model: "fake-1"}},
		"gpt":       {cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-5-mini"}},
	}
	m.itermocRefreshNow()
	providerBefore := m.ai.settings.Provider
	imVerb(m, imRowSlot, "gpt", "setkey")
	if !m.promptOpen || m.promptKind != promptKindAIKey || !m.ai.keyOnly || m.ai.cfgKind != ai.KindOpenAI {
		t.Fatalf("no key prompt for openai: open=%v kind=%v keyOnly=%v cfgKind=%q", m.promptOpen, m.promptKind, m.ai.keyOnly, m.ai.cfgKind)
	}
	m.promptOpen = false
	m.onAIKeyEntered(imPlantedKey)
	if m.pickerOpen {
		t.Error("the key prompt went on to a model picker")
	}
	if m.ai.keyOnly {
		t.Error("keyOnly stays set")
	}
	if m.ai.settings.Provider != providerBefore {
		t.Errorf("the provider changed to %q", m.ai.settings.Provider)
	}
	path, _ := ai.CredentialsPath()
	creds, _ := ai.LoadCredentials(path)
	if creds.Keys[ai.KindOpenAI] != imPlantedKey {
		t.Error("the key was not stored")
	}
	if f := imFrame(m); strings.Contains(f, imPlantedKey) {
		t.Error("the frame shows the key")
	}
}

// Spec: Set key on a slot without a provider opens Configure Provider.
func TestItermocSetKeyNoProvider(t *testing.T) {
	m := itermocTestModel(t, 70, 40)
	m.ai.slotFake, m.ai.cfg = nil, ai.Config{}
	m.ai.slots = ai.NewSlotPool(map[string]ai.Config{}, nil)
	m.itermocRefreshNow()
	imVerb(m, imRowSlot, "main", "setkey")
	if !m.pickerOpen || m.pickerKind != pickerKindAIProvider {
		t.Errorf("no provider picker: open=%v kind=%v", m.pickerOpen, m.pickerKind)
	}
}

// Spec: the main slot has Pick model…; Edit slots opens config.json.
func TestItermocSlotVerbs(t *testing.T) {
	m := itermocTestModel(t, 70, 40)
	imVerb(m, imRowSlot, ai.SlotMain, "pickmodel")
	if !m.pickerOpen || m.pickerKind != pickerKindAIModel {
		t.Errorf("no model picker: open=%v kind=%v", m.pickerOpen, m.pickerKind)
	}
	imVerb(m, imRowSlot, ai.SlotMain, "slots")
	path, _ := ai.ConfigPath()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("config.json not opened for editing: %v", err)
	}
}

// Spec: Enter on a service opens that service's actions (the same menu as
// AI: MCP Servers…), including Disable / Enable and Remove….
func TestItermocServiceEnterOpensServiceActions(t *testing.T) {
	m := itermocTestModel(t, 70, 40)
	writeUserMCP(t, `{"mcpServers":{"notes":{"command":"notes-mcp"}}}`)
	m.itermoc.refresh(*m, true)
	imCursorTo(t, m, imRowService, "notes")
	imPress(m, "enter")
	if !m.pickerOpen || m.pickerKind != pickerKindMCPServer || m.ai.mcpSel != "notes" {
		t.Fatalf("no service actions: open=%v kind=%v sel=%q", m.pickerOpen, m.pickerKind, m.ai.mcpSel)
	}
	var ids []string
	for _, it := range mcpServerActionItems(m, "notes") {
		ids = append(ids, it.ID)
	}
	got := strings.Join(ids, ",")
	for _, want := range []string{"disable", "remove"} {
		if !strings.Contains(got, want) {
			t.Errorf("service verbs %s lack %s", got, want)
		}
	}
	// Remove… goes through the confirm card.
	m.pickerOpen = false
	m.onMCPServerAction("remove")
	if !m.confirmOpen || m.confirmKind != confirmKindMCPRemove {
		t.Error("Remove… did not ask")
	}
}

// Spec: the buttons route: Connect a service opens the wizard, Log the
// output panel, Edit JSON the profiles file, New profile the name prompt.
func TestItermocButtonsRoute(t *testing.T) {
	m := itermocTestModel(t, 70, 40)
	m.itermocButton(imSecProfiles, "new")
	if !m.promptOpen || m.promptKind != promptKindAIProfileNew {
		t.Error("New profile: no name prompt")
	}
	m.promptOpen = false
	// The wizard's first step asks gh for accounts on a Cmd; the test does not
	// run it (no gh), it sends the answer.
	if cmd := m.itermocButton(imSecServices, "connect"); cmd == nil {
		t.Error("Connect: no command")
	}
	m.onMCPWizEnv(mcpWizEnvMsg{})
	if !m.pickerOpen || m.pickerKind != pickerKindMCPConnect {
		t.Errorf("Connect: picker open=%v kind=%v", m.pickerOpen, m.pickerKind)
	}
	m.pickerOpen = false
	m.itermocButton(imSecAgents, "status")
	if !m.pickerOpen || m.pickerKind != pickerKindMCPServeStatus {
		t.Errorf("Status: picker open=%v kind=%v", m.pickerOpen, m.pickerKind)
	}
}

// Spec: an outside agent's row has Show status…, Allowed tools… (the
// Always-allow list) and Disconnect…, which asks on a card with Cancel first.
func TestItermocAgentVerbs(t *testing.T) {
	m := itermocTestModel(t, 80, 40)
	m.mcpServe = &mcpServeState{clients: []*mcpServeAgent{{id: 7, name: "claude-code", since: time.Now()}}}
	m.itermocRefreshNow()
	row := imRow{kind: imRowAgent, sec: imSecAgents, name: "agent:7"}
	_, acts := m.itermocActionsFor(row)
	var ids []string
	for _, a := range acts {
		ids = append(ids, a.id)
	}
	if got := strings.Join(ids, ","); got != "status,allowed,disconnect" {
		t.Fatalf("agent verbs %s", got)
	}
	imVerb(m, imRowAgent, "agent:7", "disconnect")
	if !m.confirmOpen || m.confirmKind != confirmKindMCPServeDisconnect {
		t.Fatal("no disconnect card")
	}
	if bs := m.confirm.Buttons(); len(bs) == 0 || bs[0].ID != "cancel" {
		t.Errorf("buttons %+v: Cancel must be first", bs)
	}
	m.confirmOpen = false
	imVerb(m, imRowAgent, "agent:7", "allowed")
	if !m.pickerOpen || m.pickerKind != pickerKindAIAutoApprove {
		t.Errorf("no Always-allow list: open=%v kind=%v", m.pickerOpen, m.pickerKind)
	}
}

// Spec: the status row's verbs follow the state: off → Turn on; not
// listening → Start listening now; listening → Stop listening… (a card).
func TestItermocStatusVerbs(t *testing.T) {
	m := itermocTestModel(t, 80, 40)
	row := imRow{kind: imRowAgentStatus, sec: imSecAgents, name: "status"}
	first := func() string {
		_, acts := m.itermocActionsFor(row)
		return acts[0].id
	}
	imInject(m, func(sn *itermocSnap) { sn.mode = "off" })
	if got := first(); got != "serve-on" {
		t.Errorf("mode off: first verb %s", got)
	}
	imInject(m, func(sn *itermocSnap) { sn.mode = "auto" })
	if got := first(); got != "serve-start" {
		t.Errorf("not listening: first verb %s", got)
	}
	m.mcpServe = &mcpServeState{}
	if got := first(); got != "status" {
		t.Errorf("listening: first verb %s", got)
	}
	imVerb(m, imRowAgentStatus, "status", "serve-stop")
	if !m.confirmOpen || m.confirmKind != confirmKindMCPServeStop {
		t.Error("Stop listening… did not ask")
	}
}

