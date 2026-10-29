package clipring

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestNewestFirstAndDropsNoise(t *testing.T) {
	r := New()
	r.Push("one")
	r.Push("two")
	r.Push("two")                           // same as the newest: dropped
	r.Push("")                              // empty: dropped
	r.Push(strings.Repeat("x", maxBytes+1)) // too big: dropped
	r.Push("one")                           // older copy is fine again
	got := r.Items()
	want := []string{"one", "two", "one"}
	if len(got) != len(want) || r.Len() != len(want) {
		t.Fatalf("got %d entries (Len %d), want %d", len(got), r.Len(), len(want))
	}
	for i, w := range want {
		if got[i].Content != w {
			t.Errorf("entry %d = %q, want %q", i, got[i].Content, w)
		}
		if got[i].CapturedAt.IsZero() {
			t.Errorf("entry %d has no capture time", i)
		}
	}
	r.Push(strings.Repeat("y", maxBytes)) // exactly the limit is kept
	if r.Len() != 4 {
		t.Errorf("a 64 KB entry was dropped")
	}
}

func TestKeepsOnlyTheNewestTwenty(t *testing.T) {
	r := New()
	for i := 0; i < 50; i++ {
		r.Push(fmt.Sprint("clip ", i))
	}
	got := r.Items()
	if len(got) != defaultCap {
		t.Fatalf("%d entries, want %d", len(got), defaultCap)
	}
	if got[0].Content != "clip 49" || got[defaultCap-1].Content != "clip 30" {
		t.Errorf("kept %q … %q, want clip 49 … clip 30", got[0].Content, got[defaultCap-1].Content)
	}
}

func TestItemsIsACopy(t *testing.T) {
	r := New()
	r.Push("a")
	items := r.Items()
	items[0].Content = "changed"
	if r.Items()[0].Content != "a" {
		t.Error("changing the snapshot changed the ring")
	}
}

func TestPushFromManyGoroutines(t *testing.T) {
	r := New()
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				r.Push(fmt.Sprint(g, "-", i))
				_ = r.Items()
			}
		}(g)
	}
	wg.Wait()
	if r.Len() != defaultCap {
		t.Errorf("Len = %d, want %d", r.Len(), defaultCap)
	}
}
