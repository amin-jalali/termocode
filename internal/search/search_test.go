package search

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseJSON_Match feeds ripgrep's JSON event format through parseJSON and
// asserts we recover the path, line, col, preview text, and submatch ranges
// in the shape the renderer expects.
func TestParseJSON_Match(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"begin","data":{"path":{"text":"foo.go"}}}`,
		`{"type":"match","data":{"path":{"text":"foo.go"},"lines":{"text":"hello world hello\n"},"line_number":3,"absolute_offset":0,"submatches":[{"match":{"text":"hello"},"start":0,"end":5},{"match":{"text":"hello"},"start":12,"end":17}]}}`,
		`{"type":"end","data":{"path":{"text":"foo.go"}}}`,
	}, "\n")

	results, err := parseJSON([]byte(stream))
	if err != nil {
		t.Fatalf("parseJSON: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Path != "foo.go" {
		t.Errorf("path: got %q want foo.go", r.Path)
	}
	if r.Line != 3 {
		t.Errorf("line: got %d want 3", r.Line)
	}
	if r.Col != 1 {
		t.Errorf("col: got %d want 1", r.Col)
	}
	if r.Preview != "hello world hello" {
		t.Errorf("preview: got %q want %q", r.Preview, "hello world hello")
	}
	if len(r.Matches) != 2 {
		t.Fatalf("matches: got %d want 2", len(r.Matches))
	}
	if r.Matches[0] != (MatchRange{Start: 0, End: 5}) {
		t.Errorf("match[0] = %+v", r.Matches[0])
	}
	if r.Matches[1] != (MatchRange{Start: 12, End: 17}) {
		t.Errorf("match[1] = %+v", r.Matches[1])
	}
}

// TestParseJSON_NonMatchEventsIgnored ensures we don't emit Result rows for
// "begin", "end", "summary", or malformed envelopes — only "match" events.
func TestParseJSON_NonMatchEventsIgnored(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"begin","data":{"path":{"text":"a.go"}}}`,
		`{"type":"end","data":{"path":{"text":"a.go"}}}`,
		`{"type":"summary","data":{"elapsed_total":{"human":"0.01s","nanos":12345,"secs":0}}}`,
		`not json`,
		``,
	}, "\n")
	results, err := parseJSON([]byte(stream))
	if err != nil {
		t.Fatalf("parseJSON: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("want 0 results, got %d", len(results))
	}
}

// TestRun_EmptyQuery short-circuits before invoking rg.
func TestRun_EmptyQuery(t *testing.T) {
	results, err := Run(".", "   ")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if results != nil {
		t.Errorf("want nil results, got %v", results)
	}
}

// TestRun_LiveRipgrep exercises Run end-to-end against a sandbox tmpdir.
// Skipped if rg isn't on PATH so the suite stays portable.
func TestRunFallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample.txt"),
		[]byte("alpha beta gamma\nbeta delta beta\nepsilon\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A pruned directory and a binary file must be ignored.
	if err := os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "x.txt"), []byte("beta\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin.dat"), []byte("beta\x00beta"), 0o644); err != nil {
		t.Fatal(err)
	}

	results := fallback(t, dir, "beta", Options{})
	if len(results) != 2 {
		t.Fatalf("want 2 matching lines (binary + node_modules skipped), got %d (%+v)", len(results), results)
	}
	// Line 2 has two occurrences of "beta" → two highlight ranges.
	var line2 *Result
	for i := range results {
		if results[i].Line == 2 {
			line2 = &results[i]
		}
	}
	if line2 == nil {
		t.Fatalf("expected a match on line 2, got %+v", results)
	}
	if len(line2.Matches) != 2 {
		t.Errorf("line 2 should have 2 match ranges, got %d", len(line2.Matches))
	}
}

func TestRunFallback_SmartCase(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("Beta\nbeta\nBETA\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Lowercase query → case-insensitive: all three lines match.
	lower := fallback(t, dir, "beta", Options{})
	if len(lower) != 3 {
		t.Errorf("smart-case lowercase: want 3, got %d", len(lower))
	}
	// Mixed-case query → case-sensitive: only the exact "Beta" matches.
	upper := fallback(t, dir, "Beta", Options{})
	if len(upper) != 1 {
		t.Errorf("smart-case mixed: want 1, got %d", len(upper))
	}
}

func TestRun_LiveRipgrep(t *testing.T) {
	if !Available() {
		t.Skip("ripgrep not installed")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")
	body := "alpha beta gamma\nbeta delta\nepsilon\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	results, err := Run(dir, "beta")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 matches, got %d (%+v)", len(results), results)
	}
	for _, r := range results {
		if r.Path != "sample.txt" {
			t.Errorf("path: got %q", r.Path)
		}
		if len(r.Matches) == 0 {
			t.Errorf("expected at least one match range, got none on %+v", r)
		}
	}
}

