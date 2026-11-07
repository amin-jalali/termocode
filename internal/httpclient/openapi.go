package httpclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	yaml "go.yaml.in/yaml/v3"
)

// MaxOpenAPIBytes caps the spec file an import reads.
const MaxOpenAPIBytes = 8 << 20

// OpenAPIImport is an OpenAPI / Swagger document turned into `.http`
// text: one request per operation.
type OpenAPIImport struct {
	Title string
	// HTTP is the `.http` file: file variables for path parameters,
	// then one `### name` block per operation.
	HTTP string
	// Env holds the non-secret environment values (baseUrl) for the
	// `$shared` section of http-client.env.json. Secret placeholders
	// ({{token}}, {{apiKey}}, {{basicAuth}}) never get a value here.
	Env map[string]string
	// Count is the number of requests in HTTP.
	Count int
}

var openAPIMethods = []string{"get", "post", "put", "patch", "delete", "head", "options", "trace"}

// ImportOpenAPI reads an OpenAPI 3.x or Swagger 2.0 document (JSON or
// YAML). The document is untrusted: every value it puts into the
// output is kept on one line, so it cannot add requests or headers.
func ImportOpenAPI(data []byte) (OpenAPIImport, error) {
	var out OpenAPIImport
	if len(data) > MaxOpenAPIBytes {
		return out, fmt.Errorf("the file is larger than %d MiB", MaxOpenAPIBytes>>20)
	}
	doc, err := decodeOpenAPI(data)
	if err != nil {
		return out, err
	}
	v2 := false
	switch {
	case strings.HasPrefix(str(doc["openapi"]), "3."):
	case str(doc["swagger"]) == "2.0" || str(doc["swagger"]) == "2":
		v2 = true
	default:
		return out, errors.New("not an OpenAPI 3.x or Swagger 2.0 file (no openapi: 3.x or swagger: \"2.0\" key)")
	}
	im := &openAPIImporter{doc: doc, v2: v2, vars: map[string]string{}}
	out.Title = openapiOneLine(str(obj(doc["info"])["title"]))
	out.Env = map[string]string{"baseUrl": im.baseURL()}

	paths := obj(doc["paths"])
	keys := sortedKeys(paths)
	var blocks []string
	for _, p := range keys {
		item := im.deref(obj(paths[p]))
		for _, meth := range openAPIMethods {
			op, ok := item[meth].(map[string]any)
			if !ok {
				continue
			}
			blocks = append(blocks, im.request(p, meth, item, op))
		}
	}
	if len(blocks) == 0 {
		return out, errors.New("the file has no operations under paths")
	}
	out.Count = len(blocks)

	var b strings.Builder
	b.WriteString("# Imported from OpenAPI")
	if out.Title != "" {
		b.WriteString(": " + out.Title)
	}
	b.WriteString("\n# {{baseUrl}} comes from http-client.env.json.\n")
	if im.secrets {
		b.WriteString("# Put token / apiKey / basicAuth in http-client.private.env.json (never in this file).\n")
	}
	if len(im.vars) > 0 {
		b.WriteString("\n")
		for _, k := range sortedKeys(im.vars) {
			b.WriteString("@" + k + " = " + im.vars[k] + "\n")
		}
	}
	for _, bl := range blocks {
		b.WriteString("\n" + bl)
	}
	out.HTTP = b.String()
	return out, nil
}

func decodeOpenAPI(data []byte) (map[string]any, error) {
	t := bytes.TrimSpace(data)
	if len(t) == 0 {
		return nil, errors.New("the file is empty")
	}
	var v any
	if t[0] == '{' {
		if err := json.Unmarshal(t, &v); err != nil {
			return nil, fmt.Errorf("bad JSON: %w", err)
		}
	} else if err := yaml.Unmarshal(t, &v); err != nil {
		return nil, fmt.Errorf("bad YAML: %w", err)
	}
	m, ok := normYAML(v).(map[string]any)
	if !ok {
		return nil, errors.New("the file is not an object")
	}
	return m, nil
}

