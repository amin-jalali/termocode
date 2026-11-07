//go:build browser && webdev

package web

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/chromedp/kb"
)

// The Customize sheet of a phone view (MOBUX-16), on the component kit's
// "Pull requests" block at 390 px with touch: ⚙ opens the sheet; fields,
// filter, sort, group, rows, layout and density change a draft; the live
// preview shows the laptop's block for the draft before Apply; Cancel
// (first), Esc and the dim area change nothing; Apply hands the spec
// back; a refused run shows an error line with Try again.

const (
	custSheet   = `document.querySelector(".cust")`
	custPreview = `document.querySelector(".cust-preview")`
	custBlock   = `document.querySelector(".kit-custblock")`
)

type custSpec struct {
	View   string   `json:"view"`
	Fields []string `json:"fields"`
	Filter []struct {
		Field, Op, Value string
		In               []string
	} `json:"filter"`
	Sort []struct {
		Field string
		Desc  bool
	} `json:"sort"`
	Group   string `json:"group"`
	Limit   int    `json:"limit"`
	Layout  string `json:"layout"`
	Density string `json:"density"`
}

// blockSpec is the spec the kit's view block now runs.
func blockSpec(p *Page) custSpec {
	p.T.Helper()
	var s custSpec
	if err := json.Unmarshal([]byte(Eval[string](p, custBlock+`.dataset.spec`)), &s); err != nil {
		p.T.Fatal(err)
	}
	return s
}

func openCustomizeKit(t *testing.T) *Page {
	t.Helper()
	p := openKit(t, Phone)
	p.WaitJS(custBlock)
	p.TapJS(custBlock + `.querySelector(".cust-gear")`)
	p.WaitJS(custSheet)
	return p
}

// previewCols are the preview table's column labels.
func previewCols(p *Page) []string {
	p.T.Helper()
	return Eval[[]string](p, `[...`+custPreview+`.querySelectorAll(".vp-table th")].map((th) => th.textContent.trim())`)
}

// previewSettled waits until the preview is no longer busy.
func previewSettled(p *Page) {
	p.T.Helper()
	p.WaitJS(custPreview + ` && !` + custPreview + `.classList.contains("is-busy")`)
}

// custSelect picks value in a select of the sheet and fires change.
func custSelect(p *Page, el, value string) {
	p.T.Helper()
	p.WaitJS(el)
	Eval[bool](p, fmt.Sprintf(`(() => { const s = %s; s.value = %q; s.dispatchEvent(new Event("change", { bubbles: true })); return true; })()`, el, value))
}

// custType types text into an input of the sheet and fires input.
func custType(p *Page, el, text string) {
	p.T.Helper()
	p.WaitJS(el)
	Eval[bool](p, fmt.Sprintf(`(() => { const i = %s; i.value = %q; i.dispatchEvent(new Event("input", { bubbles: true })); return true; })()`, el, text))
}

// custButton is the sheet's first button whose text is exactly text.
func custButton(text string) string {
	return fmt.Sprintf(`[...%s.querySelectorAll("button")].find((b) => b.textContent.trim() === %q)`, custSheet, text)
}

func fieldBox(id string) string {
	return fmt.Sprintf(`%s.querySelector('[data-field=%q] input')`, custSheet, id)
}

// The sheet opens from ⚙ with its title, the preview of the current
// block, the current fields shown in order and Cancel before Apply.
func TestCustomizeOpensWithTheCurrentView(t *testing.T) {
	p := openCustomizeKit(t)
	if got := Eval[string](p, custSheet+`.querySelector(".cust-title").textContent`); got != "Pull requests · customize" {
		t.Errorf("title %q", got)
	}
	if got := strings.Join(previewCols(p), " "); got != "# Title CI Age" {
		t.Errorf("preview columns %q, want # Title CI Age", got)
	}
	shown := Eval[[]string](p, `[...`+custSheet+`.querySelectorAll(".cust-field.is-on")].map((li) => li.dataset.field)`)
	if strings.Join(shown, " ") != "number title checks age" {
		t.Errorf("shown fields %v", shown)
	}
	foot := Eval[[]string](p, `[...`+custSheet+`.querySelectorAll(".cust-foot button")].map((b) => b.textContent.trim())`)
	if strings.Join(foot, " ") != "Cancel Apply" {
		t.Errorf("foot buttons %v, want Cancel first, then Apply", foot)
	}
}

