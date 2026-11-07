package kube_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/kube"
)

func TestParseEventsNewestFirst(t *testing.T) {
	_, c := fakeClient(t)
	evs, err := c.Events(context.Background(), "", "")
	if err != nil || len(evs) != 3 {
		t.Fatalf("events = %+v, %v", evs, err)
	}
	if evs[0].Reason != "BackOff" || evs[0].Type != "Warning" || evs[0].Count != 7 || evs[0].Object != "pod/api-7d9f8c6b5-x2x4q" {
		t.Errorf("newest = %+v", evs[0])
	}
	// eventTime and series.count when lastTimestamp and count are missing.
	if evs[1].Reason != "ScalingReplicaSet" || evs[1].Count != 2 || evs[1].Object != "deployment/worker" ||
		!evs[1].Last.Equal(time.Date(2026, 10, 30, 10, 5, 0, 0, time.UTC)) {
		t.Errorf("second = %+v", evs[1])
	}
	if evs[2].Reason != "Scheduled" || evs[2].Count != 1 {
		t.Errorf("oldest = %+v", evs[2])
	}
}

func TestParseEventsBadJSON(t *testing.T) {
	if _, err := kube.ParseEvents([]byte("not json")); err == nil {
		t.Error("bad json: no error")
	}
}

func TestEventsOfOneObjectUseAFieldSelector(t *testing.T) {
	f, c := fakeClient(t)
	if _, err := c.Events(context.Background(), "pod", "api-7d9f8c6b5-x2x4q"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Events(context.Background(), "", ""); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"get events -o json --field-selector involvedObject.kind=Pod,involvedObject.name=api-7d9f8c6b5-x2x4q --context dev --namespace shop",
		"get events -o json --context dev --namespace shop",
	}
	got := f.Calls()
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("calls =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestEventsFailIsOneLine(t *testing.T) {
	f, c := fakeClient(t)
	f.Set(t, "events.err", `Error from server (Forbidden): events is forbidden: User "x" cannot list resource "events"`)
	if _, err := c.Events(context.Background(), "", ""); err == nil || strings.Contains(err.Error(), "\n") {
		t.Errorf("err = %v", err)
	}
}

func TestEventsText(t *testing.T) {
	now := time.Date(2026, 10, 30, 10, 10, 0, 0, time.UTC)
	evs := []kube.Event{
		{Type: "Warning", Reason: "BackOff", Object: "pod/api", Message: "Back-off restarting\nfailed container", Count: 7, Last: now.Add(-time.Minute)},
		{Type: "Normal", Reason: "Scheduled", Object: "pod/api", Message: "Assigned", Count: 1, Last: now.Add(-10 * time.Minute)},
	}
	got := kube.EventsText(evs, now)
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "LAST") {
		t.Fatalf("text =\n%s", got)
	}
	for _, want := range []string{"1m", "Warning", "BackOff", "pod/api", "x7", "Back-off restarting failed container"} {
		if !strings.Contains(lines[1], want) {
			t.Errorf("line %q lacks %q", lines[1], want)
		}
	}
	if strings.Contains(lines[2], "x1") {
		t.Errorf("count 1 is shown: %q", lines[2])
	}
	if got := kube.EventsText(nil, now); got != "No events.\n" {
		t.Errorf("empty = %q", got)
	}
}