// normYAML turns map[any]any (non-string YAML keys) into map[string]any.
func normYAML(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			x[k] = normYAML(e)
		}
		return x
	case map[any]any:
		m := make(map[string]any, len(x))
		for k, e := range x {
			m[fmt.Sprint(k)] = normYAML(e)
		}
		return m
	case []any:
		for i, e := range x {
			x[i] = normYAML(e)
		}
		return x
	}
	return v
}

type openAPIImporter struct {
	doc     map[string]any
	v2      bool
	vars    map[string]string // path parameter file variables
	secrets bool              // a secret placeholder was written
}

func (im *openAPIImporter) baseURL() string {
	if im.v2 {
		host := openapiOneLine(str(im.doc["host"]))
		if host == "" {
			host = "localhost"
		}
		scheme := "https"
		if s, ok := im.doc["schemes"].([]any); ok && len(s) > 0 && str(s[0]) != "" {
			scheme = openapiOneLine(str(s[0]))
		}
		return strings.TrimRight(scheme+"://"+host+openapiOneLine(str(im.doc["basePath"])), "/")
	}
	if s, ok := im.doc["servers"].([]any); ok && len(s) > 0 {
		srv := obj(s[0])
		u := openapiOneLine(str(srv["url"]))
		for name, v := range obj(srv["variables"]) {
			u = strings.ReplaceAll(u, "{"+name+"}", openapiOneLine(str(obj(v)["default"])))
		}
		if u != "" && u != "/" {
			return strings.TrimRight(u, "/")
		}
	}
	return "http://localhost"
}

// deref follows a local "$ref" (a few hops; a loop ends as {}).
func (im *openAPIImporter) deref(m map[string]any) map[string]any {
	for range 16 {
		ref, ok := m["$ref"].(string)
		if !ok {
			return m
		}
		m = obj(im.pointer(ref))
	}
	return map[string]any{}
}

func (im *openAPIImporter) pointer(ref string) any {
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}
	var cur any = im.doc
	for _, part := range strings.Split(ref[2:], "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[part]
	}
	return cur
}

type openAPIParam struct {
	name, in string
	required bool
	p        map[string]any
}

// params merges path-level and operation-level parameters (the
// operation wins on the same name + in).
func (im *openAPIImporter) params(item, op map[string]any) []openAPIParam {
	var out []openAPIParam
	idx := map[string]int{}
	for _, src := range []any{item["parameters"], op["parameters"]} {
		list, _ := src.([]any)
		for _, e := range list {
			p := im.deref(obj(e))
			pp := openAPIParam{name: str(p["name"]), in: str(p["in"]), required: p["required"] == true, p: p}
			if pp.name == "" {
				continue
			}
			k := pp.in + "\x00" + pp.name
			if i, ok := idx[k]; ok {
				out[i] = pp
				continue
			}
			idx[k] = len(out)
			out = append(out, pp)
		}
	}
	return out
}

var pathParamRe = regexp.MustCompile(`\{([^{}]+)\}`)

func (im *openAPIImporter) request(path, meth string, item, op map[string]any) string {
	params := im.params(item, op)
	name := openapiOneLine(str(op["operationId"]))
	if name == "" {
		name = strings.ToUpper(meth) + " " + openapiOneLine(path)
	}
	var b strings.Builder
	b.WriteString("### " + name + "\n")
	for _, k := range []string{"summary", "description"} {
		if s := openapiOneLine(str(op[k])); s != "" {
			b.WriteString("# " + s + "\n")
			break
		}
	}

	byPath := map[string]openAPIParam{}
	for _, p := range params {
		if p.in == "path" {
			byPath[p.name] = p
		}
	}
	u := pathParamRe.ReplaceAllStringFunc(openapiOneLine(path), func(s string) string {
		raw := s[1 : len(s)-1]
		v := openapiVarName(raw)
		if _, ok := im.vars[v]; !ok {
			ex := ""
			if p, ok := byPath[raw]; ok {
				ex = im.paramExample(p)
			}
			if ex == "" {
				ex = "1"
			}
			im.vars[v] = ex
		}
		return "{{" + v + "}}"
	})
	u = "{{baseUrl}}" + strings.ReplaceAll(u, " ", "%20")
	var q []string
	for _, p := range params {
		if p.in == "query" && p.required {
			q = append(q, url.QueryEscape(openapiOneLine(p.name))+"="+url.QueryEscape(im.paramExample(p)))
		}
	}
	if len(q) > 0 {
		u += "?" + strings.Join(q, "&")
	}
	b.WriteString(strings.ToUpper(meth) + " " + u + "\n")

	var headers []Header
	for _, p := range params {
		if p.in == "header" && p.required {
			if hn := headerName(p.name); hn != "" {
				headers = append(headers, Header{hn, im.paramExample(p)})
			}
		}
	}
	headers = append(headers, im.authHeaders(op)...)
	ctype, body := im.body(op, params)
	if ctype != "" {
		headers = append(headers, Header{"Content-Type", ctype})
	}
	for _, h := range headers {
		b.WriteString(h.Name + ": " + openapiOneLine(h.Value) + "\n")
	}
	if body != "" {
		b.WriteString("\n" + body + "\n")
	}
	return b.String()
}

