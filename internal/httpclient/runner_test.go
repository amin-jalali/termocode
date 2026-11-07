package httpclient

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// runServer answers /ok with 200, /fail with 500 and records the paths.
func runServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.RequestURI())
		mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/fail") {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func runFile(path, src string) RunFile { return RunFile{Path: path, File: Parse(src)} }

func TestRunSendsEveryRequestAndCounts(t *testing.T) {
	srv, seen := runServer(t)
	src := "@base = " + srv.URL + "\n\n### one\nGET {{base}}/ok\n\n### two\nGET {{base}}/fail\n"
	rep := Run(context.Background(), New(Options{}), []RunFile{runFile("a.http", src)}, RunOptions{})
	if rep.Passed != 1 || rep.Failed != 1 || len(rep.Results) != 2 {
		t.Fatalf("report = %+v", rep)
	}
	if got := seen(); len(got) != 2 || got[0] != "/ok" || got[1] != "/fail" {
		t.Fatalf("seen = %v", got)
	}
	r := rep.Results[1]
	if r.Passed || r.Status != 500 || r.Name != "two" || r.File != "a.http" || r.Failure == "" {
		t.Fatalf("result = %+v", r)
	}
	// The report keeps the template URL, never the resolved one.
	if r.URL != "{{base}}/fail" {
		t.Fatalf("URL = %q", r.URL)
	}
}

func TestRunStopOnFail(t *testing.T) {
	srv, seen := runServer(t)
	src := "GET " + srv.URL + "/fail\n\n###\nGET " + srv.URL + "/ok\n"
	rep := Run(context.Background(), New(Options{}), []RunFile{runFile("a.http", src)}, RunOptions{StopOnFail: true})
	if !rep.Stopped || len(rep.Results) != 1 || len(seen()) != 1 {
		t.Fatalf("report = %+v seen = %v", rep, seen())
	}
}

func TestRunIterationsWithDataRows(t *testing.T) {
	srv, seen := runServer(t)
	src := "@id = file\nGET " + srv.URL + "/ok?id={{id}}\n"
	data := []map[string]string{{"id": "a"}, {"id": "b"}}
	rep := Run(context.Background(), New(Options{}), []RunFile{runFile("a.http", src)}, RunOptions{Data: data})
	if got := seen(); len(got) != 2 || got[0] != "/ok?id=a" || got[1] != "/ok?id=b" {
		t.Fatalf("seen = %v", got)
	}
	if rep.Results[0].Iteration != 1 || rep.Results[1].Iteration != 2 {
		t.Fatalf("iterations = %+v", rep.Results)
	}
	// More iterations than rows: the rows repeat.
	Run(context.Background(), New(Options{}), []RunFile{runFile("a.http", src)}, RunOptions{Data: data, Iterations: 3})
	if got := seen(); len(got) != 5 || got[4] != "/ok?id=a" {
		t.Fatalf("seen = %v", got)
	}
}

func TestRunUnresolvedPlaceholderFailsWithoutSending(t *testing.T) {
	srv, seen := runServer(t)
	src := "GET " + srv.URL + "/ok?t={{token}}\n"
	rep := Run(context.Background(), New(Options{}), []RunFile{runFile("a.http", src)}, RunOptions{})
	if rep.Failed != 1 || len(seen()) != 0 || !strings.Contains(rep.Results[0].Failure, "token") {
		t.Fatalf("report = %+v seen = %v", rep, seen())
	}
}

func TestRunHidesSecretsInFailure(t *testing.T) {
	secret := "sk-" + "test-runner-123"
	src := "GET http://{{host}}/x\n"
	rep := Run(context.Background(), New(Options{Timeout: time.Second}), []RunFile{runFile("a.http", src)}, RunOptions{
		NewResolver: func(rf RunFile) *Resolver {
			return &Resolver{FileVars: rf.File.Vars, Secret: func(string) (string, bool) { return secret, true }}
		},
		Data: []map[string]string{{"host": "{{$secret H}}.invalid"}},
	})
	b, _ := rep.JSON()
	j, _ := rep.JUnit()
	for _, s := range []string{rep.Text(), string(b), string(j)} {
		if strings.Contains(s, secret) {
			t.Fatalf("secret leaked: %s", s)
		}
	}
	if rep.Failed != 1 {
		t.Fatalf("report = %+v", rep)
	}
}

