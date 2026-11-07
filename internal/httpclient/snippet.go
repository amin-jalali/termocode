package httpclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// SnippetLang is one language Copy as Code… can write.
type SnippetLang struct {
	ID    string
	Title string
}

// SnippetLangs are the snippet languages, in picker order.
var SnippetLangs = []SnippetLang{
	{"curl", "curl"},
	{"go", "Go (net/http)"},
	{"python", "Python (requests)"},
	{"fetch", "JavaScript (fetch)"},
	{"axios", "Node.js (axios)"},
	{"httpie", "HTTPie"},
	{"powershell", "PowerShell (Invoke-WebRequest)"},
}

// ToSnippet writes the request as code in lang (a SnippetLangs id).
// Secrets are handled like ToCurl: without withSecrets, a value that
// holds a private / $secret value is written as its {{template}}.
func ToSnippet(lang string, rr Resolved, withSecrets bool) (string, error) {
	if lang == "curl" {
		return ToCurl(rr, withSecrets), nil
	}
	r := shareable(rr, withSecrets)
	switch lang {
	case "go":
		return goSnippet(r), nil
	case "python":
		return pythonSnippet(r), nil
	case "fetch":
		return fetchSnippet(r), nil
	case "axios":
		return axiosSnippet(r), nil
	case "httpie":
		return httpieSnippet(r), nil
	case "powershell":
		return powershellSnippet(r), nil
	}
	return "", fmt.Errorf("unknown snippet language %q", lang)
}

// codeQuote is s as a double-quoted string literal that Go, Python and
// JavaScript all read the same way (JSON string rules).
func codeQuote(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}

func goSnippet(r Request) string {
	var b strings.Builder
	b.WriteString("package main\n\nimport (\n\t\"fmt\"\n\t\"io\"\n\t\"net/http\"\n")
	if r.Body != nil {
		b.WriteString("\t\"strings\"\n")
	}
	b.WriteString(")\n\nfunc main() {\n")
	body := "nil"
	if r.Body != nil {
		b.WriteString("\tbody := strings.NewReader(" + codeQuote(*r.Body) + ")\n")
		body = "body"
	}
	b.WriteString("\treq, err := http.NewRequest(" + codeQuote(r.Method) + ", " + codeQuote(r.URL) + ", " + body + ")\n")
	b.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n")
	for _, h := range r.Headers {
		b.WriteString("\treq.Header.Add(" + codeQuote(h.Name) + ", " + codeQuote(h.Value) + ")\n")
	}
	b.WriteString("\tresp, err := http.DefaultClient.Do(req)\n")
	b.WriteString("\tif err != nil {\n\t\tpanic(err)\n\t}\n")
	b.WriteString("\tdefer resp.Body.Close()\n")
	b.WriteString("\tout, _ := io.ReadAll(resp.Body)\n")
	b.WriteString("\tfmt.Println(resp.Status)\n\tfmt.Println(string(out))\n}\n")
	return b.String()
}

func pythonSnippet(r Request) string {
	var b strings.Builder
	b.WriteString("import requests\n\nurl = " + codeQuote(r.URL) + "\n")
	b.WriteString("headers = {\n")
	for _, h := range r.Headers {
		b.WriteString("    " + codeQuote(h.Name) + ": " + codeQuote(h.Value) + ",\n")
	}
	b.WriteString("}\n")
	call := "response = requests.request(" + codeQuote(r.Method) + ", url, headers=headers"
	if r.Body != nil {
		b.WriteString("data = " + codeQuote(*r.Body) + "\n")
		call += ", data=data"
	}
	b.WriteString("\n" + call + ")\n")
	b.WriteString("print(response.status_code)\nprint(response.text)\n")
	return b.String()
}

// jsHeaders writes a headers object with the given indent ("" = none).
func jsHeaders(b *strings.Builder, key, indent string, hs []Header) {
	if len(hs) == 0 {
		return
	}
	b.WriteString(indent + key + ": {\n")
	for _, h := range hs {
		b.WriteString(indent + "  " + codeQuote(h.Name) + ": " + codeQuote(h.Value) + ",\n")
	}
	b.WriteString(indent + "},\n")
}

func fetchSnippet(r Request) string {
	var b strings.Builder
	b.WriteString("const response = await fetch(" + codeQuote(r.URL) + ", {\n")
	b.WriteString("  method: " + codeQuote(r.Method) + ",\n")
	jsHeaders(&b, "headers", "  ", r.Headers)
	if r.Body != nil {
		b.WriteString("  body: " + codeQuote(*r.Body) + ",\n")
	}
	b.WriteString("});\nconsole.log(response.status);\nconsole.log(await response.text());\n")
	return b.String()
}

func axiosSnippet(r Request) string {
	var b strings.Builder
	b.WriteString("const axios = require(\"axios\");\n\naxios.request({\n")
	b.WriteString("  method: " + codeQuote(r.Method) + ",\n")
	b.WriteString("  url: " + codeQuote(r.URL) + ",\n")
	jsHeaders(&b, "headers", "  ", r.Headers)
	if r.Body != nil {
		b.WriteString("  data: " + codeQuote(*r.Body) + ",\n")
	}
	b.WriteString("}).then((response) => {\n  console.log(response.status);\n  console.log(response.data);\n});\n")
	return b.String()
}

func httpieSnippet(r Request) string {
	var b strings.Builder
	b.WriteString("http " + shellWord(r.Method) + " " + shellQuote(r.URL))
	for _, h := range r.Headers {
		b.WriteString(" " + shellQuote(h.Name+":"+h.Value))
	}
	if r.Body != nil {
		b.WriteString(" --raw " + shellQuote(*r.Body))
	}
	return b.String()
}

// psQuote is s as a PowerShell single-quoted string (” for a quote).
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func powershellSnippet(r Request) string {
	var b strings.Builder
	contentType := ""
	var hs []Header
	for _, h := range r.Headers {
		// Invoke-WebRequest takes the content type as its own parameter.
		if strings.EqualFold(h.Name, "Content-Type") {
			contentType = h.Value
			continue
		}
		hs = append(hs, h)
	}
	call := "$response = Invoke-WebRequest -Uri " + psQuote(r.URL) + " -Method " + psQuote(r.Method)
	if len(hs) > 0 {
		b.WriteString("$headers = @{\n")
		for _, h := range hs {
			b.WriteString("    " + psQuote(h.Name) + " = " + psQuote(h.Value) + "\n")
		}
		b.WriteString("}\n")
		call += " -Headers $headers"
	}
	if contentType != "" {
		call += " -ContentType " + psQuote(contentType)
	}
	if r.Body != nil {
		b.WriteString("$body = " + psQuote(*r.Body) + "\n")
		call += " -Body $body"
	}
	b.WriteString(call + "\n$response.StatusCode\n$response.Content\n")
	return b.String()
}
