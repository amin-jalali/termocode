package containers

import (
	"reflect"
	"testing"
)

func TestParseDiff(t *testing.T) {
	got := ParseDiff("C /etc\nA /etc/app.conf\n\nD /tmp/old\nX weird\n")
	want := []DiffEntry{{Kind: "changed", Path: "/etc"}, {Kind: "added", Path: "/etc/app.conf"}, {Kind: "deleted", Path: "/tmp/old"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseDiff = %+v", got)
	}
}

func TestLayerSizesLargestFirst(t *testing.T) {
	h := []HistoryEntry{
		{ID: "a", CreatedBy: "COPY app /app", Size: "30MB"},
		{ID: "b", CreatedBy: "ENV X=1", Size: "0B"},
		{ID: "c", CreatedBy: "RUN apt-get install", Size: "70MB"},
	}
	r := LayerSizes(h)
	if r.Total != 100e6 || len(r.Layers) != 2 {
		t.Fatalf("report = %+v", r)
	}
	if r.Layers[0].CreatedBy != "RUN apt-get install" || r.Layers[0].Percent != 70 || r.Layers[1].Percent != 30 {
		t.Fatalf("order/percent = %+v", r.Layers)
	}
	if r.Empty != 1 {
		t.Fatalf("empty layers = %d", r.Empty)
	}
}

func TestImageUsedBy(t *testing.T) {
	img := Image{ID: "sha256:abc123", RepoTags: []string{"web:1"}}
	cs := []Container{
		{ID: "1", Image: "web:1"},
		{ID: "2", ImageID: "sha256:abc123", Image: "other"},
		{ID: "3", Image: "abc123"},
		{ID: "4", Image: "db:1"},
	}
	var ids []string
	for _, c := range ImageUsedBy(img, cs) {
		ids = append(ids, c.ID)
	}
	if !reflect.DeepEqual(ids, []string{"1", "2", "3"}) {
		t.Fatalf("used by = %v", ids)
	}
}

func TestArchiveArgs(t *testing.T) {
	cases := []struct {
		got  func() ([]string, error)
		want []string
	}{
		{func() ([]string, error) { return SaveArgs("/tmp/a.tar", "web:1", "db:1") }, []string{"save", "-o", "/tmp/a.tar", "web:1", "db:1"}},
		{func() ([]string, error) { return LoadArgs("/tmp/a.tar") }, []string{"load", "-i", "/tmp/a.tar"}},
		{func() ([]string, error) { return ImportArgs("/tmp/fs.tar", "mine:1") }, []string{"import", "/tmp/fs.tar", "mine:1"}},
		{func() ([]string, error) { return CommitArgs("c1", "snap:1", "before upgrade") }, []string{"commit", "-m", "before upgrade", "c1", "snap:1"}},
		{func() ([]string, error) { return ExportArgs("c1", "/tmp/c1.tar") }, []string{"export", "-o", "/tmp/c1.tar", "c1"}},
		{func() ([]string, error) { return DiffArgs("c1") }, []string{"diff", "c1"}},
	}
	for i, c := range cases {
		got, err := c.got()
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%d: %v %v, want %v", i, got, err, c.want)
		}
	}
}

func TestArchiveArgsRejectFlags(t *testing.T) {
	if _, err := SaveArgs("/tmp/a.tar", "--output=/etc/x"); err == nil {
		t.Fatal("a ref that looks like a flag must fail")
	}
	if _, err := SaveArgs("/tmp/a.tar"); err == nil {
		t.Fatal("save needs a ref")
	}
	if _, err := ExportArgs("-x", "/tmp/a.tar"); err == nil {
		t.Fatal("flag id must fail")
	}
	if _, err := LoadArgs(""); err == nil {
		t.Fatal("empty path must fail")
	}
	if _, err := CommitArgs("c1", "", ""); err != nil {
		t.Fatal("commit without a ref or message is fine")
	}
}
