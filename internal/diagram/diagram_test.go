package diagram

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

func fits(t *testing.T, rows []string, width int) {
	t.Helper()
	if len(rows) == 0 {
		t.Fatal("no rows")
	}
	for i, r := range rows {
		if w := runewidth.StringWidth(r); w > width {
			t.Fatalf("row %d is %d cells, width %d: %q", i, w, width, r)
		}
	}
}

func joined(rows []string) string { return strings.Join(rows, "\n") }

// Spec (CMP-70): only the diagram languages are diagrams.
func TestIsDiagram(t *testing.T) {
	for _, l := range []string{"mermaid", "Mermaid", "mmd", "csv", "tsv", "excalidraw", "mermaid {x}"} {
		if !IsDiagram(l) {
			t.Errorf("%q should be a diagram", l)
		}
	}
	for _, l := range []string{"", "go", "json", "markdown"} {
		if IsDiagram(l) {
			t.Errorf("%q is not a diagram", l)
		}
	}
	if _, ok := Render("go", "package x", 80); ok {
		t.Fatal("go rendered as a diagram")
	}
}

// Spec (CMP-70): a CSV block is a box-drawn table; number columns sit right.
func TestCSVTable(t *testing.T) {
	rows, ok := Render("csv", "name,age\nann,3\nbob,12\n", 80)
	if !ok {
		t.Fatal("csv did not render")
	}
	want := []string{
		"┌──────┬─────┐",
		"│ name │ age │",
		"├──────┼─────┤",
		"│ ann  │   3 │",
		"│ bob  │  12 │",
		"└──────┴─────┘",
	}
	if joined(rows) != joined(want) {
		t.Fatalf("got\n%s\nwant\n%s", joined(rows), joined(want))
	}
}

// Spec (CMP-70): TSV uses tabs; a wide table shrinks its widest column.
func TestTSVAndNarrow(t *testing.T) {
	rows, ok := Render("tsv", "a\tb\n1\t"+strings.Repeat("long ", 20)+"\n", 30)
	if !ok {
		t.Fatal("tsv did not render")
	}
	fits(t, rows, 30)
	if !strings.Contains(joined(rows), "…") {
		t.Fatalf("long cell not cut:\n%s", joined(rows))
	}
	if _, ok := Render("csv", "a,b,c,d,e,f,g,h,i,j\n", 12); ok {
		t.Fatal("ten columns cannot fit 12 cells")
	}
}

// Spec (CMP-70): a top-down flowchart draws boxes in ranks joined by arrows.
func TestFlowTopDown(t *testing.T) {
	src := "flowchart TD\n  A[Start] --> B{Ready?}\n  B -->|yes| C(Ship)\n  B -->|no| D[Fix]\n  D --> B\n"
	rows, ok := Render("mermaid", src, 80)
	if !ok {
		t.Fatal("flowchart did not render")
	}
	fits(t, rows, 80)
	s := joined(rows)
	for _, w := range []string{"Start", "Ready?", "Ship", "Fix", "▼", "yes", "no", "↺"} {
		if !strings.Contains(s, w) {
			t.Fatalf("missing %q in\n%s", w, s)
		}
	}
	start, ready := -1, -1
	for i, r := range rows {
		if strings.Contains(r, "Start") && start < 0 {
			start = i
		}
		if strings.Contains(r, "Ready?") && ready < 0 {
			ready = i
		}
	}
	if !(start >= 0 && ready > start) {
		t.Fatalf("Ready? is not below Start:\n%s", s)
	}
}

// Spec (CMP-70): a left-right flowchart puts the ranks side by side.
func TestFlowLeftRight(t *testing.T) {
	rows, ok := Render("mermaid", "graph LR; A[One] -- go --> B[Two]; B --> C[Three]", 80)
	if !ok {
		t.Fatal("LR did not render")
	}
	fits(t, rows, 80)
	s := joined(rows)
	if !strings.Contains(s, "▶") || !strings.Contains(s, "go") {
		t.Fatalf("no arrow or label:\n%s", s)
	}
	for _, r := range rows {
		if strings.Contains(r, "One") {
			if !(strings.Index(r, "One") < strings.Index(r, "Two") && strings.Index(r, "Two") < strings.Index(r, "Three")) {
				t.Fatalf("not left to right: %q", r)
			}
			return
		}
	}
	t.Fatalf("no One row:\n%s", s)
}

