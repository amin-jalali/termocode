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
	start := time.Now()
	out, complete, ok := IndentPrefix(data, 32<<10)
	took := time.Since(start)
	if !ok || complete || len(out) != 32<<10 {
		t.Fatalf("len %d complete %v ok %v", len(out), complete, ok)
	}
	full, _ := PrettyJSON(data)
	if !bytes.HasPrefix(full, out) {
		t.Fatal("the prefix must equal the start of the full indent")
	}
	if took > 5*time.Millisecond {
		t.Errorf("32 KB of a 5 MB body took %v (budget 5 ms)", took)
	}
}
