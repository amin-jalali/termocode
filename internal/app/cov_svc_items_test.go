package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
)

func covSvcImage(t *testing.T, m *Model, ref string) svcRow {
	t.Helper()
	return selectRow(t, m.services, func(r svcRow) bool {
		return r.kind == svcRowHostItem && r.host == hostImage && r.imageRef == ref
	})
}

// Image keys: Enter runs (three prompts, then a card), t tags, u pulls,
// P pushes after a card, D removes after a card, h and i read pages, y
// copies the id.
func TestCovSvcImageKeys(t *testing.T) {
	m, f := covSvcHost(t)
	var clip []string
	defer SetClipboardSink(func(s string) { clip = append(clip, s) })()

	covSvcImage(t, m, "postgres:16")
	covSvcKey(m, "y")
	if len(clip) != 1 || clip[0] != "aaa" {
		t.Errorf("copied %q", clip)
	}
	// Enter: name, ports (one bad, one good), env, then the Run card.
	covSvcKey(m, "enter")
	if !m.promptOpen || m.promptKind != promptKindContainerRunName {
		t.Fatal("Enter: no name prompt")
	}
	submitPrompt(t, m, "-x")
	if !strings.Contains(regToast(m), "Container name cannot start with a dash") {
		t.Errorf("name toast = %q", regToast(m))
	}
	covSvcKey(m, "enter")
	submitPrompt(t, m, "db")
	submitPrompt(t, m, "-p")
	if !strings.Contains(regToast(m), "Port cannot start with a dash") {
		t.Errorf("port toast = %q", regToast(m))
	}
	m.promptOpen = true
	submitPrompt(t, m, "5433:5432")
	submitPrompt(t, m, "")
	submitPrompt(t, m, "nokey")
	if !strings.Contains(regToast(m), "KEY=value") {
		t.Errorf("env toast = %q", regToast(m))
	}
	m.promptOpen = true
	submitPrompt(t, m, "POSTGRES_PASSWORD=x")
	submitPrompt(t, m, "")
	if !m.confirmOpen || !strings.Contains(cardText(m), "-p 5433:5432") || !strings.Contains(cardText(m), "POSTGRES_PASSWORD=x") || !strings.Contains(cardText(m), "--name db") {
		t.Fatalf("run card = %s", cardText(m))
	}
	pumpHost(t, m, covSvcConfirm(t, m, "run"))
	if !strings.Contains(regToast(m), "Cannot run image") {
		t.Errorf("run without editor toast = %q", regToast(m))
	}

	// t: tag to a new name runs `tag`; to an existing tag asks first.
	covSvcImage(t, m, "postgres:16")
	covSvcKey(m, "t")
	if !m.promptOpen || m.promptKind != promptKindContainerTag {
		t.Fatal("t: no tag prompt")
	}
	if cmd := submitPrompt(t, m, " "); cmd != nil {
		t.Error("an empty tag did something")
	}
	covSvcKey(m, "t")
	submitPrompt(t, m, "-bad")
	if !strings.Contains(regToast(m), "Image reference cannot start with a dash") {
		t.Errorf("tag dash toast = %q", regToast(m))
	}
	covSvcKey(m, "t")
	pumpHost(t, m, submitPrompt(t, m, "postgres:prod"))
	if !strings.Contains(covSvcCalls(f), "tag postgres:16 postgres:prod") || !strings.Contains(regToast(m), "Image tagged") {
		t.Errorf("tag calls=%q toast=%q", f.Calls(), regToast(m))
	}
	covSvcImage(t, m, "postgres:16")
	covSvcKey(m, "t")
	submitPrompt(t, m, "redis:7")
	if !m.confirmOpen || !strings.Contains(cardText(m), "Replace image tag?") {
		t.Fatalf("replace card = %s", cardText(m))
	}
	f.Set(t, "all.err", "tag failed")
	pumpHost(t, m, covSvcConfirm(t, m, "tag"))
	if !strings.Contains(regToast(m), "Could not tag image") {
		t.Errorf("tag error toast = %q", regToast(m))
	}
	f.Remove("all.err")
	pumpHost(t, m, m.containersRefresh("images"))

	// u and P need an editor tab; P asks first and Cancel sends nothing.
	covSvcImage(t, m, "redis:7")
	covSvcKey(m, "u")
	if !m.promptOpen || m.promptKind != promptKindContainerPull {
		t.Fatal("u: no pull prompt")
	}
	submitPrompt(t, m, "-evil")
	if !strings.Contains(regToast(m), "cannot start with a dash") {
		t.Errorf("pull dash toast = %q", regToast(m))
	}
	covSvcKey(m, "u")
	submitPrompt(t, m, "redis:7")
	if !strings.Contains(regToast(m), "Cannot pull image") {
		t.Errorf("pull toast = %q", regToast(m))
	}
	covSvcKey(m, "P")
	if !m.confirmOpen || !strings.Contains(cardText(m), "docker push redis:7") {
		t.Fatalf("push card = %s", cardText(m))
	}
	if cmd := covSvcConfirm(t, m, "cancel"); cmd != nil || m.pendingImageRef != "" {
		t.Error("push cancel did something")
	}
	covSvcKey(m, "P")
	covSvcConfirm(t, m, "push")
	if !strings.Contains(regToast(m), "Cannot push image") {
		t.Errorf("push toast = %q", regToast(m))
	}

	// h and i read the engine (no editor: no page, no error); a failure
	// is a toast.
	covSvcImage(t, m, "redis:7")
	pumpHost(t, m, covSvcKey(m, "h"))
	pumpHost(t, m, covSvcKey(m, "i"))
	if f.Count("history") == 0 || f.Count("inspect") == 0 {
		t.Errorf("h/i calls = %q", f.Calls())
	}
	f.Set(t, "all.err", "No such image")
	pumpHost(t, m, m.imageHistory("redis:7"))
	if !strings.Contains(regToast(m), "Cannot open image history") {
		t.Errorf("history toast = %q", regToast(m))
	}
	f.Remove("all.err")

	// D: an image in use only says so; a free one asks and removes.
	covSvcImage(t, m, "postgres:16")
	covSvcKey(m, "D")
	if !m.confirmOpen || !strings.Contains(cardText(m), "Image is in use") || !strings.Contains(cardText(m), "postgres-1") {
		t.Fatalf("in use card = %s", cardText(m))
	}
	covSvcConfirm(t, m, "cancel")
	covSvcImage(t, m, "postgres:15-alpine")
	covSvcKey(m, "D")
	if !strings.Contains(cardText(m), "docker rmi postgres:15-alpine") {
		t.Fatalf("remove card = %s", cardText(m))
	}
	pumpHost(t, m, covSvcConfirm(t, m, "remove"))
	if !strings.Contains(covSvcCalls(f), "rmi postgres:15-alpine") {
		t.Errorf("rmi calls = %q", f.Calls())
	}
	f.Set(t, "all.err", "conflict: image is being used")
	covSvcImage(t, m, "postgres:15-alpine")
	covSvcKey(m, "D")
	pumpHost(t, m, covSvcConfirm(t, m, "remove"))
	if !strings.Contains(regToast(m), "Could not remove image") {
		t.Errorf("rmi error toast = %q", regToast(m))
	}
}