// Spec (CMP-70): an edge across two ranks runs through the middle rank.
func TestFlowLongEdge(t *testing.T) {
	rows, ok := Render("mermaid", "graph TD\nA-->B\nB-->C\nA-->C\n", 60)
	if !ok {
		t.Fatal("did not render")
	}
	if strings.Contains(joined(rows), "↺") {
		t.Fatalf("a forward edge went to the legend:\n%s", joined(rows))
	}
}

// Spec (CMP-70): too wide or unknown diagrams fall back to the source.
func TestFlowFallbacks(t *testing.T) {
	var b strings.Builder
	b.WriteString("graph TD\n")
	for i := 0; i < 30; i++ {
		b.WriteString("R --> N" + strings.Repeat("x", 3) + string(rune('a'+i%26)) + string(rune('a'+i/26)) + "[A long node label]\n")
	}
	if _, ok := Render("mermaid", b.String(), 40); ok {
		t.Fatal("30 wide children cannot fit 40 cells")
	}
	if _, ok := Render("mermaid", "gantt\n  title x\n", 80); ok {
		t.Fatal("gantt is not drawn yet")
	}
	if _, ok := Render("mermaid", "graph TD\n A[[[ broken", 80); ok {
		t.Fatal("a broken node rendered")
	}
}

// Spec (CMP-70): a sequence diagram has heads, lifelines and messages.
func TestSequence(t *testing.T) {
	src := "sequenceDiagram\n participant A as Alice\n participant B as Bob\n A->>B: hello there\n B-->>A: hi\n Note over A: thinks\n loop every minute\n A->>A: tick\n end\n"
	rows, ok := Render("mermaid", src, 80)
	if !ok {
		t.Fatal("sequence did not render")
	}
	fits(t, rows, 80)
	s := joined(rows)
	for _, w := range []string{"Alice", "Bob", "hello there", "hi", "thinks", "loop every minute", "tick", "▶", "◀", "╌"} {
		if !strings.Contains(s, w) {
			t.Fatalf("missing %q in\n%s", w, s)
		}
	}
}

// Spec (CMP-70): an Excalidraw scene draws its boxes, text and arrows.
func TestExcalidraw(t *testing.T) {
	src := `{"type":"excalidraw","elements":[
	 {"id":"r","type":"rectangle","x":0,"y":0,"width":120,"height":60},
	 {"id":"t","type":"text","x":10,"y":20,"width":40,"height":20,"text":"Box","containerId":"r"},
	 {"id":"e","type":"ellipse","x":300,"y":0,"width":120,"height":60},
	 {"id":"a","type":"arrow","x":120,"y":30,"width":180,"height":0,"points":[[0,0],[180,0]]},
	 {"id":"gone","type":"text","x":0,"y":200,"width":40,"height":20,"text":"Deleted","isDeleted":true}
	]}`
	rows, ok := Render("excalidraw", src, 80)
	if !ok {
		t.Fatal("excalidraw did not render")
	}
	fits(t, rows, 80)
	s := joined(rows)
	for _, w := range []string{"Box", "┌", "╭", "▶"} {
		if !strings.Contains(s, w) {
			t.Fatalf("missing %q in\n%s", w, s)
		}
	}
	if strings.Contains(s, "Deleted") {
		t.Fatal("a deleted element was drawn")
	}
	if _, ok := Render("excalidraw", "{not json", 80); ok {
		t.Fatal("bad json rendered")
	}
}

// Spec (CMP-70): odd input never panics and never overflows the width.
func FuzzRender(f *testing.F) {
	f.Add("mermaid", "graph TD\nA-->B")
	f.Add("mermaid", "sequenceDiagram\nA->>B: x")
	f.Add("csv", "a,b\n1,2")
	f.Add("excalidraw", `{"elements":[{"type":"line","x":0,"y":0,"points":[[0,0],[10,40]]}]}`)
	f.Fuzz(func(t *testing.T, lang, src string) {
		for _, w := range []int{1, 10, 40, 120} {
			rows, ok := Render(lang, src, w)
			if !ok {
				continue
			}
			for _, r := range rows {
				if runewidth.StringWidth(r) > w {
					t.Fatalf("row wider than %d: %q", w, r)
				}
			}
		}
	})
}
