package activity

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/theme"
)

// A bar too short for the bottom group shows only the icons that fit and
// clicks below them hit nothing.
func TestShortBarKeepsOnlyWhatFits(t *testing.T) {
	m := New()
	m.SetHeight(topStart + itemPitch + 1) // room for one icon only
	items := m.Items()
	if len(items) != 1 {
		t.Fatalf("items on a short bar: %+v", items)
	}
	if m.bottomGroupStart() != -1 {
		t.Fatal("a short bar has no separate bottom group")
	}
	if _, ok := m.itemAt(topStart + topGroupRows()); ok {
		t.Error("a click where the bottom group would be hits an icon")
	}
	if k, it := m.classify(topStart + topGroupRows()); k != rowEmpty || it != nil {
		t.Errorf("row %v %v", k, it)
	}
	if _, ok := m.ItemAt(-1); ok {
		t.Error("a click above the bar")
	}
	if _, ok := m.ItemAt(m.h); ok {
		t.Error("a click below the bar")
	}
	if it, ok := m.ItemAt(items[0].Y); !ok || it.View != items[0].View {
		t.Errorf("click on the first icon: %+v %v", it, ok)
	}
}

// A tall bar: the gap rows between icons and rows past the last icon of a
// group are not clickable.
func TestTallBarGapsAreNotClickable(t *testing.T) {
	m := New()
	m.SetHeight(80)
	bs := m.bottomGroupStart()
	if bs < 0 {
		t.Fatal("a tall bar has a bottom group")
	}
	if _, ok := m.itemAt(bs - 1); ok {
		t.Error("the divider row is not an icon")
	}
	if _, ok := m.itemAt(bs + bottomGroupRows()); ok {
		t.Error("a row after the bottom group is not an icon")
	}
	if len(bottomItems) > 1 {
		if _, ok := m.itemAt(bs + itemPitch); itemStride() > itemPitch && ok {
			t.Error("the gap between two bottom icons is not an icon")
		}
	}
	if k, _ := classifyInGroup(itemStride()*100, topAll()); k != rowEmpty {
		t.Error("a row past the group is empty")
	}
	if idx, isItem := strideIndex(itemStride() + 0); !isItem || idx != 1 {
		t.Errorf("stride %d %v", idx, isItem)
	}
}

func TestSetWorkflowsShownTwiceChangesNothing(t *testing.T) {
	was := WorkflowsShown()
	defer SetWorkflowsShown(was)
	SetWorkflowsShown(!was)
	SetWorkflowsShown(!was) // the same value again
	if WorkflowsShown() == was {
		t.Error("the setting did not change")
	}
}

// An icon whose bold artwork is not two cells wide is padded or cut to
// two cells, so the bar's columns never shift.
func TestBoldRowKeepsTwoCellSlot(t *testing.T) {
	old := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeUnicode
	defer func() { theme.CurrentIconMode = old }()
	st := newStyleSet()
	want := lipgloss.Width(renderActivityBoldRow(theme.Icon{Bold: [2]string{"ab", "cd"}}, 0, false, st))
	for name, ic := range map[string]theme.Icon{
		"narrow": {Bold: [2]string{"a", "b"}},
		"wide":   {Bold: [2]string{"abcd", "efgh"}},
		"cjk":    {Bold: [2]string{"日本語", "日本語"}},
	} {
		for _, row := range []int{0, 1} {
			if got := lipgloss.Width(renderActivityBoldRow(ic, row, row == 0, st)); got != want || got != Width {
				t.Errorf("%s row %d is %d cells wide, want %d", name, row, got, Width)
			}
		}
	}
}