func (im *openAPIImporter) paramExample(p openAPIParam) string {
	if v, ok := p.p["example"]; ok {
		return openapiOneLine(openapiScalar(v))
	}
	sch := im.deref(obj(p.p["schema"]))
	if len(sch) == 0 { // Swagger 2.0 puts type on the parameter
		sch = p.p
	}
	return openapiOneLine(openapiScalar(im.example(sch, 0, map[string]bool{})))
}

func (im *openAPIImporter) authHeaders(op map[string]any) []Header {
	sec, ok := op["security"]
	if !ok {
		sec = im.doc["security"]
	}
	list, _ := sec.([]any)
	if len(list) == 0 {
		return nil
	}
	req := obj(list[0])
	schemes := obj(obj(im.doc["components"])["securitySchemes"])
	if im.v2 {
		schemes = obj(im.doc["securityDefinitions"])
	}
	var out []Header
	for _, n := range sortedKeys(req) {
		s := im.deref(obj(schemes[n]))
		switch t := str(s["type"]); {
		case t == "basic" || t == "http" && strings.EqualFold(str(s["scheme"]), "basic"):
			out = append(out, Header{"Authorization", "Basic {{basicAuth}}"})
		case t == "http" || t == "oauth2" || t == "openIdConnect":
			out = append(out, Header{"Authorization", "Bearer {{token}}"})
		case t == "apiKey" && str(s["in"]) == "header":
			if hn := headerName(str(s["name"])); hn != "" {
				out = append(out, Header{hn, "{{apiKey}}"})
			}
		default:
			continue
		}
		im.secrets = true
	}
	return out
}

// body is the request's Content-Type and example body ("" = none).
func (im *openAPIImporter) body(op map[string]any, params []openAPIParam) (string, string) {
	if im.v2 {
		for _, p := range params {
			if p.in == "body" {
				ctype := "application/json"
				if c, ok := op["consumes"].([]any); ok && len(c) > 0 {
					ctype = openapiOneLine(str(c[0]))
				} else if c, ok := im.doc["consumes"].([]any); ok && len(c) > 0 {
					ctype = openapiOneLine(str(c[0]))
				}
				return ctype, im.render(ctype, im.example(im.deref(obj(p.p["schema"])), 0, map[string]bool{}))
			}
		}
		return "", ""
	}
	rb := im.deref(obj(op["requestBody"]))
	content := obj(rb["content"])
	if len(content) == 0 {
		return "", ""
	}
	ctype := ""
	for _, c := range sortedKeys(content) {
		if strings.Contains(c, "json") {
			ctype = c
			break
		}
	}
	if ctype == "" {
		ctype = sortedKeys(content)[0]
	}
	mt := obj(content[ctype])
	var ex any
	if v, ok := mt["example"]; ok {
		ex = v
	} else if exs := obj(mt["examples"]); len(exs) > 0 {
		ex = im.deref(obj(exs[sortedKeys(exs)[0]]))["value"]
	} else {
		ex = im.example(im.deref(obj(mt["schema"])), 0, map[string]bool{})
	}
	return openapiOneLine(ctype), im.render(ctype, ex)
}