// An untagged image runs and copies by its id.
func TestCovSvcUntaggedImageUsesID(t *testing.T) {
	m, _ := covSvcHost(t)
	m.services.imageRows = append(m.services.imageRows, containers.Image{ID: "sha256:ddd", RepoTags: []string{"<none>:<none>"}, Size: "1MB"})
	row := selectRow(t, m.services, func(r svcRow) bool {
		return r.kind == svcRowHostItem && r.host == hostImage && r.idx == 3
	})
	_ = row
	_, ref, ok := m.selectedImage()
	if !ok || ref != "ddd" {
		t.Errorf("untagged ref = %q %v", ref, ok)
	}
	m.askImageTag(ref)
	m.pendingImageRef = ""
	if cmd := submitPrompt(t, m, "mine:1"); cmd == nil {
		t.Error("tag from the selected image did nothing")
	}
}

// Network and volume keys: D checks who uses it first; built-in networks
// stay; a used volume only says so; Space lists the users.
func TestCovSvcStorageKeys(t *testing.T) {
	m, f := covSvcHost(t)
	f.Set(t, "inspect.json", `[{"Name":"termocode_default","Containers":{"x":{"Name":"/termocode-api-1"},"y":{"Name":""}}}]`)
	f.Set(t, "ps.json", `{"ID":"c1","Names":"termocode-postgres-1","State":"running"}`+"\n")
	covSvcHostItem(t, m, hostNetwork, "bridge")
	pumpHost(t, m, covSvcKey(m, "D"))
	if !strings.Contains(regToast(m), "Built-in network") {
		t.Errorf("bridge toast = %q", regToast(m))
	}
	covSvcHostItem(t, m, hostNetwork, "termocode_default")
	pumpHost(t, m, covSvcKey(m, "D"))
	if !m.confirmOpen || !strings.Contains(cardText(m), "Containers using it: termocode-api-1") || !strings.Contains(cardText(m), "docker network rm termocode_default") {
		t.Fatalf("network card = %s", cardText(m))
	}
	pumpHost(t, m, covSvcConfirm(t, m, "remove"))
	if !strings.Contains(covSvcCalls(f), "network rm termocode_default") {
		t.Errorf("rm calls = %q", f.Calls())
	}
	// The volume is used by c1 (the filtered ps answers it).
	covSvcHostItem(t, m, hostVolume, "termocode_db")
	pumpHost(t, m, covSvcKey(m, "D"))
	if !m.confirmOpen || !strings.Contains(cardText(m), "Volume is in use") {
		t.Fatalf("volume card = %s", cardText(m))
	}
	covSvcConfirm(t, m, "cancel")
	// Space opens the user list (already checked: no new call).
	n := f.Count("inspect")
	covSvcHostItem(t, m, hostNetwork, "termocode_default")
	pumpHost(t, m, covSvcKey(m, " "))
	if got := svcRowsText(m.services); !strings.Contains(got, "Used by termocode-api-1") || f.Count("inspect") != n {
		t.Errorf("rows = %s", got)
	}
	covSvcHostItem(t, m, hostNetwork, "termocode_default")
	covSvcKey(m, " ")
	if got := svcRowsText(m.services); strings.Contains(got, "Used by") {
		t.Errorf("fold kept users: %s", got)
	}
	// A failed check: the row says so and D refuses.
	f.Set(t, "all.err", "daemon gone")
	m.services.clearStorageInspect("volume")
	covSvcHostItem(t, m, hostVolume, "termocode_db")
	pumpHost(t, m, covSvcKey(m, "D"))
	if !strings.Contains(regToast(m), "Could not check container use") {
		t.Errorf("check fail toast = %q", regToast(m))
	}
	covSvcHostItem(t, m, hostVolume, "termocode_db")
	covSvcKey(m, "D")
	covSvcHostItem(t, m, hostVolume, "termocode_db")
	covSvcKey(m, " ")
	if got := svcRowsText(m.services); !strings.Contains(got, "Could not check container use") {
		t.Errorf("error row: %s", got)
	}
	// While a check runs the row says so; a stale answer is dropped.
	m.services.clearStorageInspect("volume")
	r := covSvcHostItem(t, m, hostVolume, "termocode_db")
	cmd := m.inspectStorageUsers(r)
	if got := svcRowsText(m.services); !strings.Contains(got, "Checking containers…") {
		t.Errorf("loading row: %s", got)
	}
	if again := m.inspectStorageUsers(r); again != nil {
		t.Error("a second check started")
	}
	msg := cmd().(storageInspectMsg)
	m.services.containerGen++
	if m.onStorageInspect(msg) != nil || m.services.storageInspectState[msg.key] != "loading" {
		t.Error("a stale answer was used")
	}
}

