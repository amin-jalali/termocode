package dbclient

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// BigQuerySession runs GoogleSQL on BigQuery over its REST API (DBX-66).
// Database is the project; params.location the jobs' location. The
// password is an OAuth access token, or a service account key (its
// JSON), which is traded for a token at the key's token_uri with a JWT
// signed here; the key itself is never sent.
//
// Every statement gets a dry run first: its bytes show as a note, a
// statement over params.maxBytes (10 GiB by default) is refused before
// it runs, and the cap goes along as maximumBytesBilled so the server
// refuses it too. The read-only twin runs only what the dry run calls a
// SELECT.
type BigQuerySession struct {
	*httpEngine
	conn     Conn
	project  string
	maxBytes int64
	readOnly bool
}

// bigQueryHost is the API's host.
const bigQueryHost = "bigquery.googleapis.com"

// BigQueryDefaultMaxBytes is the bytes cap of a statement when the
// connection sets none (10 GiB).
const BigQueryDefaultMaxBytes int64 = 10 << 30

func init() {
	registerHTTPEngine(HTTPEngine{
		ID: "bigquery", Title: "BigQuery", Port: 443, TLSPort: 443,
		Hint:    "REST API · the project as database, a token or service account key as password",
		Schemes: []string{"bigquery"},
		Open: func(c Conn, password string) (Session, error) {
			return NewBigQuerySession(c, password)
		},
		QueryParams: true,
		FixURL: func(c *Conn) {
			// bigquery://PROJECT?location=EU
			if c.Database == "" {
				c.Database = c.Host
			}
			c.Host, c.TLS, c.Port = bigQueryHost, true, 443
		},
	})
}

// NewBigQuerySession makes a session of c; it does not connect.
func NewBigQuerySession(c Conn, secret string) (*BigQuerySession, error) {
	if c.Database == "" {
		return nil, errors.New("BigQuery: no project: set the connection's database to the project id")
	}
	if c.URL == "" {
		if c.Host == "" {
			c.Host = bigQueryHost
		}
		c.TLS = true
	}
	e, err := newHTTPEngine("BigQuery", c, "")
	if err != nil {
		return nil, err
	}
	e.password = secret
	src, err := bigQueryTokenSource(secret, e.client.Transport)
	if err != nil {
		return nil, err
	}
	e.client.Transport = &bearerTransport{src: src, next: e.client.Transport}
	s := &BigQuerySession{httpEngine: e, conn: c, project: c.Database, maxBytes: BigQueryDefaultMaxBytes}
	if v := warehouseParam(c, "maxBytes"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("BigQuery: params.maxBytes %q is not a positive number of bytes", v)
		}
		s.maxBytes = n
	}
	return s, nil
}

// ── auth ─────────────────────────────────────────────────────────────────

// bearerTransport adds "Authorization: Bearer <token>" to each request.
type bearerTransport struct {
	src  func(context.Context) (string, error)
	next http.RoundTripper
}

func (b *bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	tok, err := b.src(r.Context())
	if err != nil {
		return nil, err
	}
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+tok)
	return b.next.RoundTrip(r)
}

