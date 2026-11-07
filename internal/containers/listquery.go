package containers

import (
	"sort"
	"strconv"
	"strings"
)

// ListQuery is a list filter with optional words (CTX-22):
// "state:running" or "is:exited,paused" (aliases up, stopped), "image:nginx",
// "is:dangling"; the other words are plain text. All lower case.
type ListQuery struct {
	Text     string
	States   []string
	Image    string
	Dangling bool
	Tokens   bool // at least one state:/is:/image: word
}

var stateAlias = map[string]string{"up": "running", "stopped": "exited", "exit": "exited"}

// ParseListQuery splits a filter into its words.
func ParseListQuery(s string) ListQuery {
	var q ListQuery
	var text []string
	for _, w := range strings.Fields(strings.ToLower(s)) {
		key, val, ok := strings.Cut(w, ":")
		if !ok || val == "" || (key != "state" && key != "is" && key != "image") {
			text = append(text, w)
			continue
		}
		q.Tokens = true
		if key == "image" {
			q.Image = val
			continue
		}
		for _, v := range strings.Split(val, ",") {
			switch {
			case v == "":
			case v == "dangling":
				q.Dangling = true
			default:
				if a, ok := stateAlias[v]; ok {
					v = a
				}
				q.States = append(q.States, v)
			}
		}
	}
	q.Text = strings.Join(text, " ")
	return q
}

func containsFold(s, sub string) bool { return strings.Contains(strings.ToLower(s), sub) }

// MatchContainer reports whether c passes every part of the query.
func (q ListQuery) MatchContainer(c Container) bool {
	if q.Dangling {
		return false
	}
	if len(q.States) > 0 {
		st, hit := StateFromPS(c.State, c.Status), false
		for _, s := range q.States {
			hit = hit || s == st
		}
		if !hit {
			return false
		}
	}
	if q.Image != "" && !containsFold(c.Image, q.Image) {
		return false
	}
	if q.Text == "" {
		return true
	}
	if containsFold(c.Image, q.Text) || containsFold(c.ID, q.Text) {
		return true
	}
	for _, n := range c.Names {
		if containsFold(strings.TrimPrefix(n, "/"), q.Text) {
			return true
		}
	}
	return false
}

func imageDangling(img Image) bool {
	for _, t := range img.RepoTags {
		if t != "" && !strings.Contains(t, "<none>") {
			return false
		}
	}
	return true
}

// MatchImage reports whether img passes the query; a state word never
// matches an image.
func (q ListQuery) MatchImage(img Image) bool {
	if len(q.States) > 0 {
		return false
	}
	if q.Dangling && !imageDangling(img) {
		return false
	}
	tagHas := func(sub string) bool {
		for _, t := range img.RepoTags {
			if containsFold(t, sub) {
				return true
			}
		}
		return false
	}
	if q.Image != "" && !tagHas(q.Image) {
		return false
	}
	return q.Text == "" || tagHas(q.Text) || containsFold(img.ID, q.Text)
}

var sizeUnits = map[string]float64{"b": 1, "kb": 1e3, "mb": 1e6, "gb": 1e9, "tb": 1e12,
	"kib": 1 << 10, "mib": 1 << 20, "gib": 1 << 30, "tib": 1 << 40}

// SizeBytes reads a CLI size like "12.3MB (virtual 1GB)" or "1.5kB" as
// bytes (the first number only); -1 when it is not a size.
func SizeBytes(s string) int64 {
	f := strings.Fields(s)
	if len(f) == 0 {
		return -1
	}
	w := f[0]
	i := 0
	for i < len(w) && (w[i] == '.' || (w[i] >= '0' && w[i] <= '9')) {
		i++
	}
	n, err := strconv.ParseFloat(w[:i], 64)
	mul, ok := sizeUnits[strings.ToLower(w[i:])]
	if err != nil || !ok {
		return -1
	}
	return int64(n*mul + 0.5)
}

var stateRank = map[string]int{"running": 0, "restarting": 1, "paused": 2, "created": 3, "exited": 4, "dead": 5}

func containerName(c Container) string {
	if len(c.Names) > 0 {
		return strings.TrimPrefix(c.Names[0], "/")
	}
	return c.ID
}

// SortContainers sorts in place by "name", "image", "state" (running
// first), "created" (newest first) or "size" (largest first); ties keep
// their order. Unknown keys leave the list as it is.
func SortContainers(cs []Container, by string) {
	var less func(a, b Container) bool
	switch by {
	case "name":
		less = func(a, b Container) bool { return containerName(a) < containerName(b) }
	case "image":
		less = func(a, b Container) bool { return a.Image < b.Image }
	case "state":
		rank := func(c Container) int {
			if r, ok := stateRank[StateFromPS(c.State, c.Status)]; ok {
				return r
			}
			return len(stateRank)
		}
		less = func(a, b Container) bool { return rank(a) < rank(b) }
	case "created":
		less = func(a, b Container) bool { return a.Created.After(b.Created) }
	case "size":
		less = func(a, b Container) bool { return SizeBytes(a.Size) > SizeBytes(b.Size) }
	default:
		return
	}
	sort.SliceStable(cs, func(i, j int) bool { return less(cs[i], cs[j]) })
}
