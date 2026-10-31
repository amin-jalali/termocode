package aimemory

// Recall picks the lessons that fit one AI tool call (OMX-18): a lesson
// about one of the call's paths, or one that shares at least two words
// (four or more letters) with the call. Unlike Select there is no
// "newest" fill: only real matches. Lessons whose Key is in seen are
// skipped (they were recalled or offered before). At most maxN, path
// matches first, then by shared words, newer first.
func Recall(ls []Lesson, h Hints, seen map[string]bool, maxN int) []Lesson {
	words := wordSet(h.Text)
	type hit struct{ i, score int }
	var hits []hit
	for i, l := range ls {
		if seen[l.Key()] {
			continue
		}
		if pathMatch(l.Paths, h.Paths) {
			hits = append(hits, hit{i, 1000})
			continue
		}
		n := 0
		for w := range wordSet(l.Title + " " + l.Text) {
			if words[w] {
				n++
			}
		}
		if n >= 2 {
			hits = append(hits, hit{i, n})
		}
	}
	// Highest score first, newer (later in the file) first on a tie.
	for a := 1; a < len(hits); a++ {
		for b := a; b > 0 && (hits[b].score > hits[b-1].score || hits[b].score == hits[b-1].score && hits[b].i > hits[b-1].i); b-- {
			hits[b], hits[b-1] = hits[b-1], hits[b]
		}
	}
	var out []Lesson
	for _, x := range hits {
		if len(out) == maxN {
			break
		}
		out = append(out, ls[x.i])
	}
	return out
}

// Key names a lesson for "seen" sets: its id, else its date and title.
func (l Lesson) Key() string {
	if l.ID != "" {
		return l.ID
	}
	return l.Date + "\x00" + l.Title
}
