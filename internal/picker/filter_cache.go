package picker

import (
	"slices"
	"strings"

	"github.com/sahilm/fuzzy"
)

// filterCache makes typing into a long list (Go to file on 50 000
// files) cheap. It keeps the titles of the non-header rows for the
// items it saw, and which of them the last query matched: a query that
// only grew can match no row the shorter one missed (each query letter
// must be found in order), so the next key searches just those rows.
// fuzzy scores each title on its own and sorts stably, so searching the
// rows that can match, in list order, gives the same result as
// searching all of them.
type filterCache struct {
	items    []Item   // the list the titles belong to
	titles   []string // titles of the non-header rows
	indexMap []int    // titles[k] is items[indexMap[k]]
	input    string   // the last query
	cand     []int    // the titles it matched, in list order
	scanned  int      // titles the last find searched (tests)
}

// find runs the fuzzy search of input over items' non-header titles and
// returns the titles, their item indexes and the hits (Index into
// titles, best first).
func (c *filterCache) find(items []Item, input string) ([]string, []int, fuzzy.Matches) {
	if !sameItems(c.items, items) {
		c.items, c.titles, c.indexMap, c.input, c.cand = items, nil, nil, "", nil
		c.titles = make([]string, 0, len(items))
		c.indexMap = make([]int, 0, len(items))
		for i, it := range items {
			if !it.Header {
				c.titles = append(c.titles, it.Title)
				c.indexMap = append(c.indexMap, i)
			}
		}
	}
	var hits fuzzy.Matches
	if c.input != "" && strings.HasPrefix(input, c.input) {
		sub := make([]string, len(c.cand))
		for k, i := range c.cand {
			sub[k] = c.titles[i]
		}
		hits = fuzzy.Find(input, sub)
		for k := range hits {
			hits[k].Index = c.cand[hits[k].Index]
		}
		c.scanned = len(sub)
	} else {
		hits = fuzzy.Find(input, c.titles)
		c.scanned = len(c.titles)
	}
	cand := make([]int, len(hits))
	for k, h := range hits {
		cand[k] = h.Index
	}
	slices.Sort(cand)
	c.input, c.cand = input, cand
	return c.titles, c.indexMap, hits
}

// sameItems reports whether a and b are the same list (same backing
// array and length; a picker's items are replaced, never edited).
func sameItems(a, b []Item) bool {
	return len(a) == len(b) && (len(a) == 0 || &a[0] == &b[0])
}
