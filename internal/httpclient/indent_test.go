package httpclient

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestIndentPrefixEqualsJSONIndent(t *testing.T) {
	cases := []string{
		`{"a":1,"b":[1,2,{"c":"x,y:z"}],"d":{},"e":[],"f":"q\"uote","g":null,"h":true}`,
		`[ 1 , 2 , [ ] , { } ]`,
		` {"nested":{"deeper":{"x":[{"y":"}"},"]"]}}} `,
		`{"esc":"a\\\"b\\\\","u":"é"}`,
		`[]`, `{}`,
	}
	for _, c := range cases {
		var want bytes.Buffer
		if err := json.Indent(&want, []byte(strings.TrimSpace(c)), "", "  "); err != nil {
			t.Fatalf("%q: %v", c, err)
		}
		got, complete, ok := IndentPrefix([]byte(c), 1<<20)
		if !ok || !complete || string(got) != want.String() {
			t.Fatalf("%q\n got  %q (complete=%v ok=%v)\n want %q", c, got, complete, ok, want.String())
		}
	}
	if _, _, ok := IndentPrefix([]byte("hello"), 100); ok {
		t.Fatal("not JSON")
	}
}

func TestIndentPrefixStopsEarly(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("[")
	for i := 0; sb.Len() < 5<<20; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"id":1,"name":"user","tags":["a","b"]}`)
	}
	sb.WriteString("]")
	data := []byte(sb.String())
	out, complete, ok := IndentPrefix(data, 32<<10)
	if !ok || complete || len(out) != 32<<10 {
		t.Fatalf("len %d complete %v ok %v", len(out), complete, ok)
	}
	full, _ := PrettyJSON(data)
	if !bytes.HasPrefix(full, out) {
		t.Fatal("the prefix must equal the start of the full indent")
	}
	// Speed: not a fixed number of milliseconds (a loaded CI runner or the
	// race detector made 5 ms fail at 6.7 ms) but a ratio against the work
	// it must avoid, measured in the same test on the same machine: the
	// prefix of 32 KB has to be at least 10 times faster than indenting
	// all 5 MB (it is about 150 times faster). Both are the best of a few
	// runs, so one scheduler hiccup does not decide. A prefix that stopped
	// stopping early would be as slow as the full indent and fail.
	best := func(n int, f func()) time.Duration {
		d := time.Duration(1<<63 - 1)
		for i := 0; i < n; i++ {
			start := time.Now()
			f()
			if took := time.Since(start); took < d {
				d = took
			}
		}
		return d
	}
	prefix := best(7, func() { IndentPrefix(data, 32<<10) })
	whole := best(3, func() { PrettyJSON(data) })
	if prefix*10 > whole {
		t.Errorf("32 KB of a 5 MB body took %v, the full indent %v (the prefix must be 10x faster)", prefix, whole)
	}
}
