package app

import (
	"github.com/amin-jalali/termocode/internal/kube"
	"strings"
	"testing"
)

func TestKubeEventsSectionIsFoldedThenLiveWhenOpen(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	if !m.kube.collapsed["events"] {
		t.Fatal("EVENTS starts open")
	}
	if f.Count("get events") != 0 {
		t.Fatal("a folded EVENTS section loaded")
	}
	kubeSectionTo(t, m, "events")
	kubeRunLoad(t, m, kubeKey(t, m, " "))
	if f.Count("get events -o json --context dev --namespace shop") != 1 {
		t.Errorf("no scoped get events in %v", f.Calls())
	}
	evs := m.kube.extra["events"]
	if len(evs) != 3 {
		t.Fatalf("events = %+v", evs)
	}
	if evs[0].Name == "" || !strings.Contains(evs[0].Name, "/") {
		t.Errorf("row name %q is not kind/name", evs[0].Name)
	}
}

func TestKubeEventRowKeysActOnTheInvolvedObject(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	kubeSectionTo(t, m, "events")
	kubeRunLoad(t, m, kubeKey(t, m, " "))
	kubeCursorTo(t, m, "events", "deployment/worker")
	if got := m.kube.footer(m.kube.loadedAt)[0]; got == "" {
		t.Error("no event message in the footer")
	}
	cmd := kubeKey(t, m, "E")
	if cmd == nil {
		t.Fatal("E did nothing")
	}
	msg := cmd().(kubeEventsMsg)
	if msg.title != "events: deployment/worker" {
		t.Errorf("title = %q", msg.title)
	}
	if f.Count("involvedObject.kind=Deployment,involvedObject.name=worker") != 1 {
		t.Errorf("no object events call in %v", f.Calls())
	}
	if kubeKey(t, m, "d"); m.confirmOpen {
		t.Error("d on an event opened a delete card")
	}
}

func TestKubeEventKindsOfTheNewSections(t *testing.T) {
	for kind, want := range map[string]string{"statefulset": "StatefulSet", "daemonset": "DaemonSet", "persistentvolumeclaim": "PersistentVolumeClaim", "cronjob": "CronJob"} {
		if got := kube.EventKind(kind); got != want {
			t.Errorf("%s → %q, want %q", kind, got, want)
		}
	}
}