// What a network inspect answer can look like.
func TestCovSvcNetworkInspectUsers(t *testing.T) {
	if _, err := networkInspectUsers([]any{}); err == nil {
		t.Error("empty answer accepted")
	}
	if _, err := networkInspectUsers([]any{"x"}); err == nil {
		t.Error("bad answer accepted")
	}
	got, err := networkInspectUsers([]any{map[string]any{"Containers": map[string]any{"a": map[string]any{"Name": "/b"}, "c": map[string]any{"Name": "/a"}}}})
	if err != nil || strings.Join(got, ",") != "a,b" {
		t.Errorf("users = %q %v", got, err)
	}
	rows := []containers.Container{{ID: "1", Names: []string{"one"}, Networks: []string{"n"}, Mounts: []string{"v"}}, {ID: "2", Names: []string{"two"}}}
	if got := storageSnapshotUsers(rows, svcRow{host: hostVolume, hostID: "v"}); strings.Join(got, ",") != "one" {
		t.Errorf("volume users = %q", got)
	}
	if got := storageSnapshotUsers(rows, svcRow{host: hostNetwork, hostID: "n"}); strings.Join(got, ",") != "one" {
		t.Errorf("network users = %q", got)
	}
	if wrapStorageName("abcdefgh", 3) != "abc\ndef\ngh" || wrapStorageName("ab", 3) != "ab" || wrapStorageName("ab", 0) != "ab" {
		t.Error("wrapStorageName")
	}
}

