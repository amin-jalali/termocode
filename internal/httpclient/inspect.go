package httpclient

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"time"
)

// Response inspection helpers for the Response page (API-10): cookies,
// the timing waterfall, the size breakdown, XML/HTML layout, an HTML
// text preview, a line diff of two answers and the raw wire log.

// CookieRow is one Set-Cookie of an answer, ready to show.
type CookieRow struct {
	Name     string
	Value    string // Redacted unless reveal
	Domain   string
	Path     string
	Expires  string // "session", an RFC 1123 time, or "max-age 3600 s"
	Flags    []string
	SameSite string
}

// Cookies reads the Set-Cookie headers of r. Values are hidden unless
// reveal.
func (r *Response) Cookies(reveal bool) []CookieRow {
	var out []CookieRow
	for _, h := range r.Headers {
		if h.Name != "set-cookie" {
			continue
		}
		c, err := http.ParseSetCookie(h.Value)
		if err != nil || c.Name == "" {
			continue
		}
		row := CookieRow{Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path}
		if !reveal {
			row.Value = Redacted
		}
		switch {
		case c.MaxAge < 0:
			row.Expires = "expired"
		case c.MaxAge > 0:
			row.Expires = fmt.Sprintf("max-age %d s", c.MaxAge)
		case !c.Expires.IsZero():
			row.Expires = c.Expires.UTC().Format(time.RFC1123)
		default:
			row.Expires = "session"
		}
		if c.Secure {
			row.Flags = append(row.Flags, "Secure")
		}
		if c.HttpOnly {
			row.Flags = append(row.Flags, "HttpOnly")
		}
		if c.Partitioned {
			row.Flags = append(row.Flags, "Partitioned")
		}
		switch c.SameSite {
		case http.SameSiteLaxMode:
			row.SameSite = "Lax"
		case http.SameSiteStrictMode:
			row.SameSite = "Strict"
		case http.SameSiteNoneMode:
			row.SameSite = "None"
		}
		out = append(out, row)
	}
	return out
}

// Waterfall draws the timing phases as bars on one shared time line,
// width cells wide: "dns      ███·········  12 ms". Phases that did not
// happen are left out; the transfer phase is first byte → total.
func Waterfall(t Timing, width int) []string {
	if width < 10 {
		width = 10
	}
	type phase struct {
		name       string
		start, end time.Duration
	}
	var ps []phase
	at := time.Duration(0)
	add := func(name string, d time.Duration) {
		if d <= 0 {
			return
		}
		ps = append(ps, phase{name, at, at + d})
		at += d
	}
	add("dns", t.DNS)
	add("connect", t.Connect)
	add("tls", t.TLS)
	if t.FirstByte > at {
		ps = append(ps, phase{"wait", at, t.FirstByte})
		at = t.FirstByte
	}
	total := t.Total
	if total > at {
		ps = append(ps, phase{"download", at, total})
	}
	if total < at {
		total = at
	}
	if total <= 0 {
		return []string{"no timing data"}
	}
	cell := func(d time.Duration) int { return int(int64(d) * int64(width) / int64(total)) }
	out := make([]string, 0, len(ps)+1)
	for _, p := range ps {
		a, b := cell(p.start), cell(p.end)
		if b <= a {
			b = a + 1
		}
		if b > width {
			a, b = max(width-(b-a), 0), width
		}
		bar := strings.Repeat("·", a) + strings.Repeat("█", b-a) + strings.Repeat("·", width-b)
		out = append(out, fmt.Sprintf("%-9s %s %s", p.name, bar, FormatDuration(p.end-p.start)))
	}
	return append(out, fmt.Sprintf("%-9s %s %s", "total", strings.Repeat(" ", width), FormatDuration(total)))
}

// SizeRow is one line of the size breakdown.
type SizeRow struct {
	Part  string
	Bytes int64
}