// render writes an example body. JSON is indented; anything else is one
// line. A line that starts with "###" is indented so it cannot start a
// new request.
func (im *openAPIImporter) render(ctype string, v any) string {
	if v == nil {
		return ""
	}
	var s string
	if strings.Contains(ctype, "json") {
		bs, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return ""
		}
		s = string(bs)
	} else {
		s = openapiOneLine(openapiScalar(v))
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "###") {
			lines[i] = " " + l
		}
	}
	return strings.Join(lines, "\n")
}

// example builds a value from a schema: example, default, enum, then
// the type. refs on the current path stop a loop.
func (im *openAPIImporter) example(s map[string]any, depth int, seen map[string]bool) any {
	if depth > 8 {
		return nil
	}
	if ref, ok := s["$ref"].(string); ok {
		if seen[ref] {
			return nil
		}
		seen[ref] = true
		defer delete(seen, ref)
		return im.example(obj(im.pointer(ref)), depth+1, seen)
	}
	for _, k := range []string{"example", "default"} {
		if v, ok := s[k]; ok {
			return v
		}
	}
	if e, ok := s["enum"].([]any); ok && len(e) > 0 {
		return e[0]
	}
	if all, ok := s["allOf"].([]any); ok {
		m := map[string]any{}
		for _, e := range all {
			if part, ok := im.example(obj(e), depth+1, seen).(map[string]any); ok {
				for k, v := range part {
					m[k] = v
				}
			}
		}
		return m
	}
	for _, k := range []string{"oneOf", "anyOf"} {
		if l, ok := s[k].([]any); ok && len(l) > 0 {
			return im.example(obj(l[0]), depth+1, seen)
		}
	}
	t := str(s["type"])
	if l, ok := s["type"].([]any); ok && len(l) > 0 { // 3.1: type: [string, "null"]
		t = str(l[0])
	}
	if t == "" {
		if _, ok := s["properties"]; ok {
			t = "object"
		} else if _, ok := s["items"]; ok {
			t = "array"
		}
	}
	switch t {
	case "object":
		props := obj(s["properties"])
		m := make(map[string]any, len(props))
		for _, k := range sortedKeys(props) {
			m[k] = im.example(obj(props[k]), depth+1, seen)
		}
		return m
	case "array":
		return []any{im.example(obj(s["items"]), depth+1, seen)}
	case "integer", "number":
		return 0
	case "boolean":
		return true
	case "string":
		switch str(s["format"]) {
		case "date-time":
			return "2024-01-01T00:00:00Z"
		case "date":
			return "2024-01-01"
		case "email":
			return "user@example.com"
		case "uuid":
			return "00000000-0000-0000-0000-000000000000"
		case "uri", "url":
			return "https://example.com"
		}
		return "string"
	}
	return nil
}

// OpenAPIFileName is the `.http` file name for an imported spec title:
// lower-case ASCII letters and digits joined by "-".
func OpenAPIFileName(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
		} else {
			dash = true
		}
	}
	s := b.String()
	if len(s) > 60 {
		s = strings.TrimRight(s[:60], "-")
	}
	if s == "" {
		s = "openapi"
	}
	return s + ".http"
}

// openapiVarName makes a file-variable name ([\w.-]+) from a path parameter.
func openapiVarName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r == '_' || r == '.' || r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "param"
	}
	return b.String()
}

// headerName keeps only HTTP token characters; "" when nothing is left.
func headerName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r > ' ' && r < 0x7f && !strings.ContainsRune(`()<>@,;:\"/[]?={}`, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// openapiOneLine folds every line break (and other control characters) into a
// space.
func openapiOneLine(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r < ' ' || r == 0x7f || r == 0x85 || r == 0x2028 || r == 0x2029 }) {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < ' ' || r == 0x7f || r == 0x85 || r == 0x2028 || r == 0x2029 {
			return ' '
		}
		return r
	}, s))
}

func openapiScalar(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	bs, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(bs)
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	return openapiScalar(v)
}

func obj(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
