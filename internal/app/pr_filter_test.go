package app

import (
	"context"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Git: Pull Requests (Filter)… offers the common lists; a choice reads
// that list with the matching gh flags and the picker title names it.
func TestPRFilterPicker(t *testing.T) {
	f := &prFakeGH{answers: map[string]string{"pr list": prListText}}
	m := prModel(f)
	if cmd, ok := m.dispatchPRPalette("git-pr-filter"); !ok || cmd != nil || !m.pickerOpen || m.pickerKind != pickerKindPRFilter {
		t.Fatalf("filter picker not open (%v %v)", m.pickerOpen, m.pickerKind)
	}
	box := ansi.Strip(m.picker.Box())
	for _, want := range []string{"Open", "Review Requested from Me", "Created by Me", "Assigned to Me", "Drafts", "Closed", "Merged", "All", "Search…"} {
		if !strings.Contains(box, want) {
			t.Errorf("picker misses %q:\n%s", want, box)
		}
	}
	m.pickerOpen = false
	cmd, _ := m.handlePRPickerSelect("merged")
	prDo(t, m, cmd)
	if !f.called("pr list --state merged") || !m.pickerOpen || m.pickerKind != pickerKindPRList ||
		!strings.Contains(ansi.Strip(m.picker.Box()), "Merged Pull Requests") {
		t.Errorf("calls %v open %v", f.calls, m.pickerOpen)
	}
	m.pickerKind = pickerKindPRFilter
	cmd, _ = m.handlePRPickerSelect("search")
	if cmd != nil || !m.promptOpen || m.promptKind != promptKindPRSearch {
		t.Fatal("search prompt not open")
	}
	cmd, _ = m.handlePRPromptSubmit("label:bug")
	prDo(t, m, cmd)
	if !f.called("pr list --state all --limit 50 --json "+"number") || !f.called("pr list --state all") {
		t.Errorf("calls %v", f.calls)
	}
	found := false
	for _, c := range f.calls {
		found = found || strings.HasSuffix(c, "--search=label:bug")
	}
	if !found {
		t.Errorf("search not sent: %v", f.calls)
	}
}

// Closed and merged rows say so in the hint.
func TestPRRowHintState(t *testing.T) {
	f := &prFakeGH{answers: map[string]string{"pr list": `[{"number":5,"title":"Old","state":"MERGED"},{"number":4,"title":"Gone","state":"CLOSED"}]`}}
	m := prModel(f)
	m.pickerKind = pickerKindPRFilter
	cmd, _ := m.handlePRPickerSelect("all")
	prDo(t, m, cmd)
	box := ansi.Strip(m.picker.Box())
	if !strings.Contains(box, "Merged") || !strings.Contains(box, "Closed") {
		t.Errorf("states not shown:\n%s", box)
	}
}

// Git: My Pull Requests… is a launchpad: first what asks for my review,
// then my own; Enter opens the view like the list.
func TestPRMine(t *testing.T) {
	f := &prFakeGH{answers: map[string]string{"pr list": prListText, "pr view": prViewText14}}
	m := prModel(f)
	m.ciW().run = func(ctx context.Context, dir string, args ...string) (string, string, error) {
		out, errOut, err := f.run(ctx, dir, args...)
		if strings.Contains(strings.Join(args, " "), "--author=@me") { // mine: #14 (also a review request) and #8
			out = `[{"number":14,"title":"Add parser"},{"number":8,"title":"My fix","author":{"login":"me"}}]`
		}
		return out, errOut, err
	}
	cmd, ok := m.dispatchPRPalette("git-pr-mine")
	if !ok || cmd == nil {
		t.Fatal("no command")
	}
	prDo(t, m, m.prMineCmd())
	if !m.pickerOpen || m.pickerKind != pickerKindPRList {
		t.Fatal("picker not open")
	}
	box := ansi.Strip(m.picker.Box())
	for _, want := range []string{"My Pull Requests", "Needs your review", "Yours"} {
		if !strings.Contains(box, want) {
			t.Errorf("launchpad misses %q:\n%s", want, box)
		}
	}
	if strings.Index(box, "Needs your review") > strings.Index(box, "Yours") {
		t.Error("review requests are not first")
	}
	if strings.Count(box, "#14 ") != 1 {
		t.Errorf("#14 shows twice:\n%s", box)
	}
	if !f.called("pr list --state open") {
		t.Errorf("calls %v", f.calls)
	}
	m.pickerOpen = false
	if cmd, _ := m.handlePRPickerSelect("mine-#14"); cmd == nil {
		t.Error("a launchpad row does not open the view")
	}

	empty := prModel(&prFakeGH{answers: map[string]string{"pr list": "[]"}})
	prDo(t, empty, empty.prMineCmd())
	if empty.pickerOpen || !toastHas(empty, "Nothing waits for you") {
		t.Error("empty launchpad")
	}
}
