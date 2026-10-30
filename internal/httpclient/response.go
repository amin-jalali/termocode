package httpclient

import (
	"bytes"
	"fmt"
	"mime"
	"strings"
	"time"
)

// ErrKind classifies a failed send for the page's row-2 text.
type ErrKind int

// Error kinds.
const (
	ErrKindNone ErrKind = iota
	ErrKindTimeout
	ErrKindTLS
	ErrKindDial
	ErrKindCancelled
	ErrKindRedirects
	ErrKindBlocked
	ErrKindOther
)

// Hop is one redirect step: the 3xx status and the URL it pointed to.
type Hop struct {
	Status int    `json:"status"`
	URL    string `json:"url"`
}

// Timing is what httptrace saw (zero when a phase did not happen).
type Timing struct {
	DNS       time.Duration `json:"dns,omitempty"`
	Connect   time.Duration `json:"connect,omitempty"`
	TLS       time.Duration `json:"tls,omitempty"`
	FirstByte time.Duration `json:"first_byte,omitempty"`
	Total     time.Duration `json:"total,omitempty"`
}

// Response is the result of one send. It is always returned; a failed
// send has Err set (like mobocode's HttpResponseResult).
type Response struct {
	Method     string
	URL        string // the final URL (after redirects)
	Status     int
	StatusText string
	Proto      string
	// Headers are the response headers, names lower-cased, sorted by
	// name (net/http does not keep the arrival order).
	Headers []Header
	// ReqHeaders are the headers that were sent (secrets included; use
	// RedactHeaders before showing them anywhere but the live page).
	ReqHeaders  []Header
	Body        []byte
	Size        int64 // body bytes received (after decompression)
	Truncated   bool
	MaxBody     int64
	Elapsed     time.Duration
	Timing      Timing
	Hops        []Hop
	ContentType string // media type, lower-cased, no parameters
	Charset     string // lower-cased; "" = utf-8
	IsJSON      bool
	IsBinary    bool
	Err         string
	ErrKind     ErrKind
}

// Header returns the value of the response header name (any case).
func (r *Response) Header(name string) string {
	name = strings.ToLower(name)
	for _, h := range r.Headers {
		if h.Name == name {
			return h.Value
		}
	}
	return ""
}

// Text is the body decoded with the charset (utf-8 default, latin-1
// honoured).
func (r *Response) Text() string { return DecodeText(r.Body, r.Charset) }

// TextBytes is Text as bytes: the body itself (not a copy) when it is
// UTF-8 already. Callers must not change it.
func (r *Response) TextBytes() []byte {
	if !isLatin1(r.Charset) {
		return r.Body
	}
	return []byte(r.Text())
}

func isLatin1(charset string) bool {
	switch strings.ToLower(charset) {
	case "iso-8859-1", "latin1", "latin-1", "iso8859-1", "windows-1252", "cp1252":
		return true
	}
	return false
}

// StatusLine is "200 OK", or "ERR" for a failed send.
func (r *Response) StatusLine() string {
	if r.Err != "" {
		return "ERR"
	}
	if r.StatusText == "" {
		return fmt.Sprint(r.Status)
	}
	return fmt.Sprintf("%d %s", r.Status, r.StatusText)
}

// Summary is the page's row-2 text without colours, e.g.
// "200 OK · 118 ms · 1.4 KB · json · 2 redirects".
func (r *Response) Summary() string {
	if r.Err != "" {
		return "✘ " + r.Err
	}
	parts := []string{r.StatusLine(), FormatDuration(r.Elapsed), FormatSize(r.Size)}
	if kind := r.Kind(); kind != "" {
		parts = append(parts, kind)
	}
	if n := len(r.Hops); n == 1 {
		parts = append(parts, "1 redirect")
	} else if n > 1 {
		parts = append(parts, fmt.Sprintf("%d redirects", n))
	}
	if r.Truncated {
		parts = append(parts, "truncated at "+FormatSize(r.MaxBody))
	}
	return strings.Join(parts, " · ")
}

// Kind is a short word for the body type: json, html, xml, text,
// binary or "".
func (r *Response) Kind() string {
	switch {
	case r.IsBinary:
		return "binary"
	case r.IsJSON:
		return "json"
	case strings.Contains(r.ContentType, "html"):
		return "html"
	case strings.Contains(r.ContentType, "xml"):
		return "xml"
	case strings.HasPrefix(r.ContentType, "text/"):
		return "text"
	}
	return ""
}

// FormatSize is mobocode's _formatSize: B / KB / MB, one decimal.
func FormatSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}

// FormatDuration is "118 ms" below 10 s, else "12.3 s".
func FormatDuration(d time.Duration) string {
	if d < 10*time.Second {
		return fmt.Sprintf("%d ms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1f s", d.Seconds())
}

// PrettyJSON indents JSON with two spaces (the shape of mobocode's
// prettyBody) without decoding it: no key reordering, no float
// rewriting. ok is false when b is not JSON.
func PrettyJSON(b []byte) ([]byte, bool) {
	t := bytes.TrimSpace(b)
	if len(t) == 0 || (t[0] != '{' && t[0] != '[') {
		return b, false
	}
	out, ok := indentJSON(make([]byte, 0, 3*len(t)+64), t)
	if !ok {
		return b, false
	}
	return out, true
}

// DecodeText decodes b with charset: latin-1 (iso-8859-1, latin1,
// windows-1252 as latin-1) maps bytes to runes; everything else is
// read as UTF-8.
func DecodeText(b []byte, charset string) string {
	if isLatin1(charset) {
		rs := make([]rune, len(b))
		for i, c := range b {
			rs[i] = rune(c)
		}
		return string(rs)
	}
	return string(b)
}

// parseContentType returns the media type and charset, lower-cased.
func parseContentType(v string) (string, string) {
	if v == "" {
		return "", ""
	}
	mt, params, err := mime.ParseMediaType(v)
	if err != nil {
		mt, _, _ = strings.Cut(v, ";")
		return strings.ToLower(strings.TrimSpace(mt)), ""
	}
	return strings.ToLower(mt), strings.ToLower(params["charset"])
}

// isJSONType is mobocode's isJson rule.
func isJSONType(mt string) bool {
	return strings.Contains(mt, "application/json") || strings.Contains(mt, "+json")
}

// isBinary: image/audio/video, octet-stream, pdf, or a NUL byte in the
// first 512 bytes.
func isBinary(mt string, b []byte) bool {
	switch {
	case strings.HasPrefix(mt, "image/svg"):
	case strings.HasPrefix(mt, "image/"), strings.HasPrefix(mt, "audio/"), strings.HasPrefix(mt, "video/"),
		mt == "application/octet-stream", mt == "application/pdf", mt == "application/zip":
		return true
	}
	head := b
	if len(head) > 512 {
		head = head[:512]
	}
	return bytes.IndexByte(head, 0) >= 0
}
