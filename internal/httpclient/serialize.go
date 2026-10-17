package httpclient

import "strings"

// Serialize writes requests as `.http` text, byte-equal with mobocode's
// serializeHttpFile: `###` between requests, a `### Name` line for
// named ones, headers in order, a blank line then the body, and a
// newline after a body that lacks one. File variables and directives
// are not written (mobocode would read `@x = y` as a request).
func Serialize(reqs []Request) string {
	var b strings.Builder
	for i := range reqs {
		r := &reqs[i]
		if i > 0 {
			b.WriteString("###\n")
		}
		if r.Name != "" {
			b.WriteString("### " + r.Name + "\n")
		}
		b.WriteString(r.Method + " " + r.URL + "\n")
		for _, h := range r.Headers {
			b.WriteString(h.Name + ": " + h.Value + "\n")
		}
		if r.Body != nil && *r.Body != "" {
			b.WriteString("\n")
			b.WriteString(*r.Body)
			if !strings.HasSuffix(*r.Body, "\n") {
				b.WriteString("\n")
			}
		}
	}
	return b.String()
}
