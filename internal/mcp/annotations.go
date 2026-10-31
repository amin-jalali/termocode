package mcp

import (
	"sort"
	"strings"
)

// Content annotations (MCPX-38): a server may mark a result item for the
// user only (audience ["user"]): termocode shows it to the user and does
// not send it to the model. priority (0 to 1) orders the items for the
// model, highest first; an item without one counts as 0.5.

// ContentAnnotations are a content item's hints.
type ContentAnnotations struct {
	Audience []string `json:"audience,omitempty"`
	Priority *float64 `json:"priority,omitempty"`
}

// forUserOnly reports an item whose audience is the user alone.
func (c Content) forUserOnly() bool {
	if c.Annotations == nil || len(c.Annotations.Audience) == 0 {
		return false
	}
	for _, a := range c.Annotations.Audience {
		if a != "user" {
			return false
		}
	}
	return true
}

func (c Content) priority() float64 {
	if c.Annotations == nil || c.Annotations.Priority == nil {
		return 0.5
	}
	return *c.Annotations.Priority
}

// modelOrder lists the indexes of the items for the model, by priority.
func (r CallResult) modelOrder() []int {
	idx := make([]int, 0, len(r.Content))
	for i, c := range r.Content {
		if !c.forUserOnly() {
			idx = append(idx, i)
		}
	}
	sort.SliceStable(idx, func(a, b int) bool { return r.Content[idx[a]].priority() > r.Content[idx[b]].priority() })
	return idx
}

// UserText is the text of the items for the user only ("" when none).
func (r CallResult) UserText() string {
	var parts []string
	for _, c := range r.Content {
		if !c.forUserOnly() {
			continue
		}
		switch {
		case c.Type == "text" && c.Text != "":
			parts = append(parts, c.Text)
		case c.Type == "resource" && c.Resource != nil && c.Resource.Text != "":
			parts = append(parts, c.Resource.Text)
		case c.Type == "resource_link":
			parts = append(parts, "[resource link "+c.URI+"]")
		case c.Type == "image" || c.Type == "audio":
			parts = append(parts, "["+c.Type+" "+c.MimeType+"]")
		}
	}
	return strings.Join(parts, "\n")
}
