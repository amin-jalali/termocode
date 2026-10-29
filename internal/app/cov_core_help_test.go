package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/help"
	"github.com/amin-jalali/termocode/internal/picker"
)

// The focus states the main table does not list, each with its section.
func TestCovCoreHelpTopicMoreFocus(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	base := hoverModel(t)
	sidebar := func(v activity.View) func(*Model) {
		return func(m *Model) {
			m.showExp, m.focus = true, FocusExplorer
			m.activity.SetActive(v)
		}
	}
	cases := []struct {
		name string
		set  func(m *Model)
		want string
	}{
		{"markdown preview", func(m *Model) { m.previewOpen = true }, helpContexts["preview"]},
		{"settings on a header", func(m *Model) {
			m.settingsModal = newSettingsModal()
			m.settingsModal.cursor = 0
			m.settingsModalOpen = true
		}, helpContexts["settings"]},
		{"empty palette", func(m *Model) {
			m.picker = picker.NewItems("palette", nil)
			m.pickerOpen, m.pickerKind = true, pickerKindPalette
		}, "reference/commands.md"},
		{"palette on a run-time command", func(m *Model) {
			m.picker = picker.NewItems("palette", []picker.Item{{ID: "user-x", Title: "Docs: Rebuild"}})
			m.pickerOpen, m.pickerKind = true, pickerKindPalette
		}, "reference/commands.md#docs?Docs: Rebuild"},
		{"other picker: what is under it", func(m *Model) {
			m.picker = picker.NewItems("files", []picker.Item{{ID: "a", Title: "a"}})
			m.pickerOpen, m.pickerKind = true, pickerKindFiles
		}, helpContexts["welcome"]},
		{"db compare picker", func(m *Model) {
			m.picker = picker.NewItems("db", []picker.Item{{ID: "a", Title: "a"}})
			m.pickerOpen, m.pickerKind = true, pickerKindDBCompareSource
		}, helpContexts["db-compare"]},
		{"slot form", func(m *Model) {
			m.connectOpen = true
			m.itermoc = newItermocState()
			m.itermoc.form = &imSlotForm{}
		}, itermocGuidePage + "#models"},
		{"add AI card", func(m *Model) { m.addAIOpen = true }, helpContexts["add-ai"]},
		{"profile form", func(m *Model) { m.profileFormOpen = true }, helpContexts["profile-form"]},
		{"commit view", func(m *Model) { m.commitView = &commitViewState{open: true} }, helpContexts["commit-view"]},
		{"output tab", func(m *Model) { m.panelFocused, m.panelActive = true, panelKindOutput }, helpContexts["output"]},
		{"tests tab", func(m *Model) { m.panelFocused, m.panelActive = true, panelKindTests }, helpContexts["tests-panel"]},
		{"run view", sidebar(activity.ViewRun), activityHelp["Run"]},
		{"testing view", sidebar(activity.ViewTests), activityHelp["Testing"]},
		{"search view", sidebar(activity.ViewSearch), helpContexts["search-view"]},
		{"runs view", sidebar(activity.ViewRuns), activityHelp["Runs"]},
		{"databases view", sidebar(activity.ViewDatabases), helpContexts["databases"]},
		{"http response", func(m *Model) { m.editor.SetMeta("/tmp/r/Response", false, "httpresponse") }, helpContexts["http-response"]},
		{"http file", func(m *Model) { m.editor.SetMeta("/tmp/r/api.http", false, "http") }, helpContexts["http-file"]},
		{"db result", func(m *Model) { m.editor.SetMeta("/tmp/x/SQL Result", false, "") }, helpContexts["db-result"]},
		{"a go file", func(m *Model) { m.editor.SetMeta("/tmp/r/main.go", false, "go") }, helpContexts["editor"]},
	}
	for _, c := range cases {
		m := base
		m.focus = FocusEditor
		c.set(&m)
		if got := m.helpTopicForFocus(); got != c.want {
			t.Errorf("%s: topic %q, want %q", c.name, got, c.want)
		}
	}
	// An unknown sidebar view has no topic of its own: the editor's.
	m := base
	sidebar(activity.View(999))(&m)
	if got := m.sidebarHelpTopic(); got != "" {
		t.Errorf("unknown view topic %q", got)
	}
}

// Inside the guide there is nothing more to explain; a topic that is
// not a guide link opens nothing.
func TestCovCoreContextHelpNothingToOpen(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := hoverModel(t)
	l, err := help.Parse(helpContexts["editor"])
	if err != nil {
		t.Fatal(err)
	}
	m = helpFeed(t, m, m.openHelp(l))
	if !m.helpActive() {
		t.Fatal("the guide did not open")
	}
	if m.helpTopicForFocus() != "" || m.openContextHelp() != nil {
		t.Error("context help inside the guide did something")
	}
	m = hoverModel(t)
	m.tip.shown, m.tip.help = true, "::not a page::"
	if m.openContextHelp() != nil {
		t.Error("a bad topic opened the guide")
	}
}
