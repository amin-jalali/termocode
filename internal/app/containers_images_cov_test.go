package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
)

// Pull, push, history and inspect from an image row: tabs open (or say
// they cannot), pages come back from the fake docker.
func TestCovContainerImagePullPushPages(t *testing.T) {
	m, f := ctrModel(t)
	if m.imagePull("  ") != nil {
		t.Fatal("empty pull")
	}
	_ = m.imagePull("redis:7")
	if !strings.Contains(ctrToast(m), "Cannot pull image") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.imagePush("redis:7")
	if !strings.Contains(ctrToast(m), "Cannot push image") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	ctrNvim(t, m)
	// REGISTRIES before detection: the engine is looked up on PATH.
	m.services.containerEngine = containers.Engine{}
	if m.onContainerImagePrompt(promptKindContainerPull, " alpine ") != nil || !m.servicesFocusTab(containerTabPrefix+"pull:alpine") {
		t.Fatalf("pull: %s", ctrToast(m))
	}
	m.services.containerEngine = containers.Engine{Binary: "docker", Kind: "docker", Reachable: true}
	if m.onContainerImagePrompt(promptKindContainerPull, "  ") != nil {
		t.Fatal("empty pull prompt")
	}
	if m.imagePush("redis:7") != nil || !m.servicesFocusTab(containerTabPrefix+"push:redis:7") {
		t.Fatalf("push: %s", ctrToast(m))
	}
	f.Set(t, "history.json", `{"ID":"sha256:1","CreatedBy":"x","Size":"1MB"}`)
	f.Set(t, "inspect.json", `[{"Id":"sha256:aaa"}]`)
	ctrImageRow(t, m)
	page := ctrRun[imagePageMsg](t, ctrKey(m, "h"))
	if page.err != nil || page.title != "history" {
		t.Fatalf("history %+v", page)
	}
	if m.onImagePage(page) != nil {
		t.Fatalf("page: %s", ctrToast(m))
	}
	page = ctrRun[imagePageMsg](t, ctrKey(m, "i"))
	if page.err != nil || page.title != "inspect" || !strings.Contains(page.text, "sha256:aaa") {
		t.Fatalf("inspect %+v", page)
	}
	f.Set(t, "inspect.json", "not json")
	page = ctrRun[imagePageMsg](t, m.imageInspect("x"))
	f.Set(t, "history.json", "not json")
	page2 := ctrRun[imagePageMsg](t, m.imageHistory("x"))
	if page.err == nil || page2.err == nil {
		t.Fatal("bad json gave no error")
	}
	_ = m.onImagePage(page)
	if !strings.Contains(ctrToast(m), "Cannot open image inspect") {
		t.Fatalf("toast %s", ctrToast(m))
	}
}

// Tag: the prompt checks the value, a tag that is already on another
// image asks first, a new one runs docker tag; the done toast says so.
func TestCovContainerImageTag(t *testing.T) {
	m, f := ctrModel(t)
	ctrImageRow(t, m)
	_ = ctrKey(m, "t")
	if !m.promptOpen || m.promptKind != promptKindContainerTag || !strings.HasSuffix(m.prompt.Value(), "-copy") {
		t.Fatalf("tag prompt %q", m.prompt.Value())
	}
	m.pendingImageRef = ""
	if m.onContainerImagePrompt(promptKindContainerTag, "  ") != nil {
		t.Fatal("empty tag")
	}
	_ = m.onContainerImagePrompt(promptKindContainerTag, "-x")
	if !strings.Contains(ctrToast(m), "cannot start with a dash") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	m.pendingImageRef = "postgres:16"
	_ = m.onContainerImagePrompt(promptKindContainerTag, "redis:7")
	if !m.confirmOpen || m.pendingHostOperation != "tag" || !strings.Contains(m.confirm.View(), "Replace image tag?") {
		t.Fatal("no replace-tag card")
	}
	m.pendingImageRef = "postgres:16"
	done := ctrRun[hostActionDoneMsg](t, m.onContainerImagePrompt(promptKindContainerTag, "acme/pg:1"))
	if done.err != nil || f.Count("tag postgres:16 acme/pg:1") != 1 {
		t.Fatalf("%+v %v", done, f.Calls())
	}
	_ = m.onHostActionDone(done)
	if !strings.Contains(ctrToast(m), "Image tagged") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	for op, want := range map[string]string{
		"tag": "Could not tag image", "remove-image": "Could not remove image", "remove-storage": "Could not remove item",
		"create-network": "Could not create network", "create-volume": "Could not create volume", "prune-images-all": "Could not prune images (incl. unused)",
		"other": "Could not complete container action",
	} {
		_ = m.onHostActionDone(hostActionDoneMsg{operation: op, err: errors.New("x")})
		if !strings.Contains(ctrToast(m), want) {
			t.Fatalf("%s: toast %s", op, ctrToast(m))
		}
	}
	_ = m.onHostActionDone(hostActionDoneMsg{operation: "remove-image", section: "images"})
	_ = m.onContainerImagePrompt(promptKindContainerStorageName, "a b")
	if !strings.Contains(ctrToast(m), "without spaces") {
		t.Fatalf("toast %s", ctrToast(m))
	}
}

