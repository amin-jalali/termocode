package httpclient

import (
	"strconv"
	"strings"
)

// SSEEvent is one Server-Sent Event (API-16).
type SSEEvent struct {
	Event string `json:"event"`           // "message" when the stream names none
	Data  string `json:"data"`            // data lines joined with "\n"
	ID    string `json:"id,omitempty"`    // the last event id seen so far
	Retry int    `json:"retry,omitempty"` // ms, when this event set one
}

// isStreamType says whether an answer of media type mt is a stream that
// may run for a long time (the send timeout then covers the headers only).
func isStreamType(mt string) bool {
	switch mt {
	case "text/event-stream", "application/x-ndjson", "application/jsonl", "application/ndjson":
		return true
	}
	return false
}

// ParseSSE reads an event stream the way a browser does: lines end in
// LF, CRLF or CR; ":" starts a comment; a blank line sends the event;
// an event without data is dropped, and so is a last event that has no
// blank line after it (the stream was cut).
func ParseSSE(b []byte) []SSEEvent {
	var out []SSEEvent
	var data strings.Builder
	hasData := false
	event, id, retry := "", "", 0
	s := string(b)
	for len(s) > 0 {
		i := strings.IndexAny(s, "\r\n")
		if i < 0 {
			break // no line end: the stream was cut here
		}
		line := s[:i]
		if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
			s = s[i+2:]
		} else {
			s = s[i+1:]
		}
		if line == "" {
			if hasData {
				ev := event
				if ev == "" {
					ev = "message"
				}
				out = append(out, SSEEvent{Event: ev, Data: strings.TrimSuffix(data.String(), "\n"), ID: id, Retry: retry})
			}
			data.Reset()
			hasData, event, retry = false, "", 0
			continue
		}
		if line[0] == ':' {
			continue
		}
		field, value := line, ""
		if j := strings.IndexByte(line, ':'); j >= 0 {
			field, value = line[:j], strings.TrimPrefix(line[j+1:], " ")
		}
		switch field {
		case "data":
			data.WriteString(value)
			data.WriteByte('\n')
			hasData = true
		case "event":
			event = value
		case "id":
			if !strings.ContainsRune(value, 0) {
				id = value
			}
		case "retry":
			if n, err := strconv.Atoi(value); err == nil && n >= 0 && strings.Trim(value, "0123456789") == "" {
				retry = n
			}
		}
	}
	return out
}
