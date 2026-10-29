package app

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services"
)

// imagesInUseOneRepo is the old per-repository count (one pass over the
// containers for each repository): the reference for imageUseCounts.
func imagesInUseOneRepo(st *servicesState, refs []imageRef) int {
	ids, names := map[string]bool{}, map[string]bool{}
	for _, ref := range refs {
		im := st.imageRows[ref.idx]
		ids[strings.TrimPrefix(im.ID, "sha256:")] = true
		for _, tag := range im.RepoTags {
			names[tag] = true
		}
	}
	used := 0
	for _, c := range st.containerRows {
		if ids[strings.TrimPrefix(c.ImageID, "sha256:")] || names[c.Image] {
			used++
		}
	}
	return used
}

// PERF-5: the "N in use" counts of every repository come from one pass
// over the containers and match the old count of each repository: by
// image ID (with or without "sha256:"), by any tag of the repository's
// images (also a tag of another repository on the same image), and a
// container that matches twice counts once.
func TestImageUseCountsMatchPerRepoCount(t *testing.T) {
	st := svcHostState()
	st.imageRows = []containers.Image{
		{ID: "sha256:aaa", RepoTags: []string{"postgres:16", "mirror/postgres:16"}},
		{ID: "sha256:bbb", RepoTags: []string{"postgres:15"}},
		{ID: "sha256:ccc", RepoTags: []string{"redis:7"}},
		{ID: "sha256:ddd"}, // dangling
		{ID: "eee", RepoTags: []string{"api:1", "api:latest"}},
	}
	st.containerRows = []containers.Container{
		{ID: "1", Image: "postgres:16", ImageID: "sha256:aaa"}, // both rules, counts once
		{ID: "2", Image: "mirror/postgres:16"},                 // a tag of postgres' image
		{ID: "3", Image: "something", ImageID: "bbb"},          // ID without the prefix
		{ID: "4", Image: "redis:7"},                            // by tag
		{ID: "5", Image: "x", ImageID: "sha256:ddd"},           // the dangling image
		{ID: "6", Image: "api:latest", ImageID: "sha256:eee"},  // tag and prefixed ID
		{ID: "7", Image: "nothing", ImageID: "sha256:zzz"},     // no image
		{ID: "8", Image: "postgres:15", ImageID: "sha256:ccc"}, // two repositories
	}
	for i := 0; i < 30; i++ { // and a bigger mix
		st.imageRows = append(st.imageRows, containers.Image{ID: fmt.Sprintf("sha256:%03d", i), RepoTags: []string{fmt.Sprintf("app%d:%d", i%7, i%3)}})
		st.containerRows = append(st.containerRows, containers.Container{ID: fmt.Sprintf("c%d", i), Image: fmt.Sprintf("app%d:%d", i%5, i%4), ImageID: fmt.Sprintf("%03d", (i*7)%40)})
	}
	byRepo, repos := st.imageRepos()
	got := st.imageUseCounts(byRepo)
	for _, repo := range repos {
		if want := imagesInUseOneRepo(st, byRepo[repo]); got[repo] != want {
			t.Errorf("%s: %d in use, want %d", repo, got[repo], want)
		}
	}
	if got["postgres"] != 4 || got["mirror/postgres"] != 2 || got["redis"] != 2 {
		t.Errorf("hand-checked counts: %v", got)
	}
}

// PERF-5: the stats check runs after every update of the app. When
// neither the view state nor the cursor changed since the last check it
// builds nothing; a cursor move or a touched state makes it follow again.
func TestStatsRetargetSkipsUnchangedState(t *testing.T) {
	st := perfHost()
	m := svcTestModel(st, 120, 50)
	running := -1
	for i, r := range st.rows() {
		if r.kind == svcRowHostItem && r.host == hostContainer && st.containerRows[r.idx].State == "running" {
			running = i
			break
		}
	}
	st.cursor = running
	m.statsSync()
	if st.stats.detailID == "" {
		t.Fatal("charts do not follow the cursor onto a running container")
	}
	if n := testing.AllocsPerRun(50, func() { m.statsSync() }); n > 0 {
		t.Errorf("unchanged state: the stats check allocated %.0f times per update", n)
	}
	// The cursor moves to a group row: the charts go away.
	st.cursor = 2
	for st.rows()[st.cursor].kind != svcRowGroup {
		st.cursor++
	}
	m.statsSync()
	if st.stats.detailID != "" {
		t.Errorf("cursor left the container but its charts stay (%q)", st.stats.detailID)
	}
	// The state changes under a still cursor (the row there is now a
	// running container): touch makes the check look again.
	st.cursor = running
	m.statsSync()
	id := st.stats.detailID
	st.stats.series = map[string]*containers.Series{}
	st.touch()
	m.statsSync()
	if st.stats.detailID != "" {
		t.Errorf("the samples are gone but the charts of %s stay", id)
	}
}

