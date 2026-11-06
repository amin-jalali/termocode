package shotkit

import (
	"strconv"
	"testing"
)

// CI splits the golden screens over parallel jobs with
// SHOTKIT_GOLDEN_SHARD=i/n: every case runs in exactly one shard.
func TestGoldenShardSplitsEveryCaseOnce(t *testing.T) {
	const cases = 541
	for _, n := range []int{1, 2, 3, 4, 7} {
		seen := make([]int, cases)
		for i := 1; i <= n; i++ {
			t.Setenv("SHOTKIT_GOLDEN_SHARD", strconv.Itoa(i)+"/"+strconv.Itoa(n))
			keep := goldenShard()
			for idx := range cases {
				if keep(idx) {
					seen[idx]++
				}
			}
		}
		for idx, s := range seen {
			if s != 1 {
				t.Fatalf("n=%d: case %d ran in %d shards, want 1", n, idx, s)
			}
		}
	}
}

func TestGoldenShardUnsetOrBadRunsAll(t *testing.T) {
	for _, v := range []string{"", "x", "0/3", "4/3", "1/0", "2"} {
		t.Setenv("SHOTKIT_GOLDEN_SHARD", v)
		keep := goldenShard()
		for idx := range 10 {
			if !keep(idx) {
				t.Fatalf("SHOTKIT_GOLDEN_SHARD=%q skipped case %d", v, idx)
			}
		}
	}
}