// Remove: an image in use only says so; a free one gets the rmi card.
// Enter on an image row starts Run: name, ports, env, then the card.
func TestCovContainerImageRemoveAndRun(t *testing.T) {
	m, f := ctrModel(t)
	m.services.containerRows[0].ImageID = "sha256:aaa"
	_ = m.askImageRemove(m.services.imageRows[0])
	if !m.confirmOpen || !strings.Contains(m.confirm.View(), "Image is in use") {
		t.Fatal("no in-use card")
	}
	_ = m.askImageRemove(m.services.imageRows[1])
	if m.pendingHostOperation != "remove-image" || !strings.Contains(m.confirm.View(), "rmi sha256:bbb") {
		t.Fatalf("rmi card %s", m.confirm.View())
	}
	done := ctrRun[hostActionDoneMsg](t, m.runHostAction("remove"))
	if done.err != nil || f.Count("rmi sha256:bbb") != 1 {
		t.Fatalf("%+v %v", done, f.Calls())
	}
	f.Set(t, "all.err", "conflict")
	_ = m.askImageRemove(m.services.imageRows[1])
	if done := ctrRun[hostActionDoneMsg](t, m.runHostAction("remove")); done.err == nil {
		t.Fatal("no error")
	}
	f.Remove("all.err")
	// Run.
	ctrImageRow(t, m)
	_ = ctrKey(m, "enter")
	if !m.promptOpen || m.promptKind != promptKindContainerRunName {
		t.Fatal("no run prompt")
	}
	_ = m.onContainerImagePrompt(promptKindContainerRunName, "-x")
	if !strings.Contains(ctrToast(m), "Container name cannot start with a dash") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.onContainerImagePrompt(promptKindContainerRunName, "web")
	if m.promptKind != promptKindContainerRunPorts {
		t.Fatal("no ports prompt")
	}
	_ = m.onContainerImagePrompt(promptKindContainerRunPorts, "-p")
	if !strings.Contains(ctrToast(m), "Port cannot start with a dash") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.onContainerImagePrompt(promptKindContainerRunPorts, "8080:80")
	_ = m.onContainerImagePrompt(promptKindContainerRunPorts, "")
	if m.promptKind != promptKindContainerRunEnv {
		t.Fatal("no env prompt")
	}
	_ = m.onContainerImagePrompt(promptKindContainerRunEnv, "=bad")
	if !strings.Contains(ctrToast(m), "KEY=value") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	_ = m.onContainerImagePrompt(promptKindContainerRunEnv, "A=1")
	_ = m.onContainerImagePrompt(promptKindContainerRunEnv, "")
	if !m.confirmOpen || m.pendingHostOperation != "run-image" || !strings.Contains(cardText(m), "8080:80") {
		t.Fatalf("run card %s", cardText(m))
	}
	_ = m.runHostAction("run")
	if !strings.Contains(ctrToast(m), "Cannot run image") {
		t.Fatalf("toast %s", ctrToast(m))
	}
	ctrNvim(t, m)
	m.pendingHostArgs, m.pendingHostOperation = []string{"docker", "run", "--rm", "postgres:16"}, "run-image"
	if m.runHostAction("run") != nil {
		t.Fatalf("run: %s", ctrToast(m))
	}
	if got := promptList("a, b ,,c"); strings.Join(got, "|") != "a|b|c" {
		t.Fatal(got)
	}
	if got := promptList("a,b\nc"); strings.Join(got, "|") != "a,b|c" {
		t.Fatal(got)
	}
	if got := promptLines(" a \r\n\r\nb"); strings.Join(got, "|") != "a|b" {
		t.Fatal(got)
	}
	// A dangling image row uses the id as its ref.
	m.services.imageRows = append(m.services.imageRows, containers.Image{ID: "sha256:fff", RepoTags: []string{"<none>:<none>"}})
	m.services.touch()
	selectRow(t, m.services, func(r svcRow) bool {
		return r.kind == svcRowHostItem && r.host == hostImage && r.idx == len(m.services.imageRows)-1
	})
	if _, ref, ok := m.selectedImage(); !ok || ref != "fff" {
		t.Fatalf("dangling ref %q", ref)
	}
}
