package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/containers"
)

const (
	ctrBuildHistory = `{"ref":"abc123","name":"web","status":"Completed","created_at":"2026-11-01T10:00:00Z","total_steps":12,"completed_steps":12,"cached_steps":9,"duration":"4.2s"}
{"Ref":"def456","Name":"api","Status":"Error","CreatedAt":"2026-11-02T10:00:00Z","TotalSteps":3,"CompletedSteps":1,"CachedSteps":0,"Error":"failed to solve"}
{"Ref":"ghi789","Name":"","Status":"Running"}
`
	ctrBuilders = `{"Name":"default","Driver":"docker","Current":false,"Nodes":[{"Name":"default","Status":"running","Platforms":["linux/amd64","linux/arm64"]}]}
{"Name":"multi","Driver":"docker-container","Current":true,"Nodes":[{"Name":"multi0","Status":"inactive"}]}
`
	ctrDF = `{"Type":"Images","TotalCount":"5","Active":"2","Size":"1.2GB","Reclaimable":"800MB (66%)"}
{"Type":"Build Cache","TotalCount":"41","Active":"0","Size":"2.5GB","Reclaimable":"2.5GB"}
`
)

// The Builds page lists history (newest first), builders and the cache.
func TestCovContainerBuildsPage(t *testing.T) {
	m, _ := ctrModel(t)
	x := ctrScripted(t, m)
	x.on("buildx history ls", ctrBuildHistory)
	x.on("buildx ls", ctrBuilders)
	x.on("system df", ctrDF)
	t.Cleanup(func() { m.services.stopHostWatch(); m.services.stopWatch() })
	if _, ok := m.dispatchContainersBuilds("dance"); ok {
		t.Fatal("unknown id")
	}
	_, _ = m.dispatchContainersBuilds("containers-builds")
	page := ctrRun[containersPageMsg](t, m.containersBuildsPage())
	for _, want := range []string{"api  2026-11-02", "failed: failed to solve", "9/12 cached", "4.2s", "completed", "running", "* multi", "linux/amd64, linux/arm64", "41 entries (0 in use)  size 2.5GB"} {
		if !strings.Contains(page.text, want) {
			t.Errorf("page lacks %q:\n%s", want, page.text)
		}
	}
	if strings.Index(page.text, "def456") > strings.Index(page.text, "abc123") {
		t.Error("not newest first")
	}
	// Errors and empty lists.
	txt := buildsPageText(nil, errors.New("no buildx"), nil, errors.New("no builders"), containers.BuildCacheUsage{}, errors.New("no df"), nil)
	for _, want := range []string{"no buildx", "no builders", "no df"} {
		if !strings.Contains(txt, want) {
			t.Errorf("lacks %q", want)
		}
	}
	txt = buildsPageText(nil, nil, nil, nil, containers.BuildCacheUsage{}, nil, time.UTC)
	if !strings.Contains(txt, "No builds yet.") || !strings.Contains(txt, "No builders.") || !strings.Contains(txt, "size -") {
		t.Fatal(txt)
	}
	failed := []containers.BuildRecord{{Ref: "r", Status: "Error"}}
	if txt := buildsPageText(failed, nil, nil, nil, containers.BuildCacheUsage{}, nil, time.UTC); !strings.Contains(txt, "failed: error") {
		t.Fatal(txt)
	}
	if len(containersBuildsPaletteItems()) != 4 {
		t.Fatal("palette rows")
	}
}

// Build detail: the picker lists builds, a pick opens a tab (or says it
// cannot); errors and no builds toast.
func TestCovContainerBuildDetail(t *testing.T) {
	m, _ := ctrModel(t)
	x := ctrScripted(t, m)
	x.on("buildx history ls", ctrBuildHistory)
	t.Cleanup(func() { m.services.stopHostWatch(); m.services.stopWatch() })
	_, _ = m.dispatchContainersBuilds("containers-build-detail")
	list := ctrRun[buildsListMsg](t, m.containersBuildDetailPicker())
	_ = m.onBuildsList(list)
	if !m.pickerOpen || !strings.Contains(m.picker.View(), "failed") || !strings.Contains(m.picker.View(), "abc123") {
		t.Fatalf("picker:\n%s", m.picker.View())
	}
	_ = m.onBuildsList(buildsListMsg{err: errors.New("x")})
	if !strings.Contains(ctrToast(m), "Cannot list builds") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.onBuildsList(buildsListMsg{})
	if !strings.Contains(ctrToast(m), "No builds yet") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.handleContainersExtraPick("ctx-x:build:abc123")
	if !strings.Contains(ctrToast(m), "Cannot show the build") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.openBuildDetail("-bad")
	if !strings.Contains(ctrToast(m), "Not shown") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	if m.handleContainersExtraPick("ctx-x:dance:x") != nil {
		t.Fatal("unknown pick")
	}
	ctrNvim(t, m)
	if m.openBuildDetail("abc123") != nil || !m.servicesFocusTab(containerTabPrefix+"build:abc123") {
		t.Fatalf("no tab: %s", ctrToast(m))
	}
	_, _ = m.dispatchContainersBuilds("containers-build-check")
	if !m.servicesFocusTab(containerTabPrefix + "build-check") {
		t.Fatalf("no check tab: %s", ctrToast(m))
	}
}

// The prune card offers unused and all (docker), only one for podman;
// errors toast; no engine says so.
func TestCovContainerBuildCachePrune(t *testing.T) {
	m, _ := ctrModel(t)
	x := ctrScripted(t, m)
	x.on("system df", ctrDF)
	t.Cleanup(func() { m.services.stopHostWatch(); m.services.stopWatch() })
	_, _ = m.dispatchContainersBuilds("containers-build-cache-prune")
	u := ctrRun[buildCacheUsageMsg](t, m.askBuildCachePrune())
	_ = m.onBuildCacheUsage(u)
	if !m.confirmOpen || len(m.services.pendingExtra) != 2 || !strings.Contains(m.confirm.View(), "41 entries") {
		t.Fatalf("card: %s", m.confirm.View())
	}
	done := ctrRun[containersExtraDoneMsg](t, m.onContainersExtraConfirm("prune-all"))
	if done.err != nil || x.count("builder prune --force --all") != 1 {
		t.Fatalf("%+v %v", done, x.calls)
	}
	m.services.containerEngine.Kind = "podman"
	_ = m.onBuildCacheUsage(u)
	if len(m.services.pendingExtra) != 1 {
		t.Fatalf("podman jobs %v", m.services.pendingExtra)
	}
	_ = m.onBuildCacheUsage(buildCacheUsageMsg{err: errors.New("x")})
	if !strings.Contains(ctrToast(m), "Cannot read the build cache") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.containersBuildCheck()
	if !strings.Contains(ctrToast(m), "Cannot run the build checks") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	m.services.containerEngine.Reachable = false
	for _, f := range []func() any{
		func() any { return m.containersBuildsPage() }, func() any { return m.containersBuildDetailPicker() },
		func() any { return m.askBuildCachePrune() }, func() any { return m.containersBuildCheck() }, func() any { return m.openBuildDetail("a") },
	} {
		_ = f()
		if !strings.Contains(ctrToast(m), "No container engine") {
			t.Fatalf("toast %s", ctrToast(m))
		}
	}
}