type googleServiceKey struct {
	Type        string `json:"type"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// bigQueryTokenSource gives the token of each request: secret itself, or
// for a service account key a token traded for a signed JWT and kept
// until a minute before it ends.
func bigQueryTokenSource(secret string, next http.RoundTripper) (func(context.Context) (string, error), error) {
	secret = strings.TrimSpace(secret)
	if !strings.HasPrefix(secret, "{") {
		if secret == "" {
			return nil, errors.New("BigQuery: no token: the password is an access token or a service account key")
		}
		return func(context.Context) (string, error) { return secret, nil }, nil
	}
	var key googleServiceKey
	if err := json.Unmarshal([]byte(secret), &key); err != nil || key.ClientEmail == "" || key.PrivateKey == "" {
		return nil, errors.New("BigQuery: the password looks like a key file but is not a service account key")
	}
	if key.TokenURI == "" {
		key.TokenURI = "https://oauth2.googleapis.com/token"
	}
	block, _ := pem.Decode([]byte(key.PrivateKey))
	if block == nil {
		return nil, errors.New("BigQuery: the service account key has no private key")
	}
	var rk *rsa.PrivateKey
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rk, _ = k.(*rsa.PrivateKey)
	} else if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		rk = k
	}
	if rk == nil {
		return nil, errors.New("BigQuery: the service account key is not an RSA key")
	}
	client := &http.Client{Transport: next, Timeout: 30 * time.Second}
	var mu sync.Mutex
	var tok string
	var until time.Time
	return func(ctx context.Context) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		if tok != "" && time.Now().Before(until) {
			return tok, nil
		}
		now := time.Now()
		assertion, err := signGoogleJWT(rk, key.ClientEmail, key.TokenURI, now)
		if err != nil {
			return "", err
		}
		form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {assertion}}
		req, err := http.NewRequestWithContext(ctx, "POST", key.TokenURI, strings.NewReader(form.Encode()))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := client.Do(req)
		if err != nil {
			return "", fmt.Errorf("BigQuery: sign-in with the service account key: %v", err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		var out struct {
			AccessToken string `json:"access_token"`
			ExpiresIn   int    `json:"expires_in"`
			Error       string `json:"error_description"`
		}
		_ = json.Unmarshal(data, &out)
		if resp.StatusCode != 200 || out.AccessToken == "" {
			msg := out.Error
			if msg == "" {
				msg = httpErrorMessage(data)
			}
			return "", fmt.Errorf("BigQuery: the service account sign-in was refused: %s", msg)
		}
		tok = out.AccessToken
		until = now.Add(time.Duration(max(out.ExpiresIn-60, 60)) * time.Second)
		return tok, nil
	}, nil
}

// signGoogleJWT is the RS256 assertion of a service account.
func signGoogleJWT(k *rsa.PrivateKey, email, aud string, now time.Time) (string, error) {
	enc := base64.RawURLEncoding
	head := enc.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{"iss": email, "scope": "https://www.googleapis.com/auth/bigquery",
		"aud": aud, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix()})
	unsigned := head + "." + enc.EncodeToString(claims)
	sum := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return unsigned + "." + enc.EncodeToString(sig), nil
}

// ── queries ──────────────────────────────────────────────────────────────

type bqParam struct {
	ParameterType struct {
		Type string `json:"type"`
	} `json:"parameterType"`
	ParameterValue struct {
		Value string `json:"value"`
	} `json:"parameterValue"`
}

func bqParams(args []any) []bqParam {
	var out []bqParam
	for _, a := range args {
		var p bqParam
		switch a.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			p.ParameterType.Type = "INT64"
		case float32, float64:
			p.ParameterType.Type = "FLOAT64"
		case bool:
			p.ParameterType.Type = "BOOL"
		default:
			p.ParameterType.Type = "STRING"
		}
		p.ParameterValue.Value = fmt.Sprint(a)
		out = append(out, p)
	}
	return out
}

type bqField struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Mode string `json:"mode"`
}

type bqQueryResponse struct {
	JobComplete  bool `json:"jobComplete"`
	JobReference struct {
		JobID    string `json:"jobId"`
		Location string `json:"location"`
	} `json:"jobReference"`
	Schema struct {
		Fields []bqField `json:"fields"`
	} `json:"schema"`
	Rows []struct {
		F []struct {
			V json.RawMessage `json:"v"`
		} `json:"f"`
	} `json:"rows"`
	TotalRows          string `json:"totalRows"`
	PageToken          string `json:"pageToken"`
	NumDmlAffectedRows string `json:"numDmlAffectedRows"`
}

func (s *BigQuerySession) path(p string) string {
	return "/bigquery/v2/projects/" + url.PathEscape(s.project) + p
}

func (s *BigQuerySession) Ping(ctx context.Context) error {
	var out map[string]any
	return s.doJSON(ctx, "GET", s.path("/datasets?maxResults=1"), nil, &out)
}

// dryRun is the bytes and statement type of stmt.
func (s *BigQuerySession) dryRun(ctx context.Context, stmt string, args []any) (int64, string, error) {
	q := map[string]any{"query": stmt, "useLegacySql": false}
	if len(args) > 0 {
		q["parameterMode"], q["queryParameters"] = "POSITIONAL", bqParams(args)
	}
	job := map[string]any{"configuration": map[string]any{"dryRun": true, "query": q}}
	if loc := warehouseParam(s.conn, "location"); loc != "" {
		job["jobReference"] = map[string]any{"projectId": s.project, "location": loc}
	}
	var out struct {
		Statistics struct {
			Query struct {
				TotalBytesProcessed string `json:"totalBytesProcessed"`
				StatementType       string `json:"statementType"`
			} `json:"query"`
		} `json:"statistics"`
	}
	if err := s.doJSON(ctx, "POST", s.path("/jobs"), job, &out); err != nil {
		return 0, "", err
	}
	n, _ := strconv.ParseInt(out.Statistics.Query.TotalBytesProcessed, 10, 64)
	return n, out.Statistics.Query.StatementType, nil
}

func (s *BigQuerySession) Run(ctx context.Context, stmt string, opts RunOptions) Result {
	start := time.Now()
	res := s.run(ctx, strings.TrimSpace(stmt), opts)
	res.Elapsed = time.Since(start)
	return res
}

func (s *BigQuerySession) run(ctx context.Context, stmt string, opts RunOptions) Result {
	bytesN, typ, err := s.dryRun(ctx, stmt, opts.Args)
	if err != nil {
		return Result{Err: err}
	}
	if s.readOnly && typ != "SELECT" {
		return Result{Err: fmt.Errorf("BigQuery: this is a read-only session; the dry run calls this a %s, only SELECT runs here", strings.ToLower(typ))}
	}
	if bytesN > s.maxBytes {
		return Result{Err: fmt.Errorf("BigQuery: this statement would process %s, over the %s limit; raise params.maxBytes to run it", humanBytes(bytesN), humanBytes(s.maxBytes))}
	}
	req := map[string]any{"query": stmt, "useLegacySql": false, "maximumBytesBilled": strconv.FormatInt(s.maxBytes, 10), "timeoutMs": 10000}
	if len(opts.Args) > 0 {
		req["parameterMode"], req["queryParameters"] = "POSITIONAL", bqParams(opts.Args)
	}
	if loc := warehouseParam(s.conn, "location"); loc != "" {
		req["location"] = loc
	}
	if opts.MaxRows > 0 {
		req["maxResults"] = opts.MaxRows
	}
	var resp bqQueryResponse
	if err := s.doJSON(ctx, "POST", s.path("/queries"), req, &resp); err != nil {
		return Result{Err: err}
	}
	job, loc := resp.JobReference.JobID, resp.JobReference.Location
	if loc == "" {
		loc = warehouseParam(s.conn, "location")
	}
	results := func(token string) (bqQueryResponse, error) {
		q := url.Values{"timeoutMs": {"10000"}}
		if loc != "" {
			q.Set("location", loc)
		}
		if token != "" {
			q.Set("pageToken", token)
		}
		if opts.MaxRows > 0 {
			q.Set("maxResults", strconv.Itoa(opts.MaxRows))
		}
		var out bqQueryResponse
		err := s.doJSON(ctx, "GET", s.path("/queries/"+url.PathEscape(job)+"?"+q.Encode()), nil, &out)
		return out, err
	}
	if !resp.JobComplete {
		err := warehousePoll(ctx, func() (bool, error) {
			var perr error
			resp, perr = results("")
			return resp.JobComplete, perr
		})
		if err != nil {
			if ctx.Err() != nil {
				s.cancelJob(job, loc)
			}
			return Result{Err: err}
		}
	}
	res := Result{Notices: []string{"BigQuery: " + humanBytes(bytesN) + " processed (dry run estimate)"}}
	for _, f := range resp.Schema.Fields {
		t := f.Type
		if f.Mode == "REPEATED" {
			t = "ARRAY<" + t + ">"
		}
		res.Columns = append(res.Columns, Column{Name: f.Name, Type: t})
	}
	for {
		for _, r := range resp.Rows {
			if opts.MaxRows > 0 && len(res.Rows) >= opts.MaxRows {
				res.Truncated = true
				break
			}
			row := make([]Cell, len(res.Columns))
			for i := range row {
				if i < len(r.F) {
					row[i] = bqCell(r.F[i].V)
				} else {
					row[i] = Cell{Null: true}
				}
			}
			res.Rows = append(res.Rows, row)
		}
		if resp.PageToken == "" || res.Truncated {
			break
		}
		if opts.MaxRows > 0 && len(res.Rows) >= opts.MaxRows {
			res.Truncated = true
			break
		}
		if resp, err = results(resp.PageToken); err != nil {
			res.Err = err
			return res
		}
	}
	res.Affected, _ = strconv.ParseInt(resp.NumDmlAffectedRows, 10, 64)
	return res
}

// bqValue is one REST cell as a Go value: a string, nil, or the list of
// an array or a record.
func bqValue(raw json.RawMessage) any {
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || string(t) == "null" {
		return nil
	}
	switch t[0] {
	case '[':
		var items []struct {
			V json.RawMessage `json:"v"`
		}
		_ = json.Unmarshal(t, &items)
		out := make([]any, 0, len(items))
		for _, it := range items {
			out = append(out, bqValue(it.V))
		}
		return out
	case '{':
		var rec struct {
			F []struct {
				V json.RawMessage `json:"v"`
			} `json:"f"`
		}
		_ = json.Unmarshal(t, &rec)
		out := make([]any, 0, len(rec.F))
		for _, f := range rec.F {
			out = append(out, bqValue(f.V))
		}
		return out
	}
	var s string
	if json.Unmarshal(t, &s) == nil {
		return s
	}
	return string(t)
}

func bqCell(raw json.RawMessage) Cell {
	switch v := bqValue(raw).(type) {
	case nil:
		return Cell{Null: true}
	case string:
		return Cell{Text: v}
	default:
		b, _ := json.Marshal(v)
		return Cell{Text: string(b), Kind: "json"}
	}
}

// cancelJob asks BigQuery to stop a job (best effort).
func (s *BigQuerySession) cancelJob(job, loc string) {
	if job == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := s.path("/jobs/" + url.PathEscape(job) + "/cancel")
	if loc != "" {
		p += "?location=" + url.QueryEscape(loc)
	}
	_ = s.doJSON(ctx, "POST", p, nil, nil)
}

// humanBytes is n in KB, MB, GB or TB (1024-based, one decimal).
func humanBytes(n int64) string {
	if n < 1024 {
		return strconv.FormatInt(n, 10) + " B"
	}
	v, unit := float64(n)/1024, "KB"
	for _, u := range []string{"MB", "GB", "TB", "PB"} {
		if v < 1024 {
			break
		}
		v, unit = v/1024, u
	}
	return strconv.FormatFloat(v, 'f', 1, 64) + " " + unit
}

// ── tree ─────────────────────────────────────────────────────────────────

func (s *BigQuerySession) Introspect(ctx context.Context, req IntrospectionRequest) (IntrospectionResult, error) {
	var out IntrospectionResult
	switch {
	case req.Database == "":
		out.Databases = []string{s.project}
	case req.Schema == "":
		err := s.pages(ctx, "/datasets", func(data []byte) (string, error) {
			var page struct {
				Datasets []struct {
					DatasetReference struct {
						DatasetID string `json:"datasetId"`
					} `json:"datasetReference"`
				} `json:"datasets"`
				NextPageToken string `json:"nextPageToken"`
			}
			err := json.Unmarshal(data, &page)
			for _, d := range page.Datasets {
				out.Schemas = append(out.Schemas, d.DatasetReference.DatasetID)
			}
			return page.NextPageToken, err
		})
		return out, err
	case req.Object == "":
		err := s.pages(ctx, "/datasets/"+url.PathEscape(req.Schema)+"/tables", func(data []byte) (string, error) {
			var page struct {
				Tables []struct {
					TableReference struct {
						TableID string `json:"tableId"`
					} `json:"tableReference"`
					Type string `json:"type"`
				} `json:"tables"`
				NextPageToken string `json:"nextPageToken"`
			}
			err := json.Unmarshal(data, &page)
			for _, t := range page.Tables {
				kind := t.Type
				if kind == "TABLE" {
					kind = "BASE TABLE"
				}
				out.Objects = append(out.Objects, SchemaObject{Name: t.TableReference.TableID, Kind: kind})
			}
			return page.NextPageToken, err
		})
		return out, err
	default:
		var t struct {
			Schema struct {
				Fields []bqField `json:"fields"`
			} `json:"schema"`
		}
		if err := s.doJSON(ctx, "GET", s.path("/datasets/"+url.PathEscape(req.Schema)+"/tables/"+url.PathEscape(req.Object)), nil, &t); err != nil {
			return out, err
		}
		for _, f := range t.Schema.Fields {
			out.Columns = append(out.Columns, Column{Name: f.Name, Type: f.Type, Table: req.Object})
		}
	}
	return out, nil
}

// pages GETs a list and its next pages (at most 50).
func (s *BigQuerySession) pages(ctx context.Context, p string, read func([]byte) (string, error)) error {
	token := ""
	for range 50 {
		q := p
		if token != "" {
			q += "?pageToken=" + url.QueryEscape(token)
		}
		data, err := s.do(ctx, "GET", s.path(q), nil, "")
		if err != nil {
			return err
		}
		if token, err = read(data); err != nil {
			return fmt.Errorf("BigQuery: unexpected answer: %v", err)
		}
		if token == "" {
			return nil
		}
	}
	return nil
}

func (s *BigQuerySession) Begin(context.Context) error    { return errNoTx("BigQuery") }
func (s *BigQuerySession) Commit(context.Context) error   { return errNoTx("BigQuery") }
func (s *BigQuerySession) Rollback(context.Context) error { return errNoTx("BigQuery") }

// ReadOnlySession is a twin that runs only what the dry run calls a
// SELECT.
func (s *BigQuerySession) ReadOnlySession() (Session, error) {
	return &BigQuerySession{httpEngine: s.clone(), conn: s.conn, project: s.project, maxBytes: s.maxBytes, readOnly: true}, nil
}