// Hiding a field and moving one change the preview before Apply; the
// block itself changes only on Apply.
func TestCustomizeFieldsPreviewLive(t *testing.T) {
	p := openCustomizeKit(t)
	p.TapJS(fieldBox("age"))
	p.WaitJS(`!` + custPreview + `.querySelector(".vp-table th:last-child")?.textContent.includes("Age") && !` + custPreview + `.classList.contains("is-busy")`)
	p.TapJS(fieldBox("author"))
	p.TapJS(custSheet + `.querySelector('[data-field="author"] .cust-move[aria-label="Move Author up"]')`)
	previewSettled(p)
	p.WaitJS(`[...` + custPreview + `.querySelectorAll(".vp-table th")].map((th) => th.textContent.trim()).join(" ") === "# Title Author CI"`)
	if got := blockSpec(p).Fields; strings.Join(got, " ") != "number title checks age" {
		t.Errorf("block changed before Apply: %v", got)
	}
	p.TapJS(custButton("Apply"))
	p.WaitJS(`!` + custSheet)
	if got := blockSpec(p).Fields; strings.Join(got, " ") != "number title author checks" {
		t.Errorf("applied fields %v, want number title author checks", got)
	}
}

// The last shown field cannot be hidden.
func TestCustomizeKeepsOneField(t *testing.T) {
	p := openCustomizeKit(t)
	for _, id := range []string{"number", "title", "checks"} {
		p.TapJS(fieldBox(id))
		p.WaitJS(`!` + fieldBox(id) + `.checked`)
	}
	if !Eval[bool](p, fieldBox("age")+`.disabled && `+fieldBox("age")+`.checked`) {
		t.Error("the last shown field can be hidden")
	}
}

// A filter, a sort and a group show in the preview and go out on Apply.
func TestCustomizeFilterSortGroup(t *testing.T) {
	p := openCustomizeKit(t)
	p.TapJS(custButton("+ Add a filter"))
	cond := custSheet + `.querySelector('[data-cond="0"]')`
	custSelect(p, cond+`.querySelector('select[aria-label="Filter 1: field"]')`, "checks")
	ops := Eval[[]string](p, `[...`+cond+`.querySelectorAll('select[aria-label="Filter 1: condition"] option')].map((o) => o.value)`)
	if strings.Contains(strings.Join(ops, " "), "gt") {
		t.Errorf("a state field offers %v; gt is for numbers and times", ops)
	}
	custType(p, cond+`.querySelector("input")`, "fail")
	previewSettled(p)
	p.WaitJS(custPreview + `.querySelector(".vp-count").textContent.trim() === "2 of 2 rows"`)

	custSelect(p, `[...`+custSheet+`.querySelectorAll(".cust-pair")].find((l) => l.textContent.startsWith("Sort by")).querySelector("select")`, "age")
	p.TapJS(custButton("↓ High to low"))
	previewSettled(p)
	p.WaitJS(custPreview + `.querySelector(".vp-table tbody td").textContent === "#401"`)

	custSelect(p, `[...`+custSheet+`.querySelectorAll(".cust-pair")].find((l) => l.textContent.startsWith("Group by")).querySelector("select")`, "author")
	previewSettled(p)
	p.WaitJS(custPreview + `.querySelectorAll(".vp-group").length === 2`)

	p.TapJS(custButton("Apply"))
	p.WaitJS(`!` + custSheet)
	s := blockSpec(p)
	if len(s.Filter) != 1 || s.Filter[0].Field != "checks" || s.Filter[0].Op != "eq" || s.Filter[0].Value != "fail" {
		t.Errorf("filter %+v", s.Filter)
	}
	if len(s.Sort) != 1 || s.Sort[0].Field != "age" || !s.Sort[0].Desc {
		t.Errorf("sort %+v", s.Sort)
	}
	if s.Group != "author" {
		t.Errorf("group %q", s.Group)
	}
}

