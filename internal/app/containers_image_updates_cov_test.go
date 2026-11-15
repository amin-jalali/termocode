package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
)

// Check Running Images for Updates: each running tag's local repo digest
// is compared with the loopback registry's; a different one is an
// update, a missing one or a bad ref is skipped; the toast says which.
func TestCovContainerImageUpdates(t *testing.T) {
	m, _ := ctrModel(t)
	reg := newCtrRegistry(t, 0)
	x := ctrScripted(t, m)
	same := "sha256:" + strings.Repeat("a", 64)
	other := "sha256:" + strings.Repeat("b", 64)
	api, web, local := reg.host+"/acme/api:1.0", reg.host+"/acme/web", reg.host+"/acme/local:3"
	x.on("inspect "+api, fmt.Sprintf(`[{"RepoDigests":["%s/acme/api@%s"]}]`, reg.host, same))
	x.on("inspect "+web, fmt.Sprintf(`[{"RepoDigests":["%s/acme/web@%s"]}]`, reg.host, other))
	x.on("inspect "+local, `[{"RepoDigests":[]}]`)
	m.services.containerRows = []containers.Container{
		{ID: "a", Image: api, State: "running"}, {ID: "b", Image: web, State: "running"}, {ID: "c", Image: local, State: "running"},
		{ID: "d", Image: "Bad Repo", State: "running"}, {ID: "e", Image: "x@" + same, State: "running"}, {ID: "f", Image: api, State: "running"},
		{ID: "g", Image: "stopped:1", State: "exited"},
	}
	cmd := m.checkImageUpdates()
	if !strings.Contains(ctrToast(m), "Checking 3 images") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	msg := ctrFind[imageUpdatesMsg](t, ctrMsgs(cmd))
	if !msg.updates[web] || msg.updates[api] || msg.skipped != 2 {
		t.Fatalf("%+v", msg)
	}
	_ = m.onImageUpdates(msg)
	if !strings.Contains(ctrToast(m), "Update available: "+web) || !strings.Contains(ctrToast(m), "2 not checked") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.onImageUpdates(imageUpdatesMsg{updates: map[string]bool{api: false}})
	if !strings.Contains(ctrToast(m), "Running images are up to date") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	// A registry error is skipped too.
	x.fail("inspect "+api, "gone")
	if got := ctrFind[imageUpdatesMsg](t, ctrMsgs(m.checkImageUpdates())); got.skipped < 3 {
		t.Fatalf("%+v", got)
	}
	// Many images: the first 20.
	m.services.containerRows = nil
	for i := 0; i < imageUpdateMax+5; i++ {
		m.services.containerRows = append(m.services.containerRows, containers.Container{Image: fmt.Sprintf("img%02d:1", i), State: "running"})
	}
	if got := imageUpdateTargets(m.services); len(got) != imageUpdateMax {
		t.Fatalf("%d targets", len(got))
	}
	m.services.containerRows = nil
	_ = m.checkImageUpdates()
	if !strings.Contains(ctrToast(m), "No running containers with a tagged image") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	m.services.containerEngine.Reachable = false
	_ = m.checkImageUpdates()
	if !strings.Contains(ctrToast(m), "No running engine") {
		t.Fatalf("toast %s", ctrToast(m))
	}
}