// Context keys: Enter uses it here, D asks to make it the default (and
// runs `context use` after the card), y copies the endpoint.
func TestCovSvcContextKeys(t *testing.T) {
	m, f := covSvcHost(t)
	m.services.contextRows = []containers.Context{{Name: "default", Current: true, DockerEndpoint: "unix:///var/run/docker.sock"}, {Name: "prod", DockerEndpoint: "ssh://deploy@prod"}, {Name: "bare"}}
	var clip []string
	defer SetClipboardSink(func(s string) { clip = append(clip, s) })()
	covSvcHostItem(t, m, hostContext, "prod")
	covSvcKey(m, "y")
	covSvcHostItem(t, m, hostContext, "bare")
	covSvcKey(m, "y")
	if strings.Join(clip, ",") != "ssh://deploy@prod" || !strings.Contains(regToast(m), "No endpoint") {
		t.Errorf("copy: %q %q", clip, regToast(m))
	}
	covSvcHostItem(t, m, hostContext, "prod")
	covSvcKey(m, "D")
	if !m.confirmOpen || !strings.Contains(cardText(m), "docker context use prod") {
		t.Fatalf("default card = %s", cardText(m))
	}
	pumpHost(t, m, covSvcConfirm(t, m, "use"))
	if !strings.Contains(covSvcCalls(f), "context use prod") || !strings.Contains(regToast(m), "Default context is now prod") {
		t.Errorf("use calls=%q toast=%q", f.Calls(), regToast(m))
	}
	f.Set(t, "all.err", "context not found")
	m.services.contextRows = []containers.Context{{Name: "default", Current: true}, {Name: "prod"}}
	covSvcHostItem(t, m, hostContext, "prod")
	covSvcKey(m, "D")
	pumpHost(t, m, covSvcConfirm(t, m, "use"))
	if !strings.Contains(regToast(m), "Could not change the default context") {
		t.Errorf("use error toast = %q", regToast(m))
	}
	f.Remove("all.err")
	// The context menu does the same three things.
	r := covSvcHostItem(t, m, hostContext, "prod")
	m.openContextMenu(r, 10, 10)
	if !m.menuOpen {
		t.Fatal("no context menu")
	}
	m.handleServicesMenuSelect("copy")
	m.handleServicesMenuSelect("default")
	if !m.confirmOpen {
		t.Error("menu default: no card")
	}
	m.confirmOpen = false
	if cmd := m.handleServicesMenuSelect("use"); cmd == nil {
		t.Error("menu use did nothing")
	}
	if cmd := m.handleServicesMenuSelect("nope"); cmd != nil {
		t.Error("unknown menu id did something")
	}
	// Guards.
	if m.switchContext("") != nil || m.askContextDefault("") != nil {
		t.Error("empty name did something")
	}
	m.switchContext("ghost")
	if !strings.Contains(regToast(m), "Unknown context") {
		t.Errorf("unknown toast = %q", regToast(m))
	}
	m.services.containerEngine.Binary = ""
	m.askContextDefault("prod")
	if !strings.Contains(cardText(m), "docker context use prod") {
		t.Errorf("no binary card = %s", cardText(m))
	}
	if m.copyContextEndpoint(podmanLocal) == nil || !strings.Contains(regToast(m), "local is podman") {
		t.Errorf("podman local copy = %q", regToast(m))
	}
	if m.askContextDefault(podmanLocal) == nil || !strings.Contains(regToast(m), "local is not a connection") {
		t.Errorf("podman local default = %q", regToast(m))
	}
}

// The palette context commands: with one context it says so; else the
// section opens with the cursor on a context to pick.
func TestCovSvcContextForPalette(t *testing.T) {
	m, _ := covSvcHost(t)
	m.services.contextRows = []containers.Context{{Name: "default", Current: true}}
	if name, cmd := m.contextForPalette(); name != "" || cmd == nil || !strings.Contains(regToast(m), "Only the default context exists") {
		t.Errorf("one context: %q %q", name, regToast(m))
	}
	m.services.contextRows = []containers.Context{{Name: "default", Current: true}, {Name: "prod"}}
	m.services.collapsed[hostSection("contexts")] = true
	covSvcSection(t, m, "containers")
	if name, _ := m.contextForPalette(); name != "" || !strings.Contains(regToast(m), "Pick a context") {
		t.Errorf("pick: %q %q", name, regToast(m))
	}
	if r, _ := m.servicesCurrentRow(); r.host != hostContext || r.hostID != "prod" {
		t.Errorf("cursor on %+v", r)
	}
	if name, cmd := m.contextForPalette(); name != "prod" || cmd != nil {
		t.Errorf("selected: %q", name)
	}
}
