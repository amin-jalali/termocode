package httpclient

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Collection formats ImportCollection reads.
const (
	FormatCollection = "collection" // v2.0 / v2.1 collection JSON
	FormatHAR        = "har"        // HAR 1.2 (browser network export)
	FormatInsomnia   = "insomnia"   // Insomnia v4 export
)

const (
	maxImportBytes    = 32 << 20
	maxImportRequests = 5000
	maxImportDepth    = 32
)

// Imported is the result of ImportCollection.
type Imported struct {
	Format   string
	Requests []Request
	// Vars are the collection / environment variables (string, number
	// and bool values only), for `@name = value` lines or an env file.
	Vars []Var
	// Placeholders are the {{names}} that replaced literal secret
	// header values; the user sets them in an env file or secret.
	Placeholders []string
}

// ImportCollection reads a v2.1 collection JSON, a HAR or an Insomnia
// export and returns its requests in .http form. A literal secret
// header value (Authorization, Cookie, tokens) is never copied: it
// becomes a {{placeholder}}.
func ImportCollection(data []byte) (Imported, error) {
	if len(data) > maxImportBytes {
		return Imported{}, fmt.Errorf("file too large to import (over %d MiB)", maxImportBytes>>20)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(bytes.TrimSpace(data), &top); err != nil {
		return Imported{}, errors.New("not a JSON object")
	}
	im := &importer{seen: map[string]bool{}}
	var err error
	switch {
	case top["log"] != nil:
		im.out.Format = FormatHAR
		err = im.har(top["log"])
	case top["resources"] != nil:
		im.out.Format = FormatInsomnia
		err = im.insomnia(top["resources"])
	case top["item"] != nil:
		im.out.Format = FormatCollection
		err = im.collection(top)
	default:
		return Imported{}, errors.New("unknown format: expected a collection JSON, a HAR or an Insomnia export")
	}
	if err != nil {
		return Imported{}, err
	}
	if len(im.out.Requests) == 0 {
		return Imported{}, errors.New("no requests found")
	}
	return im.out, nil
}

type importer struct {
	out  Imported
	seen map[string]bool // placeholders already listed
}

func (im *importer) add(r Request) error {
	if len(im.out.Requests) >= maxImportRequests {
		return fmt.Errorf("too many requests (over %d)", maxImportRequests)
	}
	r.Name = oneLine(r.Name)
	r.URL = oneLine(r.URL)
	if r.URL == "" {
		return nil
	}
	m := strings.ToUpper(strings.TrimSpace(r.Method))
	if !isMethodWord(m) {
		m = "GET"
	}
	r.Method = m
	for i, h := range r.Headers {
		h.Name, h.Value = oneLine(h.Name), oneLine(h.Value)
		r.Headers[i] = im.hideSecret(h)
	}
	if r.Body != nil && *r.Body == "" {
		r.Body = nil
	}
	im.out.Requests = append(im.out.Requests, r)
	return nil
}

// hideSecret turns a literal secret header value into a placeholder;
// values that are only placeholders ("Bearer {{token}}") stay.
func (im *importer) hideSecret(h Header) Header {
	if !SecretHeader(h.Name) || onlyPlaceholders(h.Value) {
		return h
	}
	name := varName(h.Name)
	ph := "{{" + name + "}}"
	switch strings.ToLower(h.Name) {
	case "authorization", "proxy-authorization":
		if scheme, _, ok := strings.Cut(strings.TrimSpace(h.Value), " "); ok && isMethodWord(strings.ToUpper(scheme)) {
			ph = scheme + " " + ph
		}
	}
	if !im.seen[name] {
		im.seen[name] = true
		im.out.Placeholders = append(im.out.Placeholders, name)
	}
	return Header{Name: h.Name, Value: ph}
}

func (im *importer) addVar(name, value string) {
	name = strings.TrimSpace(name)
	if !isPlainName(name) {
		return
	}
	im.out.Vars = append(im.out.Vars, Var{Name: name, Value: oneLine(value)})
}

// ---- v2.0 / v2.1 collection ----

type pmKV struct {
	Key      string          `json:"key"`
	Value    json.RawMessage `json:"value"`
	Disabled bool            `json:"disabled"`
}

type pmAuth struct {
	Type   string `json:"type"`
	Bearer []pmKV `json:"bearer"`
	Basic  []pmKV `json:"basic"`
	APIKey []pmKV `json:"apikey"`
}

type pmItem struct {
	Name    string          `json:"name"`
	Item    []pmItem        `json:"item"`
	Request json.RawMessage `json:"request"`
	Auth    *pmAuth         `json:"auth"`
}

type pmRequest struct {
	Method string          `json:"method"`
	URL    json.RawMessage `json:"url"`
	Header []pmKV          `json:"header"`
	Body   *struct {
		Mode       string `json:"mode"`
		Raw        string `json:"raw"`
		URLEncoded []pmKV `json:"urlencoded"`
		GraphQL    *struct {
			Query     string          `json:"query"`
			Variables json.RawMessage `json:"variables"`
		} `json:"graphql"`
	} `json:"body"`
	Auth *pmAuth `json:"auth"`
}

func (im *importer) collection(top map[string]json.RawMessage) error {
	var items []pmItem
	if err := json.Unmarshal(top["item"], &items); err != nil {
		return errors.New("bad collection: item is not a list")
	}
	var auth *pmAuth
	if a := top["auth"]; a != nil {
		_ = json.Unmarshal(a, &auth)
	}
	var vars []pmKV
	if v := top["variable"]; v != nil {
		_ = json.Unmarshal(v, &vars)
	}
	for _, v := range vars {
		if !v.Disabled {
			im.addVar(v.Key, scalar(v.Value))
		}
	}
	return im.items(items, "", auth, 0)
}

func (im *importer) items(items []pmItem, prefix string, auth *pmAuth, depth int) error {
	if depth > maxImportDepth {
		return errors.New("folders nested too deep")
	}
	for _, it := range items {
		a := auth
		if it.Auth != nil {
			a = it.Auth
		}
		name := strings.TrimSpace(it.Name)
		if prefix != "" {
			name = prefix + " / " + name
		}
		if it.Request == nil {
			if err := im.items(it.Item, name, a, depth+1); err != nil {
				return err
			}
			continue
		}
		r, err := pmToRequest(it.Request, a)
		if err != nil {
			continue
		}
		r.Name = name
		if err := im.add(r); err != nil {
			return err
		}
	}
	return nil
}

func pmToRequest(raw json.RawMessage, inherited *pmAuth) (Request, error) {
	var s string
	if json.Unmarshal(raw, &s) == nil { // a bare URL
		return Request{Method: "GET", URL: s}, nil
	}
	var pr pmRequest
	if err := json.Unmarshal(raw, &pr); err != nil {
		return Request{}, err
	}
	r := Request{Method: pr.Method, URL: pmURL(pr.URL)}
	for _, h := range pr.Header {
		if !h.Disabled && strings.TrimSpace(h.Key) != "" {
			r.Headers = append(r.Headers, Header{Name: h.Key, Value: scalar(h.Value)})
		}
	}
	if b := pr.Body; b != nil {
		switch b.Mode {
		case "raw":
			r.Body = &b.Raw
		case "urlencoded":
			var parts []string
			for _, kv := range b.URLEncoded {
				if !kv.Disabled {
					parts = append(parts, qEscape(kv.Key)+"="+qEscape(scalar(kv.Value)))
				}
			}
			body := strings.Join(parts, "&")
			r.Body = &body
			setDefaultHeader(&r, "Content-Type", "application/x-www-form-urlencoded")
		case "graphql":
			if b.GraphQL != nil {
				m := map[string]any{"query": b.GraphQL.Query}
				var v any
				if s, isText := scalarOK(b.GraphQL.Variables); isText {
					// Postman keeps the variables as JSON text.
					if t := strings.TrimSpace(s); t != "" && json.Unmarshal([]byte(t), &v) == nil && v != nil {
						m["variables"] = v
					}
				} else if json.Unmarshal(b.GraphQL.Variables, &v) == nil && v != nil {
					m["variables"] = v
				}
				out, _ := json.Marshal(m)
				body := string(out)
				r.Body = &body
				setDefaultHeader(&r, "Content-Type", "application/json")
			}
		}
	}
	a := inherited
	if pr.Auth != nil {
		a = pr.Auth
	}
	if a != nil && !r.HasHeader("Authorization") {
		switch a.Type {
		case "bearer":
			r.Headers = append(r.Headers, Header{Name: "Authorization", Value: "Bearer " + kvGet(a.Bearer, "token")})
		case "basic":
			r.Headers = append(r.Headers, Header{Name: "Authorization", Value: "Basic " + kvGet(a.Basic, "username") + " " + kvGet(a.Basic, "password")})
		case "apikey":
			if kvGet(a.APIKey, "in") != "query" {
				if k := kvGet(a.APIKey, "key"); k != "" {
					r.Headers = append(r.Headers, Header{Name: k, Value: kvGet(a.APIKey, "value")})
				}
			}
		}
	}
	return r, nil
}

func pmURL(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var u struct {
		Raw string `json:"raw"`
	}
	_ = json.Unmarshal(raw, &u)
	return u.Raw
}

func kvGet(kvs []pmKV, key string) string {
	for _, kv := range kvs {
		if kv.Key == key {
			return scalar(kv.Value)
		}
	}
	return ""
}

// ---- HAR ----

type harNV struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type harLog struct {
	Version string `json:"version"`
	Creator struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"creator"`
	Entries []harEntry `json:"entries"`
}

type harEntry struct {
	Started  string      `json:"startedDateTime"`
	Time     int         `json:"time"`
	Request  harRequest  `json:"request"`
	Response harResponse `json:"response"`
	Cache    struct{}    `json:"cache"`
	Timings  struct {
		Send    int `json:"send"`
		Wait    int `json:"wait"`
		Receive int `json:"receive"`
	} `json:"timings"`
	Comment string `json:"comment,omitempty"`
}

type harRequest struct {
	Method      string   `json:"method"`
	URL         string   `json:"url"`
	HTTPVersion string   `json:"httpVersion"`
	Cookies     []harNV  `json:"cookies"`
	Headers     []harNV  `json:"headers"`
	QueryString []harNV  `json:"queryString"`
	PostData    *harPost `json:"postData,omitempty"`
	HeadersSize int      `json:"headersSize"`
	BodySize    int      `json:"bodySize"`
}

type harPost struct {
	MimeType string  `json:"mimeType"`
	Text     string  `json:"text"`
	Params   []harNV `json:"params,omitempty"`
}

type harResponse struct {
	Status      int     `json:"status"`
	StatusText  string  `json:"statusText"`
	HTTPVersion string  `json:"httpVersion"`
	Cookies     []harNV `json:"cookies"`
	Headers     []harNV `json:"headers"`
	Content     struct {
		Size     int    `json:"size"`
		MimeType string `json:"mimeType"`
	} `json:"content"`
	RedirectURL string `json:"redirectURL"`
	HeadersSize int    `json:"headersSize"`
	BodySize    int    `json:"bodySize"`
}

// harSkipHeaders are set by the HTTP engine, not by the request.
var harSkipHeaders = map[string]bool{"content-length": true, "host": true, "connection": true}

func (im *importer) har(raw json.RawMessage) error {
	var log harLog
	if err := json.Unmarshal(raw, &log); err != nil || log.Entries == nil {
		return errors.New("bad HAR: no entries")
	}
	for _, e := range log.Entries {
		r := Request{Method: e.Request.Method, URL: e.Request.URL, Name: e.Comment}
		for _, h := range e.Request.Headers {
			n := strings.TrimSpace(h.Name)
			if n == "" || strings.HasPrefix(n, ":") || harSkipHeaders[strings.ToLower(n)] {
				continue
			}
			r.Headers = append(r.Headers, Header{Name: n, Value: h.Value})
		}
		if p := e.Request.PostData; p != nil {
			body := p.Text
			if body == "" && len(p.Params) > 0 {
				parts := make([]string, 0, len(p.Params))
				for _, kv := range p.Params {
					parts = append(parts, qEscape(kv.Name)+"="+qEscape(kv.Value))
				}
				body = strings.Join(parts, "&")
			}
			r.Body = &body
			if p.MimeType != "" {
				setDefaultHeader(&r, "Content-Type", p.MimeType)
			}
		}
		if err := im.add(r); err != nil {
			return err
		}
	}
	return nil
}

// ExportHAR writes requests as a HAR 1.2 log (no responses). Secret
// header values that are not only placeholders are redacted, and so
// are secret query values in the URL.
func ExportHAR(reqs []Request, now time.Time) ([]byte, error) {
	log := harLog{Version: "1.2", Entries: []harEntry{}}
	log.Creator.Name = "termocode"
	log.Creator.Version = "1"
	for _, r := range reqs {
		u := RedactURL(r.URL)
		hr := harRequest{Method: r.Method, URL: u, HTTPVersion: "HTTP/1.1", Cookies: []harNV{},
			Headers: []harNV{}, QueryString: []harNV{}, HeadersSize: -1, BodySize: len(r.BodyText())}
		for _, h := range RedactTemplateHeaders(r.Headers) {
			hr.Headers = append(hr.Headers, harNV{Name: h.Name, Value: h.Value})
		}
		if _, q, ok := strings.Cut(u, "?"); ok {
			q, _, _ = strings.Cut(q, "#")
			for _, part := range strings.Split(q, "&") {
				if part == "" {
					continue
				}
				k, v, _ := strings.Cut(part, "=")
				if dk, err := url.QueryUnescape(k); err == nil {
					k = dk
				}
				if dv, err := url.QueryUnescape(v); err == nil {
					v = dv
				}
				hr.QueryString = append(hr.QueryString, harNV{Name: k, Value: v})
			}
		}
		if r.Body != nil && *r.Body != "" {
			hr.PostData = &harPost{MimeType: r.Header("Content-Type"), Text: *r.Body}
		}
		e := harEntry{Started: now.UTC().Format(time.RFC3339Nano), Request: hr, Comment: r.Name}
		e.Response.Cookies, e.Response.Headers = []harNV{}, []harNV{}
		e.Response.HeadersSize, e.Response.BodySize = -1, -1
		log.Entries = append(log.Entries, e)
	}
	return json.MarshalIndent(map[string]any{"log": log}, "", "  ")
}

// ---- Insomnia v4 ----

type insResource struct {
	ID       string          `json:"_id"`
	Type     string          `json:"_type"`
	ParentID string          `json:"parentId"`
	Name     string          `json:"name"`
	Method   string          `json:"method"`
	URL      string          `json:"url"`
	Data     json.RawMessage `json:"data"`
	Body     *struct {
		MimeType string `json:"mimeType"`
		Text     string `json:"text"`
		Params   []struct {
			Name     string `json:"name"`
			Value    string `json:"value"`
			Disabled bool   `json:"disabled"`
		} `json:"params"`
	} `json:"body"`
	Headers []struct {
		Name     string `json:"name"`
		Value    string `json:"value"`
		Disabled bool   `json:"disabled"`
	} `json:"headers"`
	Parameters []struct {
		Name     string `json:"name"`
		Value    string `json:"value"`
		Disabled bool   `json:"disabled"`
	} `json:"parameters"`
	Auth *struct {
		Type     string `json:"type"`
		Token    string `json:"token"`
		Prefix   string `json:"prefix"`
		Username string `json:"username"`
		Password string `json:"password"`
		Disabled bool   `json:"disabled"`
	} `json:"authentication"`
}

// insVars rewrites Insomnia {{ _.name }} templates to {{name}}.
func insVars(s string) string {
	return placeholderRe.ReplaceAllStringFunc(s, func(m string) string {
		expr := placeholderRe.FindStringSubmatch(m)[1]
		if n, ok := strings.CutPrefix(expr, "_."); ok {
			return "{{" + strings.TrimSpace(n) + "}}"
		}
		return m
	})
}

func (im *importer) insomnia(raw json.RawMessage) error {
	var res []insResource
	if err := json.Unmarshal(raw, &res); err != nil {
		return errors.New("bad Insomnia export: resources is not a list")
	}
	byID := map[string]*insResource{}
	for i := range res {
		byID[res[i].ID] = &res[i]
	}
	folder := func(id string) string {
		var names []string
		for depth := 0; depth <= maxImportDepth; depth++ {
			p := byID[id]
			if p == nil || p.Type != "request_group" {
				break
			}
			names = append([]string{strings.TrimSpace(p.Name)}, names...)
			id = p.ParentID
		}
		return strings.Join(names, " / ")
	}
	for i := range res {
		x := &res[i]
		switch x.Type {
		case "environment":
			if p := byID[x.ParentID]; p != nil && p.Type == "environment" {
				continue // only the base environment
			}
			var data map[string]json.RawMessage
			if json.Unmarshal(x.Data, &data) != nil {
				continue
			}
			keys := make([]string, 0, len(data))
			for k := range data {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if v, ok := scalarOK(data[k]); ok {
					im.addVar(k, v)
				}
			}
		case "request":
			r := Request{Method: x.Method, URL: insVars(x.URL), Name: x.Name}
			if f := folder(x.ParentID); f != "" {
				r.Name = f + " / " + strings.TrimSpace(x.Name)
			}
			var qs []string
			for _, p := range x.Parameters {
				if !p.Disabled && p.Name != "" {
					qs = append(qs, qEscape(insVars(p.Name))+"="+qEscape(insVars(p.Value)))
				}
			}
			if len(qs) > 0 {
				sep := "?"
				if strings.Contains(r.URL, "?") {
					sep = "&"
				}
				r.URL += sep + strings.Join(qs, "&")
			}
			for _, h := range x.Headers {
				if !h.Disabled && strings.TrimSpace(h.Name) != "" {
					r.Headers = append(r.Headers, Header{Name: h.Name, Value: insVars(h.Value)})
				}
			}
			if b := x.Body; b != nil {
				body := insVars(b.Text)
				if body == "" && len(b.Params) > 0 {
					var parts []string
					for _, p := range b.Params {
						if !p.Disabled {
							parts = append(parts, qEscape(insVars(p.Name))+"="+qEscape(insVars(p.Value)))
						}
					}
					body = strings.Join(parts, "&")
				}
				r.Body = &body
				if b.MimeType != "" && body != "" {
					setDefaultHeader(&r, "Content-Type", b.MimeType)
				}
			}
			if a := x.Auth; a != nil && !a.Disabled && !r.HasHeader("Authorization") {
				switch a.Type {
				case "bearer":
					prefix := a.Prefix
					if prefix == "" {
						prefix = "Bearer"
					}
					r.Headers = append(r.Headers, Header{Name: "Authorization", Value: prefix + " " + insVars(a.Token)})
				case "basic":
					r.Headers = append(r.Headers, Header{Name: "Authorization", Value: "Basic " + insVars(a.Username) + " " + insVars(a.Password)})
				}
			}
			if err := im.add(r); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---- helpers ----

// oneLine keeps a value on one line so the .http text stays valid.
func oneLine(s string) string {
	s = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ").Replace(s)
	return strings.TrimSpace(s)
}

// varName makes a placeholder name from a header name
// ("X-API-Key" → "x_api_key").
func varName(h string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(strings.TrimSpace(h)) {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			b.WriteRune(c)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "secret"
	}
	return b.String()
}

// qEscape form-encodes s, but keeps {{placeholders}} as they are.
func qEscape(s string) string {
	if strings.Contains(s, "{{") {
		return s
	}
	return url.QueryEscape(s)
}

func setDefaultHeader(r *Request, name, value string) {
	if !r.HasHeader(name) {
		r.Headers = append(r.Headers, Header{Name: name, Value: value})
	}
}

// scalar is a JSON string, number or bool as text ("" otherwise).
func scalar(raw json.RawMessage) string {
	v, _ := scalarOK(raw)
	return v
}

func scalarOK(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return "", false
	}
	switch x := v.(type) {
	case string:
		return x, true
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(x), true
	}
	return "", false
}
