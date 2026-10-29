package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Spec: Given a.txt and b.txt open with b.txt shown,
// when the user runs Layout: Save As… "pair" for this project,
// then closes every tab and runs Layout: Open… and picks "pair",
// then both tabs come back with b.txt active, the status bar names the
// layout, and the file sits in .termocode/layouts.
func TestLayoutSaveThenOpenBringsTheTabsBack(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	open(d, "b.txt")
	if !waitTabs(d, "a.txt", "b.txt") {
		got, _ := tabs(d)
		t.Fatalf("tabs = %v", got)
	}

	d.Palette("layout-save")
	if !d.WaitForText("Name for this layout", 3*time.Second) {
		t.Fatalf("no name prompt:\n%s", screen(d))
	}
	d.Type("pair")
	d.Key("enter")
	if !d.WaitForText("This project", 3*time.Second) {
		t.Fatalf("no scope card:\n%s", screen(d))
	}
	d.Key("right", "enter") // Only me, This project
	file := filepath.Join(d.Dir, ".termocode", "layouts", "pair.jsonc")
	if !d.WaitFor(func() bool { _, err := os.Stat(file); return err == nil }, 3*time.Second) {
		t.Fatalf("no layout file:\n%s", screen(d))
	}
	body, _ := os.ReadFile(file)
	for _, want := range []string{`"a.txt"`, `"b.txt"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("file lacks %s:\n%s", want, body)
		}
	}
	if strings.Contains(string(body), d.Dir) {
		t.Errorf("file holds an absolute path:\n%s", body)
	}

	d.Key("ctrl+w", "ctrl+w")
	if !d.WaitFor(func() bool { got, _ := tabs(d); return len(got) == 0 }, 3*time.Second) {
		got, _ := tabs(d)
		t.Fatalf("tabs left: %v", got)
	}
	d.Palette("layout-open")
	if !d.WaitForText("pair", 3*time.Second) {
		t.Fatalf("pair not listed:\n%s", screen(d))
	}
	d.Type("pair")
	d.Key("enter")
	if !waitTabs(d, "a.txt", "b.txt") {
		got, _ := tabs(d)
		t.Fatalf("after Open, tabs = %v\n%s", got, screen(d))
	}
	if _, active := tabs(d); active != "b.txt" {
		t.Errorf("active tab %q, want b.txt", active)
	}
	if !d.WaitForText("Layout: pair", 3*time.Second) {
		t.Errorf("status bar does not name the layout:\n%s", screen(d))
	}
}