// SizeBreakdown is how many bytes each part of the exchange took: the
// request headers and body, the response headers, the body as sent on
// the wire (Content-Length, when the server compressed it) and as
// decoded.
func (r *Response) SizeBreakdown(reqBody int) []SizeRow {
	hdr := func(hs []Header) int64 {
		var n int64
		for _, h := range hs {
			n += int64(len(h.Name) + 2 + len(h.Value) + 2)
		}
		return n
	}
	rows := []SizeRow{
		{"request headers", hdr(r.ReqHeaders)},
		{"request body", int64(reqBody)},
		{"response headers", hdr(r.Headers)},
	}
	if enc := r.Header("content-encoding"); enc != "" && enc != "identity" {
		var cl int64
		if _, err := fmt.Sscan(r.Header("content-length"), &cl); err == nil && cl >= 0 {
			rows = append(rows, SizeRow{"response body (" + enc + ")", cl})
		}
	}
	rows = append(rows, SizeRow{"response body", r.Size})
	return rows
}

// IsMarkup says whether the answer is XML or HTML.
func (r *Response) IsMarkup() bool {
	k := r.Kind()
	return k == "xml" || k == "html"
}

// PrettyMarkup indents XML (and well-formed HTML / XHTML) two spaces
// per level so indent folds work on it. ok is false when b does not
// read as markup; HTML that is not well-formed is then shown as is.
func PrettyMarkup(b []byte) ([]byte, bool) {
	t := bytes.TrimSpace(b)
	if len(t) == 0 || t[0] != '<' {
		return b, false
	}
	d := xml.NewDecoder(bytes.NewReader(t))
	d.Strict = false
	d.AutoClose = xml.HTMLAutoClose
	d.Entity = xml.HTMLEntity
	var out bytes.Buffer
	depth := 0
	line := func(s string) {
		out.WriteString(strings.Repeat("  ", depth))
		out.WriteString(s)
		out.WriteByte('\n')
	}
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return b, false
		}
		switch v := tok.(type) {
		case xml.StartElement:
			line("<" + markupName(v.Name) + markupAttrs(v.Attr) + ">")
			depth++
		case xml.EndElement:
			if depth > 0 {
				depth--
			}
			line("</" + markupName(v.Name) + ">")
		case xml.CharData:
			if s := strings.TrimSpace(string(v)); s != "" {
				for _, l := range strings.Split(s, "\n") {
					if l = strings.TrimSpace(l); l != "" {
						line(xmlEscape(l))
					}
				}
			}
		case xml.Comment:
			line("<!--" + string(v) + "-->")
		case xml.ProcInst:
			line("<?" + v.Target + " " + string(v.Inst) + "?>")
		case xml.Directive:
			line("<!" + string(v) + ">")
		}
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), true
}

func markupName(n xml.Name) string {
	if n.Space != "" && !strings.Contains(n.Space, "/") {
		return n.Space + ":" + n.Local
	}
	return n.Local
}

func markupAttrs(as []xml.Attr) string {
	var sb strings.Builder
	for _, a := range as {
		sb.WriteString(" " + markupName(a.Name) + `="` + xmlEscape(a.Value) + `"`)
	}
	return sb.String()
}

func xmlEscape(s string) string {
	var sb strings.Builder
	_ = xml.EscapeText(&sb, []byte(s))
	return strings.ReplaceAll(sb.String(), "&#xA;", "\n")
}

