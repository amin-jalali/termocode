package worktrees

import "testing"

// CMP-31: the seen comment ids have no doubles, keep the newest past the
// cap and survive a restart; the sent run is kept.
func TestMarkFollowSeen(t *testing.T) {
	var m Meta
	m.MarkFollowSeen([]int64{1, 2, 2, 3}, 30)
	m.MarkFollowSeen([]int64{3, 4}, 0)
	if len(m.FollowSeen) != 4 || m.FollowSeen[3] != 4 || m.FollowRun != 30 {
		t.Fatalf("meta %+v", m)
	}
	set := m.FollowSeenSet()
	if !set[1] || !set[4] || set[5] {
		t.Errorf("set %v", set)
	}
	big := make([]int64, MaxFollowSeen+10)
	for i := range big {
		big[i] = int64(100 + i)
	}
	m.MarkFollowSeen(big, 0)
	if len(m.FollowSeen) != MaxFollowSeen || m.FollowSeen[len(m.FollowSeen)-1] != int64(100+MaxFollowSeen+9) {
		t.Errorf("cap: len %d last %d", len(m.FollowSeen), m.FollowSeen[len(m.FollowSeen)-1])
	}
	common := t.TempDir()
	if err := Update(common, func(all map[string]Meta) {
		md := all["/w"]
		md.Follow = true
		md.MarkFollowSeen([]int64{7}, 31)
		all["/w"] = md
	}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(common)
	if err != nil {
		t.Fatal(err)
	}
	if md := got["/w"]; !md.Follow || len(md.FollowSeen) != 1 || md.FollowRun != 31 {
		t.Errorf("after load %+v", md)
	}
}
