package app_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// DBX-37: Options… on the import card (lines to skip, column formats) and
// several files in one import.

func TestDBImportOptionsSkipLinesAndDateFormat(t *testing.T) {
	s := newEditSession(0)
	d := importDriver(t, s, "", map[string]string{"data.csv": "exported by a tool\nid,email,plan\n1,a@x.io,07/11/2026\n"})
	card := importCard(t, d)
	if !strings.Contains(card, "Options…") {
		t.Fatalf("no Options button:\n%s", card)
	}
	// Nothing maps yet (the first line is not the header), so there is no
	// Import button: Options… is the fourth button.
	d.Key("right", "right", "right", "enter") // Options…
	if !d.WaitForText("Lines to skip at the start", 2*time.Second) {
		t.Fatalf("no options picker:\n%s", frameText(d))
	}
	d.Key("enter")
	if !d.WaitForText("Lines to skip before the header", 2*time.Second) {
		t.Fatalf("no skip prompt:\n%s", frameText(d))
	}
	d.Key("backspace")
	d.Type("1")
	d.Key("enter")
	if !d.WaitForText("skip the first 1 line", 5*time.Second) {
		t.Fatalf("card does not show the option:\n%s", frameText(d))
	}
	if f := frameText(d); !strings.Contains(f, "Import 1 row into") || !strings.Contains(f, "header row") {
		t.Fatalf("the file was not read again with the option:\n%s", f)
	}
	// Column formats: plan gets the date format DD/MM/YYYY.
	d.Key("right", "right", "right", "right", "enter") // Options…
	d.WaitForText("Column formats", 2*time.Second)
	d.Key("down", "down", "down", "enter")
	if !d.WaitForText("Column formats (Enter changes one", 2*time.Second) {
		t.Fatalf("no column picker:\n%s", frameText(d))
	}
	d.Key("down", "down", "enter") // plan
	if !d.WaitForText("Format of plan", 2*time.Second) {
		t.Fatalf("no format picker:\n%s", frameText(d))
	}
	d.Key("down", "down", "enter") // Date format…
	if !d.WaitForText("Date format with YYYY", 2*time.Second) {
		t.Fatalf("no date prompt:\n%s", frameText(d))
	}
	d.Type("DD/MM/YYYY")
	d.Key("enter")
	if !d.WaitForText("Format of plan", 2*time.Second) {
		t.Fatalf("not back in the format picker:\n%s", frameText(d))
	}
	d.Key("esc", "esc", "esc") // format → columns → options → card
	if !d.WaitForText("[date DD/MM/YYYY]", 2*time.Second) {
		t.Fatalf("card does not show the format:\n%s", frameText(d))
	}
	d.Key("enter") // Import
	if !d.WaitForText("Imported 1 row", 5*time.Second) {
		t.Fatalf("no done toast:\n%s", frameText(d))
	}
	args := s.insertArgs()
	if len(args) != 1 || fmt.Sprint(args[0]) != "[1 a@x.io 2026-11-07]" {
		t.Fatalf("insert args %v", args)
	}
}

func TestDBImportSeveralFilesInOneTransaction(t *testing.T) {
	s := newEditSession(0)
	d := importDriver(t, s, "", map[string]string{
		"data.csv":  "id,email,plan\n1,a,x\n2,b,y\n",
		"data2.csv": "id,email,plan\n3,c,z\n4,d,w\n",
	})
	d.Key("i")
	if !d.WaitForText("Several files", 3*time.Second) {
		t.Fatalf("no several-files item:\n%s", frameText(d))
	}
	d.Type("Several")
	d.Key("enter")
	if !d.WaitForText("a pattern like data/*.csv", 2*time.Second) {
		t.Fatalf("no pattern prompt:\n%s", frameText(d))
	}
	d.Type("data*.csv")
	d.Key("enter")
	if !d.WaitForText("Import 4 rows from 2 files", 5*time.Second) {
		t.Fatalf("no card for two files:\n%s", frameText(d))
	}
	d.Key("enter") // Import
	if !d.WaitForText("Imported 4 rows", 5*time.Second) {
		t.Fatalf("no done toast:\n%s", frameText(d))
	}
	begins := 0
	for _, e := range s.log() {
		if e == "BEGIN" {
			begins++
		}
	}
	if begins != 1 || !s.has("COMMIT") {
		t.Fatalf("not one transaction: %q", s.log())
	}
}