// A filter still missing its value is not sent; "is one of" takes a
// comma list; a removed filter is gone.
func TestCustomizeFilterValues(t *testing.T) {
	p := openCustomizeKit(t)
	p.TapJS(custButton("+ Add a filter"))
	p.TapJS(custButton("+ Add a filter"))
	c0 := custSheet + `.querySelector('[data-cond="0"]')`
	custSelect(p, c0+`.querySelector('select[aria-label="Filter 1: field"]')`, "author")
	custSelect(p, c0+`.querySelector('select[aria-label="Filter 1: condition"]')`, "in")
	custType(p, c0+`.querySelector("input")`, "amin, li")
	previewSettled(p)
	p.WaitJS(custPreview + `.querySelector(".vp-count").textContent.trim() === "3 of 3 rows"`)
	p.TapJS(custSheet + `.querySelector('[aria-label="Remove filter 2"]')`)
	p.WaitJS(`!` + custSheet + `.querySelector('[data-cond="1"]')`)
	p.TapJS(custButton("Apply"))
	p.WaitJS(`!` + custSheet)
	s := blockSpec(p)
	if len(s.Filter) != 1 || s.Filter[0].Op != "in" || strings.Join(s.Filter[0].In, ",") != "amin,li" {
		t.Errorf("filter %+v, want author in [amin li]", s.Filter)
	}
}

// Layout and density are one tap each; the preview follows.
func TestCustomizeLayoutAndDensity(t *testing.T) {
	p := openCustomizeKit(t)
	p.TapJS(custButton("List"))
	previewSettled(p)
	p.WaitJS(custPreview + `.querySelector(".vp").dataset.kind === "list"`)
	p.TapJS(custButton("Detailed"))
	p.WaitJS(custPreview + `.querySelector(".vp").classList.contains("is-detailed")`)
	p.TapJS(custButton("Apply"))
	p.WaitJS(`!` + custSheet)
	if s := blockSpec(p); s.Layout != "list" || s.Density != "detailed" {
		t.Errorf("layout %q density %q, want list detailed", s.Layout, s.Density)
	}
}

// Cancel, Esc and the dim area close the sheet and change nothing.
func TestCustomizeCancelChangesNothing(t *testing.T) {
	p := openCustomizeKit(t)
	before := Eval[string](p, custBlock+`.dataset.spec`)
	p.TapJS(fieldBox("age"))
	p.TapJS(custButton("Cancel"))
	p.WaitJS(`!` + custSheet)
	p.TapJS(custBlock + `.querySelector(".cust-gear")`)
	p.WaitJS(custSheet)
	p.TapJS(fieldBox("title"))
	p.Key(kb.Escape)
	p.WaitJS(`!` + custSheet)
	if got := Eval[string](p, custBlock+`.dataset.spec`); got != before {
		t.Errorf("spec changed without Apply: %s → %s", before, got)
	}
}

// A run the laptop refuses shows the reason with ✗ and Try again; the
// last good preview stays below it.
func TestCustomizeRefusedRunShowsRetry(t *testing.T) {
	p := openKit(t, Phone)
	p.TapJS(ButtonByText("Laptop refuses"))
	p.TapJS(custBlock + `.querySelector(".cust-gear")`)
	p.WaitJS(custSheet)
	p.TapJS(fieldBox("author"))
	p.WaitJS(custPreview + `.querySelector(".cust-failed")`)
	got := Eval[string](p, custPreview+`.querySelector(".cust-failed").textContent`)
	if !strings.Contains(got, "✗") || !strings.Contains(got, "The laptop did not run this view") || !strings.Contains(got, "Try again") {
		t.Errorf("error line %q", got)
	}
	if !Eval[bool](p, `!!`+custPreview+`.querySelector(".vp-table")`) {
		t.Error("the last good preview is gone")
	}
}

// Every target of the sheet is at least 44 px tall.
func TestCustomizeTargetsAre44(t *testing.T) {
	p := openCustomizeKit(t)
	p.TapJS(custButton("+ Add a filter"))
	small := Eval[[]string](p, `[...`+custSheet+`.querySelectorAll("button, select, input:not([type=checkbox]), label.cust-check")].filter((el) => el.getBoundingClientRect().height < 44).map((el) => el.className + " " + el.textContent.trim())`)
	if len(small) != 0 {
		t.Errorf("targets under 44 px: %v", small)
	}
}
