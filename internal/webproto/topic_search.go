package webproto

import "time"

// The search data (web design 2.7): search.run streams its hits as
// search.batch events (with the request's id) and answers with a summary
// when it ends. One search runs per connection: a new one stops the old
// (which then answers with stopped "replaced"); search.stop stops it.

func init() {
	registerEvent(EventSearchBatch, SearchBatch{})
	registerResult(OpSearchRun, SearchSummary{})
}

// EventSearchBatch is the ev kind of a batch of search hits.
const EventSearchBatch = "search.batch"

// Search limits (the hits cap is MaxSearchHits).
const (
	MaxSearchText    = 400              // bytes of a hit's line or context line; more is cut
	MaxSearchBytes   = 1 << 20          // bytes of all hits' text of one search
	MaxSearchBatch   = 100              // hits in one batch
	SearchContext    = 1                // context lines before and after a hit
	SearchTimeout    = 10 * time.Second // a search stops after this
	SearchDefaultMax = MaxSearchHits    // max when the args say 0
)

// SearchBatch is a batch of hits of the search request ID.
type SearchBatch struct {
	ID      uint64      `json:"id"`
	Results []SearchHit `json:"results"`
}

// SearchHit is one matching line.
type SearchHit struct {
	Path   string       `json:"path"` // workspace-relative
	Line   int          `json:"line"` // 1-based
	Col    int          `json:"col"`  // 1-based column of the first match (bytes)
	Text   string       `json:"text"`
	Marks  []SearchMark `json:"marks,omitempty"`  // the matches in Text
	Before []string     `json:"before,omitempty"` // the lines just before
	After  []string     `json:"after,omitempty"`  // the lines just after
}

// SearchMark is one match in a hit's text, as offsets the page can use
// on a JavaScript string (UTF-16 code units), end exclusive.
type SearchMark struct {
	S int `json:"s"`
	E int `json:"e"`
}

// SearchSummary answers search.run.
type SearchSummary struct {
	Files     int    `json:"files"`                                                   // files with a hit sent
	Matches   int    `json:"matches"`                                                 // hits sent
	Truncated bool   `json:"truncated,omitempty"`                                     // more hits exist than were sent
	Ms        int64  `json:"ms"`                                                      // how long it ran
	Stopped   string `json:"stopped,omitempty" enum:"stopped|replaced|timeout|limit"` // why it ended early
}