func TestRunDelayAndCancel(t *testing.T) {
	srv, seen := runServer(t)
	src := "GET " + srv.URL + "/ok\n\n###\nGET " + srv.URL + "/ok\n"
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	rep := Run(ctx, New(Options{}), []RunFile{runFile("a.http", src)}, RunOptions{Delay: 10 * time.Second})
	if time.Since(start) > 5*time.Second || len(seen()) != 1 || !rep.Stopped {
		t.Fatalf("report = %+v seen = %v", rep, seen())
	}
}

func TestRunReportJSONAndJUnit(t *testing.T) {
	srv, _ := runServer(t)
	src := "### ok\nGET " + srv.URL + "/ok\n\n### bad\nGET " + srv.URL + "/fail\n"
	rep := Run(context.Background(), New(Options{}), []RunFile{runFile("api/a.http", src)}, RunOptions{})
	b, err := rep.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		Passed, Failed int
		Results        []struct{ Name string }
	}
	if err := json.Unmarshal(b, &back); err != nil || back.Passed != 1 || back.Failed != 1 || len(back.Results) != 2 {
		t.Fatalf("json = %s (%v)", b, err)
	}
	j, err := rep.JUnit()
	if err != nil {
		t.Fatal(err)
	}
	var suites struct {
		Tests    int `xml:"tests,attr"`
		Failures int `xml:"failures,attr"`
		Suites   []struct {
			Name  string `xml:"name,attr"`
			Cases []struct {
				Name    string    `xml:"name,attr"`
				Failure *struct{} `xml:"failure"`
			} `xml:"testcase"`
		} `xml:"testsuite"`
	}
	if err := xml.Unmarshal(j, &suites); err != nil {
		t.Fatalf("junit = %s (%v)", j, err)
	}
	if suites.Tests != 2 || suites.Failures != 1 || len(suites.Suites) != 1 || suites.Suites[0].Name != "api/a.http" {
		t.Fatalf("junit = %s", j)
	}
	if c := suites.Suites[0].Cases; len(c) != 2 || c[0].Failure != nil || c[1].Failure == nil || c[1].Name != "bad" {
		t.Fatalf("cases = %+v", c)
	}
	if txt := rep.Text(); !strings.Contains(txt, "1 passed, 1 failed") || !strings.Contains(txt, "bad") {
		t.Fatalf("text = %q", txt)
	}
}

func TestLoadRunDataCSVAndJSON(t *testing.T) {
	dir := t.TempDir()
	csvPath := filepath.Join(dir, "d.csv")
	os.WriteFile(csvPath, []byte("id,name\n1,Ada\n2,\"Lin, B\"\n"), 0o600)
	rows, err := LoadRunData(csvPath)
	if err != nil || len(rows) != 2 || rows[1]["name"] != "Lin, B" || rows[0]["id"] != "1" {
		t.Fatalf("csv rows = %v (%v)", rows, err)
	}
	jsonPath := filepath.Join(dir, "d.json")
	os.WriteFile(jsonPath, []byte(`[{"id":1,"ok":true,"name":"Ada"},{"id":2}]`), 0o600)
	rows, err = LoadRunData(jsonPath)
	if err != nil || len(rows) != 2 || rows[0]["id"] != "1" || rows[0]["ok"] != "true" || rows[1]["id"] != "2" {
		t.Fatalf("json rows = %v (%v)", rows, err)
	}
	bad := filepath.Join(dir, "d.json5")
	os.WriteFile(bad, []byte(`{}`), 0o600)
	if _, err := LoadRunData(bad); err == nil {
		t.Fatal("unknown extension should fail")
	}
}

func TestFindHTTPFilesWalksFolders(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"b.http", "a/x.rest", "a/y.txt", "node_modules/z.http", ".git/w.http"} {
		full := filepath.Join(dir, p)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte("GET http://x\n"), 0o600)
	}
	got, err := FindHTTPFiles([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "a/x.rest"), filepath.Join(dir, "b.http")}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v want %v", got, want)
	}
	if _, err := FindHTTPFiles([]string{filepath.Join(dir, "missing.http")}); err == nil {
		t.Fatal("missing path should fail")
	}
}