// fallback runs the pure-Go walker over dir and collects every result.
func fallback(t *testing.T, dir, query string, opts Options) []Result {
	t.Helper()
	var out []Result
	if err := runFallback(context.Background(), dir, query, opts, func(rs []Result) {
		out = append(out, rs...)
	}); err != nil {
		t.Fatalf("runFallback(%q): %v", query, err)
	}
	return out
}

func writeFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunFallback_CaseToggle(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "f.txt", "beta\nBeta\n")
	if got := fallback(t, dir, "beta", Options{CaseSensitive: true}); len(got) != 1 || got[0].Line != 1 {
		t.Errorf("case-sensitive lowercase query: want only line 1, got %+v", got)
	}
}

func TestRunFallback_WholeWord(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "f.txt", "foo\nfoobar\nbar_foo\nx foo-y\n")
	got := fallback(t, dir, "foo", Options{WholeWord: true})
	var lines []int
	for _, r := range got {
		lines = append(lines, r.Line)
	}
	if len(lines) != 2 || lines[0] != 1 || lines[1] != 4 {
		t.Errorf("whole word: want lines [1 4], got %v", lines)
	}
}

func TestRunFallback_Regex(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "f.txt", "a1b\na.b\nab\n")
	// Literal: "." only matches a real dot.
	if got := fallback(t, dir, "a.b", Options{}); len(got) != 1 || got[0].Line != 2 {
		t.Errorf("literal: want line 2 only, got %+v", got)
	}
	// Regex: "." matches any char.
	if got := fallback(t, dir, "a.b", Options{Regex: true}); len(got) != 2 {
		t.Errorf("regex: want 2 lines, got %+v", got)
	}
	// Bad regex surfaces as an error.
	if err := runFallback(context.Background(), dir, "a(", Options{Regex: true}, func([]Result) {}); err == nil {
		t.Error("invalid regex: want error")
	}
}

func TestRunFallback_Globs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "needle\n")
	writeFile(t, dir, "b.txt", "needle\n")
	writeFile(t, dir, "gen/c.go", "needle\n")
	paths := func(rs []Result) map[string]bool {
		m := map[string]bool{}
		for _, r := range rs {
			m[filepath.ToSlash(r.Path)] = true
		}
		return m
	}
	got := paths(fallback(t, dir, "needle", Options{Include: []string{"*.go"}}))
	if len(got) != 2 || !got["a.go"] || !got["gen/c.go"] {
		t.Errorf("include *.go: got %v", got)
	}
	got = paths(fallback(t, dir, "needle", Options{Exclude: []string{"gen"}}))
	if len(got) != 2 || got["gen/c.go"] {
		t.Errorf("exclude gen: got %v", got)
	}
	got = paths(fallback(t, dir, "needle", Options{Include: []string{"gen/**"}}))
	if len(got) != 1 || !got["gen/c.go"] {
		t.Errorf("include gen/**: got %v", got)
	}
}

func TestRunFallback_Context(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "f.txt", "one\ntwo\nHIT\nfour\nHIT\nsix\nseven\n")
	got := fallback(t, dir, "HIT", Options{Context: 1})
	if len(got) != 2 {
		t.Fatalf("want 2 results, got %d", len(got))
	}
	first, second := got[0], got[1]
	if len(first.Before) != 1 || first.Before[0].Text != "two" || first.Before[0].Line != 2 {
		t.Errorf("first.Before = %+v", first.Before)
	}
	if len(first.After) != 1 || first.After[0].Text != "four" {
		t.Errorf("first.After = %+v", first.After)
	}
	if len(second.Before) != 1 || second.Before[0].Line != 4 {
		t.Errorf("second.Before = %+v", second.Before)
	}
	if len(second.After) != 1 || second.After[0].Text != "six" {
		t.Errorf("second.After = %+v", second.After)
	}
}

// TestSearch_MaxResults checks the cap: Shown stops at MaxResults while
// Total keeps counting, and 0 means unlimited.
func TestSearch_MaxResults(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", strings.Repeat("hit\n", 30))
	writeFile(t, dir, "b.txt", strings.Repeat("hit\n", 30))

	rs, sum, err := Collect(context.Background(), []string{dir}, "hit", Options{MaxResults: 40})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 40 || sum.Shown != 40 || sum.Total != 60 || !sum.Truncated() {
		t.Errorf("capped: len=%d sum=%+v", len(rs), sum)
	}
	if sum.Files != 2 {
		t.Errorf("files: got %d want 2", sum.Files)
	}
	for _, r := range rs {
		if !filepath.IsAbs(r.Path) {
			t.Errorf("want absolute path, got %q", r.Path)
		}
	}

	rs, sum, _ = Collect(context.Background(), []string{dir}, "hit", Options{})
	if len(rs) != 60 || sum.Truncated() {
		t.Errorf("unlimited: len=%d sum=%+v", len(rs), sum)
	}
}

