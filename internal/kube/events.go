package kube

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Event is one row of `kubectl get events`.
type Event struct {
	Type    string // Normal or Warning
	Reason  string
	Message string
	Object  string // "pod/api-1": the involved object, kind in lower case
	Count   int    // how many times it happened (at least 1)
	Last    time.Time
}

type rawEvent struct {
	Metadata       meta `json:"metadata"`
	InvolvedObject struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	} `json:"involvedObject"`
	Type           string     `json:"type"`
	Reason         string     `json:"reason"`
	Message        string     `json:"message"`
	Count          int        `json:"count"`
	FirstTimestamp *time.Time `json:"firstTimestamp"`
	LastTimestamp  *time.Time `json:"lastTimestamp"`
	EventTime      *time.Time `json:"eventTime"`
	Series         *struct {
		Count            int        `json:"count"`
		LastObservedTime *time.Time `json:"lastObservedTime"`
	} `json:"series"`
}

func firstTime(ts ...*time.Time) time.Time {
	for _, t := range ts {
		if t != nil && !t.IsZero() {
			return *t
		}
	}
	return time.Time{}
}

// ParseEvents reads `kubectl get events -o json`, newest first.
func ParseEvents(b []byte) ([]Event, error) {
	raw, err := parseList[rawEvent](b)
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(raw))
	for _, r := range raw {
		e := Event{Type: r.Type, Reason: r.Reason, Message: r.Message, Count: r.Count,
			Object: strings.ToLower(r.InvolvedObject.Kind) + "/" + r.InvolvedObject.Name}
		var observed *time.Time
		if r.Series != nil {
			if e.Count < r.Series.Count {
				e.Count = r.Series.Count
			}
			observed = r.Series.LastObservedTime
		}
		if e.Count < 1 {
			e.Count = 1
		}
		created := r.Metadata.CreationTimestamp
		e.Last = firstTime(observed, r.LastTimestamp, r.EventTime, r.FirstTimestamp, &created)
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Last.After(out[j].Last) })
	return out, nil
}

// eventKind is the API kind of a view kind ("pod" → "Pod").
func eventKind(kind string) string {
	switch kind {
	case "deployment":
		return "Deployment"
	case "service":
		return "Service"
	case "job":
		return "Job"
	case "pod":
		return "Pod"
	}
	if kind == "" {
		return ""
	}
	return strings.ToUpper(kind[:1]) + kind[1:]
}

// Events lists the namespace's events, newest first (read only). With a
// kind and name ("pod", "api-1") only that object's events.
func (c Client) Events(ctx context.Context, kind, name string) ([]Event, error) {
	args := []string{"get", "events", "-o", "json"}
	if kind != "" && name != "" {
		args = append(args, "--field-selector", "involvedObject.kind="+eventKind(kind)+",involvedObject.name="+name)
	}
	out, err := c.capture(ctx, append(args, c.scope(true)...)...)
	if err != nil {
		return nil, err
	}
	return ParseEvents([]byte(out))
}

// EventsText is events as a plain table for a read-only page: LAST, TYPE,
// REASON, OBJECT, MESSAGE (one line each; "x7" when it happened 7 times).
func EventsText(evs []Event, now time.Time) string {
	if len(evs) == 0 {
		return "No events.\n"
	}
	rows := [][]string{{"LAST", "TYPE", "REASON", "OBJECT", "MESSAGE"}}
	for _, e := range evs {
		msg := strings.Join(strings.Fields(e.Message), " ")
		if e.Count > 1 {
			msg = "x" + strconv.Itoa(e.Count) + " " + msg
		}
		rows = append(rows, []string{Age(e.Last, now), e.Type, e.Reason, e.Object, msg})
	}
	widths := make([]int, 4)
	for _, r := range rows {
		for i := range widths {
			widths[i] = max(widths[i], len([]rune(r[i])))
		}
	}
	var b strings.Builder
	for _, r := range rows {
		for i := range widths {
			fmt.Fprintf(&b, "%-*s  ", widths[i], r[i])
		}
		b.WriteString(r[4])
		b.WriteString("\n")
	}
	return b.String()
}
