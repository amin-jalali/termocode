package containers

import (
	"reflect"
	"testing"
)

func TestParseListQueryTokens(t *testing.T) {
	q := ParseListQuery("State:Running  image:Nginx web")
	if !q.Tokens || !reflect.DeepEqual(q.States, []string{"running"}) || q.Image != "nginx" || q.Text != "web" {
		t.Fatalf("ParseListQuery = %+v", q)
	}
	if q := ParseListQuery("is:stopped,paused"); !reflect.DeepEqual(q.States, []string{"exited", "paused"}) {
		t.Fatalf("aliases and lists: %+v", q.States)
	}
	if q := ParseListQuery("is:dangling"); !q.Dangling || len(q.States) != 0 {
		t.Fatalf("dangling: %+v", q)
	}
	if q := ParseListQuery("postgres"); q.Tokens || q.Text != "postgres" {
		t.Fatalf("plain text has no tokens: %+v", q)
	}
}

func TestListQueryMatchContainer(t *testing.T) {
	web := Container{ID: "aa11", Names: []string{"/shop-web-1"}, Image: "nginx:1.27", State: "running"}
	job := Container{ID: "bb22", Names: []string{"migrate"}, Image: "termocode-migrate", State: "exited"}
	for _, c := range []struct {
		q        string
		web, job bool
	}{
		{"state:running", true, false},
		{"is:exited", false, true},
		{"is:up", true, false},
		{"image:nginx", true, false},
		{"is:running image:migrate", false, false},
		{"is:running,exited web", true, false},
		{"is:dangling", false, false},
	} {
		q := ParseListQuery(c.q)
		if got := q.MatchContainer(web); got != c.web {
			t.Errorf("%q on web = %v", c.q, got)
		}
		if got := q.MatchContainer(job); got != c.job {
			t.Errorf("%q on job = %v", c.q, got)
		}
	}
}

func TestListQueryMatchImage(t *testing.T) {
	tagged := Image{ID: "sha256:1", RepoTags: []string{"postgres:16"}}
	dangling := Image{ID: "sha256:2", RepoTags: []string{"<none>:<none>"}}
	if !ParseListQuery("is:dangling").MatchImage(dangling) || ParseListQuery("is:dangling").MatchImage(tagged) {
		t.Fatal("is:dangling must match only untagged images")
	}
	if !ParseListQuery("image:postgres").MatchImage(tagged) || ParseListQuery("image:redis").MatchImage(tagged) {
		t.Fatal("image: must match the tag")
	}
	if ParseListQuery("is:running").MatchImage(tagged) {
		t.Fatal("a state filter never matches an image")
	}
}

func TestSizeBytes(t *testing.T) {
	for in, want := range map[string]int64{
		"0B": 0, "512B": 512, "1.5kB": 1500, "12.3MB (virtual 1GB)": 12300000, "2GB": 2000000000,
		"1KiB": 1024, "": -1, "junk": -1,
	} {
		if got := SizeBytes(in); got != want {
			t.Errorf("SizeBytes(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestSortContainers(t *testing.T) {
	cs := []Container{
		{ID: "1", Names: []string{"b"}, State: "exited", Image: "z", Size: "2MB"},
		{ID: "2", Names: []string{"a"}, State: "running", Image: "y", Size: "10MB"},
		{ID: "3", Names: []string{"c"}, State: "paused", Image: "x", Size: "1kB"},
	}
	ids := func() string {
		s := ""
		for _, c := range cs {
			s += c.ID
		}
		return s
	}
	for by, want := range map[string]string{"name": "213", "image": "321", "size": "213", "state": "231"} {
		SortContainers(cs, by)
		if got := ids(); got != want {
			t.Errorf("sort by %s = %s, want %s", by, got, want)
		}
	}
}
