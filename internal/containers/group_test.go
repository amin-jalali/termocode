package containers

import (
	"reflect"
	"testing"
)

// PERF-5: GroupIndexes is GroupByProject by index: the same groups in
// the same order, each listing the positions of its containers.
func TestGroupIndexesMatchGroupByProject(t *testing.T) {
	rows, err := ParseContainers(bigPS(100))
	if err != nil {
		t.Fatal(err)
	}
	rows = append(rows,
		Container{ID: "u1", Names: []string{"loose"}},
		Container{ID: "p1", Labels: map[string]string{"io.podman.compose.project": "pod", "io.podman.compose.project.working_dir": "/w/pod"}},
		Container{ID: "o2", Names: []string{"other-too"}},
		Container{ID: "w1", Project: "proj7", WorkingDir: "/w/seven"},
	)
	for _, current := range []string{"", "proj3", "pod", "other", "missing"} {
		want := GroupByProject(rows, current)
		got := GroupIndexes(rows, current)
		if len(got) != len(want) {
			t.Fatalf("current %q: %d groups, want %d", current, len(got), len(want))
		}
		for i, g := range got {
			w := want[i]
			if g.Name != w.Name || g.WorkingDir != w.WorkingDir || g.IsOther != w.IsOther {
				t.Fatalf("current %q group %d: %+v, want %s %s %v", current, i, g, w.Name, w.WorkingDir, w.IsOther)
			}
			var cs []Container
			for _, idx := range g.Idx {
				cs = append(cs, rows[idx])
			}
			if !reflect.DeepEqual(cs, w.Containers) {
				t.Fatalf("current %q group %s: other containers", current, g.Name)
			}
		}
	}
}

func BenchmarkGroupByProject100(b *testing.B) {
	rows, _ := ParseContainers(bigPS(100))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = GroupByProject(rows, "proj3")
	}
}

func BenchmarkGroupIndexes100(b *testing.B) {
	rows, _ := ParseContainers(bigPS(100))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = GroupIndexes(rows, "proj3")
	}
}
