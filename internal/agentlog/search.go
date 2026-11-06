package agentlog

import (
	"strings"
	"time"
)

// SearchLimits caps a search (0 fields take DefaultSearchLimits').
type SearchLimits struct {
	Files   int   // session files read
	Bytes   int64 // bytes read, summed over the files
	PerFile int   // hits kept per file
}

// DefaultSearchLimits are the caps of the search inside sessions.
var DefaultSearchLimits = SearchLimits{Files: 200, Bytes: 50 << 20, PerFile: 3}

// Hit is one match.
type Hit struct {
	Session Summary
	Time    time.Time
	Kind    Kind
	Snippet string // one line around the match, redacted
}

// Search looks for query (any case) in the turns of sums, in order. The
// text searched is the redacted text, so a secret is never a hit. capped
// reports that a cap stopped it before the last file.
func Search(sums []Summary, query string, lim SearchLimits) (hits []Hit, capped bool) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, false
	}
	def := DefaultSearchLimits
	if lim.Files <= 0 {
		lim.Files = def.Files
	}
	if lim.Bytes <= 0 {
		lim.Bytes = def.Bytes
	}
	if lim.PerFile <= 0 {
		lim.PerFile = def.PerFile
	}
	var read int64
	for i, sum := range sums {
		if i >= lim.Files {
			return hits, true
		}
		if sum.Err != "" {
			continue
		}
		s, err := ReadFile(sum.CLI, sum.Path)
		if err != nil && len(s.Turns) == 0 {
			continue
		}
		n := 0
		for _, t := range s.Turns {
			text := t.Text
			switch t.Kind {
			case ToolCall:
				text = t.Tool + " " + t.Args
			case ToolResult:
				text = t.Body()
			}
			j := strings.Index(strings.ToLower(text), q)
			if j < 0 {
				continue
			}
			hits = append(hits, Hit{Session: sum, Time: t.Time, Kind: t.Kind, Snippet: snippet(text, j, len(q))})
			if n++; n >= lim.PerFile {
				break
			}
		}
		size, _ := statFile(sum.Path)
		read += min(size, maxRead)
		if read >= lim.Bytes && i < len(sums)-1 {
			return hits, true
		}
	}
	return hits, false
}

// snippet is one line of text around [at, at+n), about 90 runes.
func snippet(text string, at, n int) string {
	from, to := at, at+n
	for i := 0; i < 40 && from > 0; i++ {
		from--
		for from > 0 && text[from]&0xC0 == 0x80 {
			from--
		}
	}
	for i := 0; i < 50 && to < len(text); i++ {
		to++
		for to < len(text) && text[to]&0xC0 == 0x80 {
			to++
		}
	}
	out := strings.Join(strings.Fields(text[from:to]), " ")
	if from > 0 {
		out = "…" + out
	}
	if to < len(text) {
		out += "…"
	}
	return out
}
