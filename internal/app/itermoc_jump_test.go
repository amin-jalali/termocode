package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/activity"
)

// Spec: a jump into a section opens a folded section, drops a filter that
// would hide it, and puts the cursor on its header.
func TestItermocJumpOpensFoldedSection(t *testing.T) {
	m := itermocTestModel(t, 70, 40)
	st := m.itermoc
	st.collapse[imSecAgents] = true
	st.filter = "zzz"
	m.revealItermocView(true, imSecAgents)
	if st.collapse[imSecAgents] || st.filter != "" {
		t.Errorf("fold=%v filter=%q", st.collapse[imSecAgents], st.filter)
	}
	if r, _ := st.current(); r.kind != imRowSection || r.sec != imSecAgents {
		t.Errorf("cursor on %+v", r)
	}
}

// Spec: the palette commands of the sections open the view at that section.
func TestItermocPaletteSections(t *testing.T) {
	for id, sec := range map[string]string{
		"itermoc-profiles": imSecProfiles, "itermoc-models": imSecModels,
		"itermoc-services": imSecServices, "itermoc-agents": imSecAgents,
	} {
		m := itermocTestModel(t, 70, 40)
		m.activity.SetActive(activity.ViewFiles)
		if _, ok := m.dispatchItermocPalette(id); !ok {
			t.Fatalf("%s not handled", id)
		}
		if r, _ := m.itermoc.current(); r.kind != imRowSection || r.sec != sec {
			t.Errorf("%s: cursor on %+v", id, r)
		}
	}
	// The old commands stay where they were: the status screen is still
	// reachable by its own palette id, and from the OUTSIDE AGENTS menu.
	m := itermocTestModel(t, 70, 40)
	if _, ok := m.dispatchMCPServePalette("mcp-serve-status"); !ok || !m.pickerOpen || m.pickerKind != pickerKindMCPServeStatus {
		t.Error("MCP: Server Status… no longer opens the status screen")
	}
	m.pickerOpen = false
	if cmd, ok := m.dispatchAIPalette("ai-mcp-servers"); !ok || !m.pickerOpen || m.pickerKind != pickerKindMCPServers {
		t.Errorf("AI: MCP Servers… no longer opens the servers picker (cmd %v)", cmd != nil)
	}
}
