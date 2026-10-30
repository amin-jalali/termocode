package webproto

import (
	"strings"
	"testing"
)

// Spec (WEB-24): the search data is typed for the page: the streamed
// batch event and the summary that answers search.run.
func TestSearchDataGenerated(t *testing.T) {
	b, err := GenerateTS()
	if err != nil {
		t.Fatal(err)
	}
	ts := string(b)
	for _, want := range []string{
		`  "search.batch": SearchBatch;`,
		`  "search.run": SearchSummary;`,
		`  stopped?: "stopped" | "replaced" | "timeout" | "limit";`,
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("proto.d.ts misses %q", want)
		}
	}
}