// TestSearch_Options runs toggles + context through whichever engine is
// installed, so rg and the fallback are held to the same expectations.
func TestSearch_Options(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.go", "pre\nfoo := 1\npost\nfoobar\n")
	writeFile(t, dir, "b.txt", "foo\n")
	rs, sum, err := Collect(context.Background(), []string{dir}, "foo",
		Options{WholeWord: true, Include: []string{"*.go"}, Context: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || sum.Total != 1 {
		t.Fatalf("want 1 result, got %+v", rs)
	}
	r := rs[0]
	if r.Line != 2 || len(r.Before) != 1 || r.Before[0].Text != "pre" || len(r.After) != 1 || r.After[0].Text != "post" {
		t.Errorf("result = %+v", r)
	}
}

// TestStream_Batches checks that Stream delivers every result and ends
// with a Done batch carrying the summary.
func TestStream_Batches(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		writeFile(t, dir, filepath.Join("d", string(rune('a'+i))+".txt"), strings.Repeat("hit\n", 60))
	}
	got := 0
	var done *Batch
	for b := range Stream(context.Background(), []string{dir}, "hit", Options{MaxResults: 250}) {
		if b.Done {
			bb := b
			done = &bb
			continue
		}
		if len(b.Results) == 0 {
			t.Error("empty non-final batch")
		}
		got += len(b.Results)
	}
	if done == nil {
		t.Fatal("no Done batch")
	}
	if got != 250 || done.Summary.Shown != 250 || done.Summary.Total != 300 {
		t.Errorf("got %d results, summary %+v", got, done.Summary)
	}
}

// TestStream_Cancel makes sure a cancelled stream still closes its channel.
func TestStream_Cancel(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "hit\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range Stream(ctx, []string{dir}, "hit", Options{}) {
	}
}

func TestRgArgs(t *testing.T) {
	args := strings.Join(rgArgs("q", Options{
		CaseSensitive: true, WholeWord: true, Context: 2,
		Include: []string{"*.go"}, Exclude: []string{"vendor", "!gen"},
	}), " ")
	for _, want := range []string{"--json", "--case-sensitive", "--fixed-strings", "--word-regexp",
		"--context=2", "--glob=*.go", "--glob=!vendor", "--glob=!gen", "-- q"} {
		if !strings.Contains(args, want) {
			t.Errorf("args %q missing %q", args, want)
		}
	}
	args = strings.Join(rgArgs("q", Options{Regex: true}), " ")
	if !strings.Contains(args, "--smart-case") || strings.Contains(args, "--fixed-strings") {
		t.Errorf("regex/smart-case args wrong: %q", args)
	}
}

// TestParseStream_Context feeds rg's context events and checks they are
// attached to the neighbouring matches, and that files are flushed apart.
func TestParseStream_Context(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"begin","data":{"path":{"text":"a.go"}}}`,
		`{"type":"context","data":{"path":{"text":"a.go"},"lines":{"text":"before\n"},"line_number":1,"submatches":[]}}`,
		`{"type":"match","data":{"path":{"text":"a.go"},"lines":{"text":"hit\n"},"line_number":2,"submatches":[{"match":{"text":"hit"},"start":0,"end":3}]}}`,
		`{"type":"context","data":{"path":{"text":"a.go"},"lines":{"text":"after\n"},"line_number":3,"submatches":[]}}`,
		`{"type":"end","data":{"path":{"text":"a.go"}}}`,
		`{"type":"begin","data":{"path":{"text":"b.go"}}}`,
		`{"type":"match","data":{"path":{"text":"b.go"},"lines":{"text":"hit\n"},"line_number":9,"submatches":[{"match":{"text":"hit"},"start":0,"end":3}]}}`,
		`{"type":"end","data":{"path":{"text":"b.go"}}}`,
	}, "\n")
	var files []string
	var all []Result
	err := parseStream(strings.NewReader(stream), 1, func(p string, rs []Result) {
		files = append(files, p)
		all = append(all, rs...)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || files[0] != "a.go" || files[1] != "b.go" {
		t.Fatalf("files = %v", files)
	}
	a := all[0]
	if len(a.Before) != 1 || a.Before[0].Text != "before" || len(a.After) != 1 || a.After[0].Text != "after" {
		t.Errorf("context not attached: %+v", a)
	}
}

func TestSplitGlobs(t *testing.T) {
	got := SplitGlobs(" *.go, ,src/** ,")
	if len(got) != 2 || got[0] != "*.go" || got[1] != "src/**" {
		t.Errorf("SplitGlobs = %q", got)
	}
}

// CountLiteral backs the replace confirm, so it is exact and case-sensitive
// on both engines.
func TestCountAndReplaceLiteral(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "a.b a.b axb\n")
	writeFile(t, dir, "b.txt", "nothing\n")

	counts, err := CountLiteral([]string{dir}, "a.b")
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 1 || counts[0].Count != 2 || filepath.Base(counts[0].Path) != "a.txt" {
		t.Fatalf("counts = %+v", counts)
	}
	n, err := ReplaceLiteralInFile(counts[0].Path, "a.b", "Z")
	if err != nil || n != 2 {
		t.Fatalf("replace: n=%d err=%v", n, err)
	}
	data, _ := os.ReadFile(counts[0].Path)
	if string(data) != "Z Z axb\n" {
		t.Errorf("after replace: %q", data)
	}
}
