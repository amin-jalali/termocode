package httpclient

import (
	"fmt"
	"strings"
)

// APIDocs renders a parsed .http file as Markdown API docs: a table of
// the requests, then one section per request (request line, headers,
// body) and the file variables. Secret header values, secret-looking
// variables and secret query values are hidden; {{placeholders}} stay
// as written.
func APIDocs(f File, title string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# API docs: %s\n\n", title)
	if len(f.Requests) == 0 {
		b.WriteString("No requests in this file.\n")
		return b.String()
	}
	n := len(f.Requests)
	if n == 1 {
		b.WriteString("1 request\n\n")
	} else {
		fmt.Fprintf(&b, "%d requests\n\n", n)
	}
	b.WriteString("| Method | Request | URL |\n|---|---|---|\n")
	seen := map[string]int{}
	for i := range f.Requests {
		r := &f.Requests[i]
		a := docsAnchor(r.Label())
		if k := seen[a]; k > 0 {
			seen[a] = k + 1
			a = fmt.Sprintf("%s-%d", a, k)
		} else {
			seen[a] = 1
		}
		fmt.Fprintf(&b, "| `%s` | [%s](#%s) | `%s` |\n", r.Method, docsCell(r.Label()), a, docsCell(RedactURL(r.URL)))
	}
	for i := range f.Requests {
		r := &f.Requests[i]
		fmt.Fprintf(&b, "\n## %s\n\n```http\n%s %s\n```\n", r.Label(), r.Method, RedactURL(r.URL))
		if hs := RedactTemplateHeaders(r.Headers); len(hs) > 0 {
			b.WriteString("\n**Headers**\n\n| Name | Value |\n|---|---|\n")
			for _, h := range hs {
				fmt.Fprintf(&b, "| %s | %s |\n", docsCell(h.Name), docsCell(h.Value))
			}
		}
		if body := strings.TrimRight(r.BodyText(), "\r\n"); strings.TrimSpace(body) != "" {
			lang := ""
			if looksJSON(body) {
				lang = "json"
			}
			fmt.Fprintf(&b, "\n**Body**\n\n```%s\n%s\n```\n", lang, body)
		}
	}
	if len(f.Vars) > 0 {
		b.WriteString("\n## Variables\n\n| Name | Value |\n|---|---|\n")
		for _, v := range f.Vars {
			val := RedactURL(v.Value)
			if SecretHeader(v.Name) {
				val = Redacted
			}
			fmt.Fprintf(&b, "| `%s` | `%s` |\n", docsCell(v.Name), docsCell(val))
		}
	}
	return b.String()
}

// docsCell escapes a Markdown table cell.
func docsCell(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "|", "\\|"), "\n", " ")
}

// docsAnchor is the heading's link id: lower case, spaces become "-",
// other punctuation is dropped.
func docsAnchor(s string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(s) {
		switch {
		case c == ' ':
			b.WriteByte('-')
		case c == '-' || c == '_' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c > 127:
			b.WriteRune(c)
		}
	}
	return b.String()
}

// APIDocsOpenAPI renders an OpenAPI 3.x / Swagger 2.0 file as the same
// Markdown docs (API-23): each operation as a request, its base URL as
// the `baseUrl` variable. The spec title is the page title, else name.
func APIDocsOpenAPI(data []byte, name string) (string, error) {
	im, err := ImportOpenAPI(data)
	if err != nil {
		return "", err
	}
	title := im.Title
	if title == "" {
		title = name
	}
	src := "@baseUrl = " + openapiOneLine(im.Env["baseUrl"]) + "\n" + im.HTTP
	return APIDocs(Parse(src), title), nil
}
