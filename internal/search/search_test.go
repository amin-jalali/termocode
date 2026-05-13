package search

import (
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

// TestParseJSON_CapAtMaxResults verifies we stop reading after MaxResults
// match events even if the stream is longer.
func TestParseJSON_CapAtMaxResults(t *testing.T) {
	var b strings.Builder
	for i := 0; i < MaxResults+50; i++ {
		b.WriteString(`{"type":"match","data":{"path":{"text":"x"},"lines":{"text":"x\n"},"line_number":1,"submatches":[{"match":{"text":"x"},"start":0,"end":1}]}}` + "\n")
	}
	results, err := parseJSON([]byte(b.String()))
	if err != nil {
		t.Fatalf("parseJSON: %v", err)
	}
	if len(results) != MaxResults {
		t.Errorf("cap: got %d want %d", len(results), MaxResults)
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
