package app

import (
	"strings"
	"testing"
)

// The palette command shows ALL CONTEXTS and lists every context of the
// fake docker; the rows read, fold, copy and reload; running it again
// hides the section.
func TestCovContainerAllContexts(t *testing.T) {
	m, f := ctxSession(t)
	var copied []string
	defer SetClipboardSink(func(s string) { copied = append(copied, s) })()
	f.Set(t, "fail-prod.err", "prod is down")
	st := m.services
	cmd := m.toggleAllContexts()
	if !st.allContexts || !strings.Contains(ctrToast(m), "ALL CONTEXTS shown") {
		t.Fatal("not shown")
	}
	if !strings.Contains(svcHintText(st), "Loading…") || st.allContextsCount() != "…" {
		t.Fatal("not loading")
	}
	msg := ctrFind[allContextsMsg](t, ctrMsgs(cmd))
	_ = m.onAllContexts(allContextsMsg{gen: msg.gen + 9})
	if st.multiLoaded {
		t.Fatal("stale answer applied")
	}
	_ = m.onAllContexts(msg)
	if !st.multiLoaded || len(st.multi.Containers) == 0 || len(st.multi.Problems) != 1 {
		t.Fatalf("multi %+v", st.multi)
	}
	if !strings.Contains(st.allContextsCount(), "1 not answering") {
		t.Fatal(st.allContextsCount())
	}
	r := selectRow(t, st, func(r svcRow) bool { return r.kind == svcRowHostItem && r.host == hostAllContainer })
	if l1, l2 := st.allContextsFooter(r); !strings.Contains(l1, "default") || !strings.Contains(l2, "Enter use this context") {
		t.Fatalf("footer %q %q", l1, l2)
	}
	if st.renderAllContainer(r, true, true, 40) == "" || st.renderAllContainer(svcRow{idx: 99}, false, false, 40) != "" {
		t.Fatal("render")
	}
	if l1, _ := st.allContextsFooter(svcRow{idx: -1}); l1 != "" {
		t.Fatal("footer out of range")
	}
	if _, ok := m.allContextsKey("y"); !ok || len(copied) != 1 {
		t.Fatal("y did not copy")
	}
	if cmd, ok := m.allContextsKey("r"); !ok || cmd == nil {
		t.Fatal("r did not reload")
	}
	if _, ok := m.allContextsKey("R"); ok {
		t.Fatal("R is not only ours")
	}
	if _, ok := m.allContextsKey("zz"); ok {
		t.Fatal("other key")
	}
	if cmd, ok := m.allContextsKey("enter"); !ok || cmd == nil {
		t.Fatal("enter did not switch")
	}
	// Fold a group, then the section.
	st.collapsed[hostGroup(allContextsSection, "default")] = true
	for _, row := range st.allContextsRows() {
		if row.host == hostAllContainer && st.multi.Containers[row.idx].Source == "default" {
			t.Fatal("folded group listed")
		}
	}
	st.collapsed[hostSection(allContextsSection)] = true
	if rows := st.allContextsRows(); len(rows) != 1 || m.loadAllContexts() != nil {
		t.Fatal("folded section")
	}
	st.collapsed[hostSection(allContextsSection)] = false
	// Not on an ALL CONTEXTS row.
	st.cursor = 0
	if _, ok := m.allContextsKey("y"); ok {
		t.Fatal("y off the section")
	}
	selectRow(t, st, func(r svcRow) bool { return r.kind == svcRowSection && r.section == hostSection(allContextsSection) })
	if _, ok := m.allContextsKey("y"); ok {
		t.Fatal("y on the header")
	}
	// Nothing anywhere.
	st.multi.Containers, st.multi.Problems = nil, nil
	if !strings.Contains(svcHintText(st), "No containers in any context") {
		t.Fatalf("hints %s", svcHintText(st))
	}
	if len(multiPaletteItems()) != 1 || !isAllContextsSection("host/"+allContextsSection) || isAllContextsSection("host/images") {
		t.Fatal("palette / section key")
	}
	// No contexts known: they are listed first.
	st.contextRows = nil
	if msg := ctrFind[allContextsMsg](t, ctrMsgs(m.loadAllContexts())); len(msg.res.Containers)+len(msg.res.Problems) == 0 {
		t.Fatal("no answer without known contexts")
	}
	_ = m.toggleAllContexts()
	if st.allContexts || !strings.Contains(ctrToast(m), "ALL CONTEXTS hidden") {
		t.Fatal("not hidden")
	}
	if _, ok := m.allContextsKey("y"); ok || m.onAllContexts(msg) != nil {
		t.Fatal("hidden section answered")
	}
}
