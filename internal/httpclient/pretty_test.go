package httpclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

// prettyRef is the old PrettyJSON (json.Indent), the reference.
func prettyRef(b []byte) ([]byte, bool) {
	t := bytes.TrimSpace(b)
	if len(t) == 0 || (t[0] != '{' && t[0] != '[') {
		return b, false
	}
	var out bytes.Buffer
	if err := json.Indent(&out, t, "", "  "); err != nil {
		return b, false
	}
	return out.Bytes(), true
}

func checkPretty(t *testing.T, in []byte) {
	t.Helper()
	got, gok := PrettyJSON(in)
	want, wok := prettyRef(in)
	if gok != wok || !bytes.Equal(got, want) {
		t.Fatalf("PrettyJSON(%q) = %q, %v; json.Indent gives %q, %v", in, got, gok, want, wok)
	}
}

var prettySeeds = []string{
	`{}`, `[]`, `{ }`, `[ ]`, ` {"a":1} `, "\t[1,2]\n\f", `{"a":{}}`, `{"a":[ ]}`, `[[],{}]`,
	`{"a":1,"b":[true,false,null],"c":"x\"y\\z\u00e9\/"}`, `[-0,0.5,1e10,1E-5,-1.5e+3,123]`,
	`[01]`, `[1.]`, `[.5]`, `[1e]`, `[-]`, `[+1]`, `[1,]`, `{"a":1,}`, `{"a"}`, `{a:1}`, `{"a":1 "b":2}`,
	`[tru]`, `[nul]`, `[falsey]`, `["\x"]`, `["\u12G4"]`, `["a` + "\n" + `b"]`, `["é","` + "\xff\xfe" + `"]`,
	`{"a":1}{"b":2}`, `[1] x`, `[`, `{`, `{"a":`, `["abc`, `[1 2]`, `{"a":1]`, `[1}`, `not json`, ``, `   `,
	`"str"`, `1`, `[1,[2,[3,[4]]]]`, `{"k":"v","o":{"p":[1,{"q":null}]}}`, "[\"\\u0000\"]", "[\"\t\"]",
	strings.Repeat("[", 10000) + strings.Repeat("]", 10000),
	strings.Repeat("[", 10001) + strings.Repeat("]", 10001),
}

func TestPrettyJSONMatchesIndent(t *testing.T) {
	for _, s := range prettySeeds {
		checkPretty(t, []byte(s))
	}
}

// Random documents and random byte changes to them.
func TestPrettyJSONMatchesIndentRandom(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	var gen func(d int) string
	gen = func(d int) string {
		switch k := r.IntN(9); {
		case d > 4 || k < 3:
			return []string{`1`, `-2.5e3`, `"s"`, `"\n\u00e9"`, `true`, `null`, `0`, `""`}[r.IntN(8)]
		case k < 6:
			n := r.IntN(4)
			parts := make([]string, n)
			for i := range parts {
				parts[i] = gen(d + 1)
			}
			return "[" + strings.Join(parts, []string{",", " , ", ",\n"}[r.IntN(3)]) + "]"
		default:
			n := r.IntN(4)
			parts := make([]string, n)
			for i := range parts {
				parts[i] = fmt.Sprintf(`"k%d" : %s`, i, gen(d+1))
			}
			return "{" + strings.Join(parts, ",") + "}"
		}
	}
	for range 5000 {
		doc := []byte("[" + gen(0) + "]")
		checkPretty(t, doc)
		for range 3 {
			m := bytes.Clone(doc)
			switch r.IntN(3) {
			case 0:
				m[r.IntN(len(m))] = "{}[],:\"\\ 01-.eE+tfnu\x00\x1f"[r.IntN(21)]
			case 1:
				i := r.IntN(len(m))
				m = append(m[:i], m[i+1:]...)
			default:
				i := r.IntN(len(m))
				m = append(m[:i], append([]byte{"{}[],:\" 1e-"[r.IntN(11)]}, m[i:]...)...)
			}
			checkPretty(t, m)
		}
	}
}

func FuzzPrettyJSON(f *testing.F) {
	for _, s := range prettySeeds[:len(prettySeeds)-2] {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) { checkPretty(t, b) })
}

// PERF-8: PrettyJSON takes at most half of json.Indent's time (best of
// 5 each, measured back to back, so a busy machine slows both).
func TestPrettyJSONTwiceAsFastAsIndent(t *testing.T) {
	if raceEnabled || testing.Short() {
		t.Skip("timing")
	}
	var sb strings.Builder
	sb.WriteString("[")
	for i := 0; sb.Len() < 2<<20; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `{"id":%d,"name":"user %d","tags":["a","b"],"o":{"c":"x","n":-1.5e3}}`, i, i)
	}
	sb.WriteString("]")
	data := []byte(sb.String())
	best := func(f func()) time.Duration {
		d := time.Duration(1<<63 - 1)
		for range 5 {
			s := time.Now()
			f()
			d = min(d, time.Since(s))
		}
		return d
	}
	ref := best(func() { prettyRef(data) })
	got := best(func() { PrettyJSON(data) })
	if got > ref/2 {
		t.Errorf("PrettyJSON %v, json.Indent %v: want at most half", got, ref)
	}
}