// gateExec runs the fake engine, but `context ls` waits until `ps` has
// started: it answers only when the two run at the same time.
type gateExec struct {
	services.Exec
	psStarted chan struct{}
	once      sync.Once
}

func (g *gateExec) Capture(ctx context.Context, dir string, argv ...string) (string, string, int, error) {
	args := strings.Join(argv[1:], " ")
	if strings.HasPrefix(args, "ps ") {
		g.once.Do(func() { close(g.psStarted) })
	}
	if strings.HasPrefix(args, "context ls") {
		select {
		case <-g.psStarted:
		case <-time.After(5 * time.Second):
			return "", "context ls ran before ps started", 1, nil
		}
	}
	return g.Exec.Capture(ctx, dir, argv...)
}

// PERF-5: opening the view (and a context switch) finds the engine and
// then lists the contexts and the open sections at the same time, not
// one engine call after the other.
func TestContainersDetectListsInParallel(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("DOCKER_CONTEXT", "")
	hostFake(t)
	old := servicesExec
	servicesExec = &gateExec{Exec: services.SpawnExec{}, psStarted: make(chan struct{})}
	defer func() { servicesExec = old }()
	st := svcTestState()
	st.root = t.TempDir()
	st.sections = map[string]bool{}
	st.visible = true
	st.collapsed[hostSection("containers")] = false
	m := svcTestModel(st, 100, 80)
	msg, ok := m.containersDetect()().(containerHostMsg)
	if !ok {
		t.Fatal("detect gave no host message")
	}
	if !msg.engine.Reachable || len(msg.errs) != 0 || len(msg.contexts) != 2 || len(msg.containers) == 0 {
		t.Fatalf("engine %v, errs %v, %d contexts, %d containers", msg.engine.Reachable, msg.errs, len(msg.contexts), len(msg.containers))
	}
	if !slices.Contains(msg.loaded, "contexts") || !slices.Contains(msg.loaded, "containers") {
		t.Errorf("loaded %v", msg.loaded)
	}
}

// PERF-5: a burst of engine events (compose up of many projects) is
// taken as one message: the events already waiting come together, so
// the view lists its sections once per burst, not once per event. An
// event of a kind that is not in the burst lists nothing.
func TestContainerEventBurstIsOneMessage(t *testing.T) {
	ch := make(chan containers.Event, 64)
	for i := 0; i < 40; i++ {
		ch <- containers.Event{Type: "container", Action: "start"}
	}
	ch <- containers.Event{Type: "network", Action: "create"}
	msg := waitContainerEvent(context.Background(), ch, 7)().(containerEventMsg)
	if msg.closed || msg.gen != 7 || len(msg.events) != 41 {
		t.Fatalf("closed %v gen %d, %d events (want 41 in one message)", msg.closed, msg.gen, len(msg.events))
	}
	if len(ch) != 0 {
		t.Errorf("%d events left behind", len(ch))
	}
	kinds := containerEventKinds(msg.events)
	if !kinds["container"] || !kinds["network"] || kinds["image"] || kinds["volume"] {
		t.Errorf("kinds %v", kinds)
	}
	// A closed stream after the waiting events: the events come first.
	ch <- containers.Event{Type: "image"}
	close(ch)
	msg = waitContainerEvent(context.Background(), ch, 7)().(containerEventMsg)
	if len(msg.events) != 1 || msg.closed {
		t.Fatalf("events %d closed %v", len(msg.events), msg.closed)
	}
	if msg = waitContainerEvent(context.Background(), ch, 7)().(containerEventMsg); !msg.closed {
		t.Fatal("the closed stream was not reported")
	}
}
