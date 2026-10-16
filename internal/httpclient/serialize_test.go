package httpclient

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the testdata goldens")

// TestSerializeDartCases ports the serializeHttpFile group of
// mobocode's rest_parser_test.dart, asserting the exact bytes the Dart
// code writes (StringBuffer.writeln = "\n").
func TestSerializeDartCases(t *testing.T) {
	cases := []struct {
		name string
		in   []Request
		want string
	}{
		{"GET", []Request{{Method: "GET", URL: "https://example.com"}},
			"GET https://example.com\n"},
		{"headers", []Request{{Method: "GET", URL: "https://example.com", Headers: hs("Accept", "application/json")}},
			"GET https://example.com\nAccept: application/json\n"},
		{"body with blank-line separator", []Request{{Method: "POST", URL: "https://example.com", Body: sp(`{"key":"val"}`)}},
			"POST https://example.com\n\n{\"key\":\"val\"}\n"},
		{"### between requests", []Request{{Method: "GET", URL: "https://a.com"}, {Method: "GET", URL: "https://b.com"}},
			"GET https://a.com\n###\nGET https://b.com\n"},
		{"named title", []Request{{Method: "GET", URL: "https://a.com", Name: "List Users"}},
			"### List Users\nGET https://a.com\n"},
		{"named second request", []Request{{Method: "GET", URL: "https://a.com"}, {Method: "GET", URL: "https://b.com", Name: "B"}},
			"GET https://a.com\n###\n### B\nGET https://b.com\n"},
		{"body ending in newline", []Request{{Method: "POST", URL: "https://a.com", Body: sp("x\n")}},
			"POST https://a.com\n\nx\n"},
		{"empty body not written", []Request{{Method: "POST", URL: "https://a.com", Body: sp("")}},
			"POST https://a.com\n"},
		{"empty list", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Serialize(c.in); got != c.want {
				t.Fatalf("Serialize\n got  %q\n want %q", got, c.want)
			}
		})
	}
}

// TestRoundTripDart is the Dart "round-trips" case.
func TestRoundTripDart(t *testing.T) {
	src := "### Get user\nGET https://api.example.com/users/1\nAccept: application/json\n\n###\n### Create user\nPOST https://api.example.com/users\nContent-Type: application/json\n\n{\"name\":\"Alice\"}\n"
	a := Parse(src)
	b := Parse(Serialize(a.Requests))
	if !reflect.DeepEqual(viewOf(a.Requests), viewOf(b.Requests)) {
		t.Fatalf("round trip differs:\n%#v\n%#v", viewOf(a.Requests), viewOf(b.Requests))
	}
}

// TestFixtures checks testdata/*.http against *.golden.json (parse)
// and *.serialized.http (serialize). The same files can be copied into
// mobocode's test/ folder as shared goldens. Run with -update to
// rewrite them.
func TestFixtures(t *testing.T) {
	files, _ := filepath.Glob("testdata/*.http")
	var srcs []string
	for _, f := range files {
		if !strings.HasSuffix(f, ".serialized.http") {
			srcs = append(srcs, f)
		}
	}
	if len(srcs) < 3 {
		t.Fatalf("want at least 3 fixtures, got %v", srcs)
	}
	for _, path := range srcs {
		t.Run(filepath.Base(path), func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			f := Parse(string(data))
			js, _ := json.MarshalIndent(f, "", "  ")
			js = append(js, '\n')
			ser := Serialize(f.Requests)
			base := strings.TrimSuffix(path, ".http")
			if *update {
				_ = os.WriteFile(base+".golden.json", js, 0o644)
				_ = os.WriteFile(base+".serialized.http", []byte(ser), 0o644)
			}
			wantJS, _ := os.ReadFile(base + ".golden.json")
			if string(wantJS) != string(js) {
				t.Fatalf("parse golden differs for %s:\n%s", path, js)
			}
			wantSer, _ := os.ReadFile(base + ".serialized.http")
			if string(wantSer) != ser {
				t.Fatalf("serialize golden differs for %s:\n%s", path, ser)
			}
		})
	}
}

// FuzzRoundTrip: parse → serialize → parse gives the same requests.
// The skips are inputs where mobocode's Dart grammar loses the same
// information (a comment-looking request line, a URL that still ends in
// " HTTP/x" after one strip, a CR inside the body).
func FuzzRoundTrip(f *testing.F) {
	files, _ := filepath.Glob("testdata/*.http")
	for _, p := range files {
		if b, err := os.ReadFile(p); err == nil {
			f.Add(string(b))
		}
	}
	f.Add("GET https://a.com\n")
	f.Fuzz(func(t *testing.T, src string) {
		a := Parse(src)
		for _, r := range a.Requests {
			// A request line that only parses because of leading spaces
			// ("  #x y") reads as a comment once written; same in Dart.
			if isComment(r.Method) || httpSuffixRe.MatchString(r.URL) || strings.Contains(r.BodyText(), "\r") {
				t.Skip()
			}
			for _, h := range r.Headers {
				// A header name that itself reads as a comment cannot
				// survive a round trip in the Dart grammar either.
				if isComment(h.Name) {
					t.Skip()
				}
			}
		}
		b := Parse(Serialize(a.Requests))
		if !reflect.DeepEqual(viewOf(a.Requests), viewOf(b.Requests)) {
			t.Fatalf("round trip differs for %q:\n%#v\n%#v", src, viewOf(a.Requests), viewOf(b.Requests))
		}
	})
}

func BenchmarkParse2000(b *testing.B) {
	var sb strings.Builder
	for i := 0; i < 2000; i++ {
		sb.WriteString("### req\n# @name r\nPOST {{host}}/users/1\nContent-Type: application/json\nAuthorization: Bearer {{token}}\n\n{\"name\": \"x\"}\n\n")
	}
	src := sb.String()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if n := len(Parse(src).Requests); n != 2000 {
			b.Fatal(n)
		}
	}
}
