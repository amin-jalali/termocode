package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
)

// ctrWatch wires a fake event channel as the host watcher of st.
func ctrWatch(st *servicesState) chan containers.Event {
	ch := make(chan containers.Event, 8)
	ctx, cancel := context.WithCancel(context.Background())
	st.hostWatchCh, st.hostWatchCtx, st.hostWatchGen = ch, ctx, 7
	st.hostWatchCancel, st.hostWatchStop = cancel, func() {}
	return ch
}

// Engine events reload the sections they touch (images through
// REGISTRIES when IMAGES is hidden), drop the storage users cache, and
// a closed stream restarts the watcher.
func TestCovContainerEventsReloadSections(t *testing.T) {
	m, _ := ctrModel(t)
	st := m.services
	ch := ctrWatch(st)
	st.storageInspectState["item/network/x"] = "done"
	st.storageInspectState["item/volume/y"] = "done"
	if m.onContainerEvent(containerEventMsg{gen: 1}) != nil {
		t.Fatal("stale event handled")
	}
	ch <- containers.Event{Type: ""}
	ch <- containers.Event{Type: "image"}
	ch <- containers.Event{Type: "network"}
	ch <- containers.Event{Type: "volume"}
	msg := waitContainerEvent(st.hostWatchCtx, ch, 7)().(containerEventMsg)
	if len(msg.events) != 4 {
		t.Fatalf("burst %+v", msg.events)
	}
	st.sections["project"] = true
	st.sections["images"] = false
	st.registryRows = []containers.Registry{{Host: "ghcr.io"}}
	st.collapsed[registryFoldKey("ghcr.io")] = false
	cmd := m.onContainerEvent(msg)
	if cmd == nil {
		t.Fatal("no reload")
	}
	if _, ok := st.storageInspectState["item/network/x"]; ok {
		t.Fatal("network users not dropped")
	}
	if _, ok := st.storageInspectState["item/volume/y"]; ok {
		t.Fatal("volume users not dropped")
	}
	// The channel closes: the watcher restarts.
	close(ch)
	closed := waitContainerEvent(st.hostWatchCtx, ch, 7)().(containerEventMsg)
	if !closed.closed {
		t.Fatal("not closed")
	}
	t.Cleanup(func() { st.stopHostWatch(); st.stopWatch() })
	_ = m.onContainerEvent(closed)
	if st.hostWatchCh == ch {
		t.Fatal("old watcher kept")
	}
	// A cancelled context ends the wait.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := waitContainerEvent(ctx, make(chan containers.Event), 1)().(containerEventMsg); !got.closed {
		t.Fatal("cancelled wait")
	}
	// A burst cut short by a close returns what it has.
	ch2 := make(chan containers.Event, 2)
	ch2 <- containers.Event{Type: "container"}
	close(ch2)
	if got := waitContainerEvent(context.Background(), ch2, 1)().(containerEventMsg); len(got.events) != 1 || got.closed {
		t.Fatalf("%+v", got)
	}
}

// The engine and context helpers: podman auth file and connection, the
// hidden view's work, and the section choice when nothing is shown.
func TestCovContainerStateHelpers(t *testing.T) {
	t.Setenv("REGISTRY_AUTH_FILE", "/x/auth.json")
	if registryConfigPath("podman") != "/x/auth.json" {
		t.Fatal(registryConfigPath("podman"))
	}
	t.Setenv("REGISTRY_AUTH_FILE", "")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1")
	if registryConfigPath("podman") != filepath.Join("/run/user/1", "containers", "auth.json") {
		t.Fatal(registryConfigPath("podman"))
	}
	st := svcHostState()
	st.containerEngine.Kind = "podman"
	t.Setenv("CONTAINER_CONNECTION", "remote1")
	if st.activeContext() != "remote1" {
		t.Fatal(st.activeContext())
	}
	st.containerEngine.Kind = "other"
	st.contextRows = nil
	if st.activeContext() != "" {
		t.Fatal(st.activeContext())
	}
	// Hiding the view cancels its calls and forgets the storage checks.
	called := 0
	st.containerCancel = func() { called++ }
	st.containerCancels["item/volume/v"] = func() { called++ }
	st.storageInspectState["item/volume/v"] = "loading"
	st.cancelHostWork()
	if called != 2 || st.containerCancel != nil || st.storageInspectState["item/volume/v"] != "" {
		t.Fatalf("called %d", called)
	}
	for _, s := range containerSectionOrder {
		st.sections[s] = false
	}
	if got := st.sectionChoice(); len(got) != 1 || got[0] != "none" {
		t.Fatal(got)
	}
	st.applyFoldKeys([]string{"compose", "!local"})
	if !st.collapsed[svcSecCompose] || st.collapsed[svcSecLocal] {
		t.Fatal("project folds")
	}
	keys := st.foldKeys()
	found := false
	for _, k := range keys {
		found = found || k == svcSecCompose
	}
	if !found {
		t.Fatalf("fold keys %v", keys)
	}
}

// R with an engine that stopped looks for it again; an answer for a
// hidden view or an old generation is dropped; a detect with section
// errors keeps them.
func TestCovContainerStateReloadAndDetect(t *testing.T) {
	m, _ := ctrModel(t)
	st := m.services
	t.Cleanup(func() { st.stopHostWatch(); st.stopWatch() })
	st.containerEngine.Reachable = false
	st.containerPreference = preferredContainerEngine()
	_ = m.containersReloadAll()
	if st.containerDetected {
		t.Fatal("still detected")
	}
	if m.onContainerHost(containerHostMsg{gen: st.containerGen + 1, section: "detect"}) != nil {
		t.Fatal("old answer used")
	}
	_ = m.onContainerHost(containerHostMsg{gen: st.containerGen, section: "detect", engine: containers.Engine{Binary: "docker", Kind: "docker"},
		loaded: []string{"networks"}, errs: map[string]string{"networks": "network ls failed"}})
	if st.containerErrors["networks"] != "network ls failed" {
		t.Fatalf("errors %v", st.containerErrors)
	}
	if m.containersList(allContextsSection) != nil {
		t.Fatal("ALL CONTEXTS off: listed")
	}
	st.containerEngine.Kind = "other"
	if m.containersList("contexts") != nil {
		t.Fatal("contexts for an engine without them")
	}
	m.services = nil
	if m.containersReloadAll() != nil {
		t.Fatal("nil services")
	}
}
