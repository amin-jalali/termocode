package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/containers"
)

func TestContainerLogsPickerChoices(t *testing.T) {
	cases := map[string]containers.LogsOptions{
		"follow:200":  {Tail: 200},
		"follow:1000": {Tail: 1000},
		"follow:all":  {Tail: -1},
		"since:10m":   {Since: "10m", Tail: -1},
		"since:1h":    {Since: "1h", Tail: -1},
	}
	for id, want := range cases {
		got, ok := containerLogsChoice(id, true)
		want.Timestamps = true
		if !ok || got != want {
			t.Fatalf("%s = %+v %v, want %+v", id, got, ok, want)
		}
	}
	if _, ok := containerLogsChoice("save", false); ok {
		t.Fatal("save is not a follow choice")
	}
	items := containerLogsItems(false)
	var hasTS, hasSave, hasSearch bool
	for _, it := range items {
		hasTS = hasTS || (it.ID == "ts" && strings.Contains(it.Title, "off"))
		hasSave = hasSave || it.ID == "save"
		hasSearch = hasSearch || it.ID == "search"
	}
	if !hasTS || !hasSave || !hasSearch {
		t.Fatalf("items miss timestamps, save or search: %+v", items)
	}
}

func TestContainerLogsSearchAndFileName(t *testing.T) {
	text := "a ok\nb ERROR disk\nc fine\nd error net"
	got := filterLogLines(text, "error")
	if len(got) != 2 || !strings.Contains(got[0], "disk") || !strings.Contains(got[1], "net") {
		t.Fatalf("matches = %q", got)
	}
	name := containerLogsFileName("web/1", time.Date(2026, 11, 7, 10, 4, 5, 0, time.UTC))
	if name != "web-1-logs-20261107-100405.log" {
		t.Fatalf("file name = %q", name)
	}
}

func TestContainerLogsOptionsCommandIsWired(t *testing.T) {
	found := false
	for _, it := range servicesPaletteItems() {
		found = found || it.ID == "containers-logs-options"
	}
	if !found {
		t.Fatal("containers-logs-options is not in the palette")
	}
	if p := aiActionPolicies["containers-logs-options"]; p.Gate != aiGateNever {
		t.Fatalf("policy = %+v, want never (a picker)", p)
	}
}
