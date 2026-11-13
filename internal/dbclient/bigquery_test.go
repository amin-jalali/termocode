package dbclient

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// fakeBigQuery is the BigQuery REST API of project "proj" plus an
// OAuth token endpoint for a service account key.
type fakeBigQuery struct {
	mu      sync.Mutex
	queries []map[string]any
	dry     []string
	auth    []string
	grants  []url.Values
}

func (f *fakeBigQuery) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path == "/token" {
			_ = r.ParseForm()
			f.grants = append(f.grants, r.PostForm)
			_, _ = io.WriteString(w, `{"access_token":"ya29.from-key","expires_in":3600,"token_type":"Bearer"}`)
			return
		}
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		var body map[string]any
		if r.Method == "POST" {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		switch {
		case r.Method == "POST" && r.URL.Path == "/bigquery/v2/projects/proj/jobs":
			cfg, _ := body["configuration"].(map[string]any)
			q, _ := cfg["query"].(map[string]any)
			sql, _ := q["query"].(string)
			f.dry = append(f.dry, sql)
			typ, bytes := "SELECT", "1536"
			switch {
			case strings.HasPrefix(sql, "DELETE"):
				typ = "DELETE"
			case strings.Contains(sql, "huge"):
				bytes = "53687091200" // 50 GiB
			case strings.HasPrefix(sql, "SELEC "):
				w.WriteHeader(400)
				_, _ = io.WriteString(w, `{"error":{"code":400,"message":"Syntax error: Unexpected identifier \"SELEC\" at [1:1]"}}`)
				return
			}
			_, _ = io.WriteString(w, `{"statistics":{"query":{"totalBytesProcessed":"`+bytes+`","statementType":"`+typ+`"}}}`)
		case r.Method == "POST" && r.URL.Path == "/bigquery/v2/projects/proj/queries":
			f.queries = append(f.queries, body)
			sql, _ := body["query"].(string)
			switch {
			case strings.Contains(sql, "slow"):
				_, _ = io.WriteString(w, `{"jobComplete":false,"jobReference":{"projectId":"proj","jobId":"job-9","location":"EU"}}`)
			case strings.HasPrefix(sql, "DELETE"):
				_, _ = io.WriteString(w, `{"jobComplete":true,"numDmlAffectedRows":"3","jobReference":{"jobId":"job-2"}}`)
			default:
				_, _ = io.WriteString(w, `{"jobComplete":true,"jobReference":{"jobId":"job-1"},"schema":{"fields":[{"name":"id","type":"INTEGER"},{"name":"tags","type":"STRING","mode":"REPEATED"},{"name":"note","type":"STRING"}]},"totalRows":"2","rows":[{"f":[{"v":"1"},{"v":[{"v":"a"},{"v":"b"}]},{"v":"hi"}]},{"f":[{"v":"2"},{"v":[]},{"v":null}]}]}`)
			}
		case r.Method == "GET" && r.URL.Path == "/bigquery/v2/projects/proj/queries/job-9":
			if r.URL.Query().Get("location") != "EU" {
				t.Errorf("poll without the job's location: %s", r.URL)
			}
			_, _ = io.WriteString(w, `{"jobComplete":true,"schema":{"fields":[{"name":"n","type":"INTEGER"}]},"rows":[{"f":[{"v":"42"}]}]}`)
		case r.URL.Path == "/bigquery/v2/projects/proj/datasets":
			_, _ = io.WriteString(w, `{"datasets":[{"datasetReference":{"datasetId":"sales"}},{"datasetReference":{"datasetId":"web"}}]}`)
		case r.URL.Path == "/bigquery/v2/projects/proj/datasets/sales/tables":
			_, _ = io.WriteString(w, `{"tables":[{"tableReference":{"tableId":"orders"},"type":"TABLE"},{"tableReference":{"tableId":"v_orders"},"type":"VIEW"}]}`)
		case r.URL.Path == "/bigquery/v2/projects/proj/datasets/sales/tables/orders":
			_, _ = io.WriteString(w, `{"schema":{"fields":[{"name":"id","type":"INTEGER"},{"name":"note","type":"STRING"}]}}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(404)
		}
	}
}

// DBX-66: BigQuery over its REST API: every statement gets a dry run
// first (its bytes as a note, refused over params.maxBytes, 10 GiB by
// default, and sent as maximumBytesBilled), rows with repeated fields,
// a job still running polled in its location, DML counts, the error
// text, the tree from datasets and tables, and a read-only twin that the
// dry run's statement type decides.
func TestBigQueryREST(t *testing.T) {
	f := &fakeBigQuery{}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	s, err := OpenHTTPEngine(Conn{Driver: "bigquery", URL: srv.URL, Database: "proj"}, "ya29.token")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	res := s.Run(ctx, "SELECT id, tags, note FROM sales.orders WHERE id = ?", RunOptions{Args: []any{7}})
	if res.Err != nil || len(res.Columns) != 3 || len(res.Rows) != 2 || res.Rows[0][1].Text != `["a","b"]` || !res.Rows[1][2].Null {
		t.Fatalf("select = %+v", res)
	}
	if len(res.Notices) == 0 || !strings.Contains(res.Notices[0], "1.5 KB") {
		t.Errorf("notices = %q, want the dry run's bytes", res.Notices)
	}
	f.mu.Lock()
	q := f.queries[len(f.queries)-1]
	if q["useLegacySql"] != false || q["maximumBytesBilled"] != "10737418240" || q["parameterMode"] != "POSITIONAL" {
		t.Errorf("query = %v", q)
	}
	if f.auth[0] != "Bearer ya29.token" {
		t.Errorf("auth = %q", f.auth[0])
	}
	f.mu.Unlock()
	if r := s.Run(ctx, "SELECT * FROM huge", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "50.0 GB") || !strings.Contains(r.Err.Error(), "maxBytes") {
		t.Fatalf("over the cap = %v", r.Err)
	}
	if r := s.Run(ctx, "SELECT slow", RunOptions{}); r.Err != nil || len(r.Rows) != 1 || r.Rows[0][0].Text != "42" {
		t.Fatalf("poll = %+v", r)
	}
	if r := s.Run(ctx, "DELETE FROM sales.orders WHERE true", RunOptions{}); r.Err != nil || r.Affected != 3 {
		t.Fatalf("delete = %+v", r)
	}
	if r := s.Run(ctx, "SELEC 1", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "Syntax error") {
		t.Fatalf("error = %v", r.Err)
	}
	in, err := s.Introspect(ctx, IntrospectionRequest{})
	if err != nil || strings.Join(in.Databases, ",") != "proj" {
		t.Fatalf("projects = %v %v", in.Databases, err)
	}
	if in, err = s.Introspect(ctx, IntrospectionRequest{Database: "proj"}); err != nil || strings.Join(in.Schemas, ",") != "sales,web" {
		t.Fatalf("datasets = %v %v", in.Schemas, err)
	}
	if in, err = s.Introspect(ctx, IntrospectionRequest{Database: "proj", Schema: "sales"}); err != nil || len(in.Objects) != 2 || in.Objects[1].Kind != "VIEW" {
		t.Fatalf("tables = %v %v", in.Objects, err)
	}
	if in, err = s.Introspect(ctx, IntrospectionRequest{Database: "proj", Schema: "sales", Object: "orders"}); err != nil || len(in.Columns) != 2 || in.Columns[0].Type != "INTEGER" {
		t.Fatalf("columns = %v %v", in.Columns, err)
	}
	ro, _ := s.ReadOnlySession()
	f.mu.Lock()
	n := len(f.queries)
	f.mu.Unlock()
	if r := ro.Run(ctx, "DELETE FROM sales.orders WHERE true", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "read-only") {
		t.Fatalf("read-only delete = %v", r.Err)
	}
	f.mu.Lock()
	if len(f.queries) != n {
		t.Errorf("the refused delete ran")
	}
	f.mu.Unlock()
}

// A service account key as the password: a signed JWT is traded for an
// access token at the key's token_uri, once, and the key never leaves.
func TestBigQueryServiceAccountKey(t *testing.T) {
	f := &fakeBigQuery{}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(k)
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	key, _ := json.Marshal(map[string]string{"type": "service_account", "client_email": "bot@proj.iam.gserviceaccount.com", "private_key": pemKey, "token_uri": srv.URL + "/token"})
	s, err := OpenHTTPEngine(Conn{Driver: "bigquery", URL: srv.URL, Database: "proj"}, string(key))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for range 2 {
		if r := s.Run(context.Background(), "SELECT 1", RunOptions{}); r.Err != nil {
			t.Fatal(r.Err)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.grants) != 1 || f.grants[0].Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" || strings.Count(f.grants[0].Get("assertion"), ".") != 2 {
		t.Fatalf("grants = %v", f.grants)
	}
	for _, a := range f.auth {
		if a != "Bearer ya29.from-key" {
			t.Fatalf("auth = %q", a)
		}
	}
}

// bigquery://PROJECT?location=EU: the project is the database, the
// host is Google's.
func TestBigQueryURL(t *testing.T) {
	c, err := ParseURL("bigquery://my-proj?location=EU&maxBytes=1000000")
	if err != nil {
		t.Fatal(err)
	}
	if c.Driver != "bigquery" || c.Database != "my-proj" || c.Host != "bigquery.googleapis.com" || !c.TLS || c.Params["location"] != "EU" || c.Params["maxBytes"] != "1000000" {
		t.Fatalf("conn = %+v", c)
	}
}
