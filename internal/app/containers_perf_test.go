package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containers"
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
