package httpclient

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

// LooksOpenAPI reports whether a .yaml/.yml/.json file is an OpenAPI 3.x
// or Swagger 2.0 document.
func LooksOpenAPI(name string, data []byte) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".yaml", ".yml", ".json":
	default:
		return false
	}
	if len(data) > MaxOpenAPIBytes {
		return false
	}
	doc, err := decodeOpenAPI(data)
	if err != nil {
		return false
	}
	return strings.HasPrefix(str(doc["openapi"]), "3.") || str(doc["swagger"]) == "2.0" || str(doc["swagger"]) == "2"
}

// NewMockOpenAPI builds a mock server from an OpenAPI 3.x or Swagger 2.0
// document (API-22). Every operation answers with its first 2xx response
// (else "default" as 200) and that response's example, or one built from
// its schema. Routes sit under the path of the first server URL
// (Swagger: basePath); `{param}` segments match any segment.
func NewMockOpenAPI(data []byte) (*Mock, error) {
	if len(data) > MaxOpenAPIBytes {
		return nil, errors.New("the file is too large")
	}
	doc, err := decodeOpenAPI(data)
	if err != nil {
		return nil, err
	}
	v2 := false
	switch {
	case strings.HasPrefix(str(doc["openapi"]), "3."):
	case str(doc["swagger"]) == "2.0" || str(doc["swagger"]) == "2":
		v2 = true
	default:
		return nil, errors.New("not an OpenAPI 3.x or Swagger 2.0 file")
	}
	im := &openAPIImporter{doc: doc, v2: v2, vars: map[string]string{}}
	prefix := ""
	if u, err := url.Parse(im.baseURL()); err == nil {
		prefix = strings.TrimRight(u.Path, "/")
	}
	m := &Mock{done: make(chan struct{})}
	paths := obj(doc["paths"])
	for _, p := range sortedKeys(paths) {
		item := im.deref(obj(paths[p]))
		for _, meth := range openAPIMethods {
			op, ok := item[meth].(map[string]any)
			if !ok {
				continue
			}
			rt := MockRoute{Method: strings.ToUpper(meth), Status: http.StatusOK}
			rt.Path = prefix + pathParamRe.ReplaceAllStringFunc(openapiOneLine(p), func(s string) string {
				return ":" + s[1:len(s)-1]
			})
			if !strings.HasPrefix(rt.Path, "/") {
				rt.Path = "/" + rt.Path
			}
			rt.Status, rt.Type, rt.Body = im.mockResponse(op)
			m.routes = append(m.routes, rt)
		}
	}
	if len(m.routes) == 0 {
		return nil, errors.New("the file has no operations under paths")
	}
	return m, nil
}

// mockResponse picks the status, content type and body of op's answer.
func (im *openAPIImporter) mockResponse(op map[string]any) (int, string, string) {
	resps := obj(op["responses"])
	code, status := "", http.StatusOK
	for _, k := range sortedKeys(resps) {
		if strings.HasPrefix(k, "2") {
			code = k
			if n, err := strconv.Atoi(k); err == nil {
				status = n
			}
			break
		}
	}
	if code == "" {
		if _, ok := resps["default"]; !ok {
			return status, "", ""
		}
		code = "default"
	}
	resp := im.deref(obj(resps[code]))
	var ctype string
	var ex any
	if im.v2 {
		ctype = "application/json"
		if c, ok := op["produces"].([]any); ok && len(c) > 0 {
			ctype = openapiOneLine(str(c[0]))
		} else if c, ok := im.doc["produces"].([]any); ok && len(c) > 0 {
			ctype = openapiOneLine(str(c[0]))
		}
		if v, ok := obj(resp["examples"])[ctype]; ok {
			ex = v
		} else if s := obj(resp["schema"]); len(s) > 0 {
			ex = im.example(im.deref(s), 0, map[string]bool{})
		}
	} else {
		content := obj(resp["content"])
		if len(content) == 0 {
			return status, "", ""
		}
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
		if v, ok := mt["example"]; ok {
			ex = v
		} else if exs := obj(mt["examples"]); len(exs) > 0 {
			ex = im.deref(obj(exs[sortedKeys(exs)[0]]))["value"]
		} else {
			ex = im.example(im.deref(obj(mt["schema"])), 0, map[string]bool{})
		}
		ctype = openapiOneLine(ctype)
	}
	if ex == nil {
		return status, "", ""
	}
	if strings.Contains(ctype, "json") {
		bs, err := json.Marshal(ex)
		if err != nil {
			return status, "", ""
		}
		return status, ctype, string(bs)
	}
	return status, ctype, openapiScalar(ex)
}
