package source

import (
	"errors"
	"strings"
	"testing"
)

func collect(t *testing.T, s Shape, docs ...string) Result {
	t.Helper()
	c, err := newCollector(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range docs {
		if err := c.addDoc([]byte(d)); err != nil {
			t.Fatal(err)
		}
	}
	return c.res
}

func TestKeyFromPathAndHash(t *testing.T) {
	r := collect(t, Shape{Items: "$.issues", Key: "$.id"},
		`{"issues":[{"id":"LIN-228"},{"id":42},{"id":"has space"},{"x":1},{"id":"`+strings.Repeat("a", 200)+`"}]}`)
	if len(r.Items) != 5 {
		t.Fatalf("items: %d", len(r.Items))
	}
	if r.Items[0].Key != "LIN-228" || r.Items[1].Key != "42" {
		t.Errorf("plain keys: %q %q", r.Items[0].Key, r.Items[1].Key)
	}
	for _, i := range []int{2, 3, 4} {
		if !strings.HasPrefix(r.Items[i].Key, "sha256:") || len(r.Items[i].Key) != 7+32 {
			t.Errorf("item %d: want a hash key, got %q", i, r.Items[i].Key)
		}
	}
}

func TestHashKeyIsStable(t *testing.T) {
	a := collect(t, Shape{}, `{"b":1,"a":[1, 2]}`)
	b := collect(t, Shape{}, "{ \"a\": [1,2],\n \"b\": 1 }")
	if a.Items[0].Key != b.Items[0].Key {
		t.Errorf("same item, other formatting: %q vs %q", a.Items[0].Key, b.Items[0].Key)
	}
	c := collect(t, Shape{}, `{"b":2,"a":[1,2]}`)
	if c.Items[0].Key == a.Items[0].Key {
		t.Error("other item, same key")
	}
	if string(b.Items[0].Data) != `{"a":[1,2],"b":1}` {
		t.Errorf("data not compact: %s", b.Items[0].Data)
	}
}

func TestMaxItemsCap(t *testing.T) {
	r := collect(t, Shape{MaxItems: 2}, `[1,2,3,4]`)
	if len(r.Items) != 2 || !r.Truncated || r.Total != 4 {
		t.Errorf("got %d items, truncated %v, total %d", len(r.Items), r.Truncated, r.Total)
	}
	r = collect(t, Shape{}, "["+strings.TrimSuffix(strings.Repeat("1,", 150), ",")+"]")
	if len(r.Items) != DefaultMaxItems || !r.Truncated {
		t.Errorf("default cap: %d", len(r.Items))
	}
	for _, n := range []int{-1, HardMaxItems + 1} {
		if _, err := newCollector(Shape{MaxItems: n}); err == nil {
			t.Errorf("max_items %d: want an error", n)
		}
	}
}

func TestItemSizeCap(t *testing.T) {
	c, _ := newCollector(Shape{})
	big := `["` + strings.Repeat("x", MaxItemBytes) + `"]`
	err := c.addDoc([]byte(big))
	if !errors.Is(err, ErrTooBig) {
		t.Fatalf("want ErrTooBig, got %v", err)
	}
	ok := `["` + strings.Repeat("x", MaxItemBytes-2) + `"]`
	c, _ = newCollector(Shape{})
	if err := c.addDoc([]byte(ok)); err != nil {
		t.Fatalf("exactly 64 KB: %v", err)
	}
}

func TestBadShapeRefusedEarly(t *testing.T) {
	if _, err := newCollector(Shape{Items: "issues"}); err == nil {
		t.Error("items path without $: want an error")
	}
	if _, err := newCollector(Shape{Key: "$.["}); err == nil {
		t.Error("bad key path: want an error")
	}
}