// HTMLText is a text preview of an HTML page: tags dropped, script and
// style left out, block tags as line breaks, entities decoded, blank
// lines dropped. Nothing is fetched or run.
func HTMLText(b []byte) string {
	s := string(b)
	var sb strings.Builder
	low := strings.ToLower(s)
	for i := 0; i < len(s); {
		if s[i] != '<' {
			j := strings.IndexByte(s[i:], '<')
			if j < 0 {
				j = len(s) - i
			}
			sb.WriteString(s[i : i+j])
			i += j
			continue
		}
		j := strings.IndexByte(s[i:], '>')
		if j < 0 {
			break
		}
		tag := strings.TrimLeft(low[i+1:i+j], "/")
		name, _, _ := strings.Cut(strings.TrimSpace(tag), " ")
		i += j + 1
		switch name {
		case "script", "style", "head", "template":
			if end := strings.Index(low[i:], "</"+name); end >= 0 {
				i += end
				if k := strings.IndexByte(s[i:], '>'); k >= 0 {
					i += k + 1
				}
			}
		case "br", "p", "div", "li", "tr", "h1", "h2", "h3", "h4", "h5", "h6", "table", "ul", "ol", "section", "article", "header", "footer", "title", "pre":
			sb.WriteByte('\n')
		}
	}
	var out []string
	for _, l := range strings.Split(html.UnescapeString(sb.String()), "\n") {
		if l = strings.Join(strings.Fields(l), " "); l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// DiffLines compares two texts line by line (a = older answer, b =
// newer) and returns "  same", "- old", "+ new" lines. Long inputs
// fall back to a plain prefix/suffix cut so the cost stays bounded.
func DiffLines(a, b []string) []string {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	suf := 0
	for suf < len(a)-pre && suf < len(b)-pre && a[len(a)-1-suf] == b[len(b)-1-suf] {
		suf++
	}
	ma, mb := a[pre:len(a)-suf], b[pre:len(b)-suf]
	var out []string
	for _, l := range a[:pre] {
		out = append(out, "  "+l)
	}
	if len(ma)*len(mb) > 4_000_000 {
		for _, l := range ma {
			out = append(out, "- "+l)
		}
		for _, l := range mb {
			out = append(out, "+ "+l)
		}
	} else {
		out = append(out, lcsDiff(ma, mb)...)
	}
	for _, l := range a[len(a)-suf:] {
		out = append(out, "  "+l)
	}
	return out
}

func lcsDiff(a, b []string) []string {
	n, m := len(a), len(b)
	dp := make([][]int32, n+1)
	for i := range dp {
		dp[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	var out []string
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, "  "+a[i])
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			out = append(out, "- "+a[i])
			i++
		default:
			out = append(out, "+ "+b[j])
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, "- "+a[i])
	}
	for ; j < m; j++ {
		out = append(out, "+ "+b[j])
	}
	return out
}

// WireLog is the exchange as it went over the wire, in HTTP/1.1 text
// form: request line, request headers, request body, then status line,
// response headers and body. Secret header values and URL parts are
// hidden unless reveal. A binary body is one note line.
func (r *Response) WireLog(method, url string, reqBody string, reveal bool) []string {
	u := url
	if !reveal {
		u = RedactURL(u)
	}
	val := func(h Header) string {
		if reveal {
			return h.Value
		}
		return RedactHeaderValue(h.Name, h.Value)
	}
	out := []string{"> " + method + " " + u}
	for _, h := range r.ReqHeaders {
		out = append(out, "> "+h.Name+": "+val(h))
	}
	out = append(out, ">")
	if reqBody != "" {
		for _, l := range strings.Split(strings.TrimSuffix(reqBody, "\n"), "\n") {
			out = append(out, "> "+l)
		}
	}
	out = append(out, "")
	if r.Err != "" {
		return append(out, "! "+r.Err)
	}
	proto := r.Proto
	if proto == "" {
		proto = "HTTP/1.1"
	}
	out = append(out, "< "+proto+" "+r.StatusLine())
	for _, h := range r.Headers {
		out = append(out, "< "+h.Name+": "+val(h))
	}
	out = append(out, "<")
	switch {
	case r.IsBinary:
		out = append(out, "< (binary body, "+FormatSize(r.Size)+")")
	case len(r.Body) > 0:
		for _, l := range strings.Split(strings.TrimSuffix(strings.ReplaceAll(r.Text(), "\r\n", "\n"), "\n"), "\n") {
			out = append(out, "< "+l)
		}
	}
	return out
}
