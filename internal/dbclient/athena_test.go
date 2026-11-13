package dbclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// The SigV4 signer against the AWS test suite's get-vanilla request.
func TestSigV4GetVanilla(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://example.amazonaws.com/", nil)
	at := time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC)
	sigV4Sign(req, nil, awsCreds{key: "AKIDEXAMPLE", secret: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"}, "us-east-1", "service", at)
	want := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request, SignedHeaders=host;x-amz-date, Signature=5fa00fa31553b73ebf1942676e86291e8372ff2a2260956d9b8aae1d763fbf31"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization =\n%s\nwant\n%s", got, want)
	}
	if req.Header.Get("X-Amz-Date") != "20150830T123600Z" {
		t.Fatalf("X-Amz-Date = %q", req.Header.Get("X-Amz-Date"))
	}
}

// fakeAthena is the Athena JSON API of one region.
type fakeAthena struct {
	mu      sync.Mutex
	targets []string
	bodys   []map[string]any
	auth    []string
	polls   int
}

func (f *fakeAthena) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		target := strings.TrimPrefix(r.Header.Get("X-Amz-Target"), "AmazonAthena.")
		f.targets = append(f.targets, target)
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		if r.Header.Get("Content-Type") != "application/x-amz-json-1.1" {
			t.Errorf("content type = %q", r.Header.Get("Content-Type"))
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.bodys = append(f.bodys, body)
		id, _ := body["QueryExecutionId"].(string)
		switch target {
		case "StartQueryExecution":
			q, _ := body["QueryString"].(string)
			switch {
			case strings.HasPrefix(q, "SELEC "):
				w.WriteHeader(400)
				_, _ = io.WriteString(w, `{"__type":"InvalidRequestException","Message":"line 1:1: mismatched input 'SELEC'"}`)
			case strings.HasPrefix(q, "INSERT"):
				_, _ = io.WriteString(w, `{"QueryExecutionId":"q-ins"}`)
			case strings.Contains(q, "broken"):
				_, _ = io.WriteString(w, `{"QueryExecutionId":"q-bad"}`)
			default:
				_, _ = io.WriteString(w, `{"QueryExecutionId":"q-1"}`)
			}
		case "GetQueryExecution":
			f.polls++
			state := "SUCCEEDED"
			if id == "q-1" && f.polls == 1 {
				state = "RUNNING"
			}
			reason := ""
			if id == "q-bad" {
				state, reason = "FAILED", "TABLE_NOT_FOUND: line 1:15: Table 'awsdatacatalog.sales.broken' does not exist"
			}
			_, _ = io.WriteString(w, `{"QueryExecution":{"QueryExecutionId":"`+id+`","Status":{"State":"`+state+`","StateChangeReason":"`+reason+`"},"StatementType":"DML"}}`)
		case "GetQueryResults":
			if id == "q-ins" {
				_, _ = io.WriteString(w, `{"UpdateCount":2,"ResultSet":{"Rows":[],"ResultSetMetadata":{"ColumnInfo":[]}}}`)
				return
			}
			if body["NextToken"] == "page2" {
				_, _ = io.WriteString(w, `{"ResultSet":{"ResultSetMetadata":{"ColumnInfo":[{"Name":"id","Type":"integer"},{"Name":"note","Type":"varchar"}]},"Rows":[{"Data":[{"VarCharValue":"3"},{"VarCharValue":"c"}]}]}}`)
				return
			}
			_, _ = io.WriteString(w, `{"NextToken":"page2","ResultSet":{"ResultSetMetadata":{"ColumnInfo":[{"Name":"id","Type":"integer"},{"Name":"note","Type":"varchar"}]},"Rows":[{"Data":[{"VarCharValue":"id"},{"VarCharValue":"note"}]},{"Data":[{"VarCharValue":"1"},{"VarCharValue":"hi"}]},{"Data":[{"VarCharValue":"2"},{}]}]}}`)
		case "StopQueryExecution":
		case "ListDatabases":
			_, _ = io.WriteString(w, `{"DatabaseList":[{"Name":"sales"},{"Name":"web"}]}`)
		case "ListTableMetadata":
			_, _ = io.WriteString(w, `{"TableMetadataList":[{"Name":"orders","TableType":"EXTERNAL_TABLE"},{"Name":"v","TableType":"VIRTUAL_VIEW"}]}`)
		case "GetTableMetadata":
			_, _ = io.WriteString(w, `{"TableMetadata":{"Name":"orders","Columns":[{"Name":"id","Type":"int"}],"PartitionKeys":[{"Name":"dt","Type":"string"}]}}`)
		default:
			t.Errorf("unexpected target %q", target)
			w.WriteHeader(400)
		}
	}
}

// DBX-66: Athena over its JSON API, signed with SigV4 (access key id as
// the user, secret key as the password): a query is started in the
// workgroup with its output location, polled to the end, its result
// pages read (the header row of a SELECT dropped) until MaxRows, a
// failure's reason as the error, DML counts, the tree from the data
// catalog, and a read-only twin.
func TestAthenaJSONAPI(t *testing.T) {
	old := warehousePollEvery
	warehousePollEvery = time.Millisecond
	defer func() { warehousePollEvery = old }()
	f := &fakeAthena{}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	s, err := OpenHTTPEngine(Conn{Driver: "athena", URL: srv.URL, User: "AKIDTEST", Database: "sales",
		Params: map[string]string{"region": "eu-west-1", "workgroup": "analysts", "output": "s3://bucket/results/"}}, "secret-key-1")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	res := s.Run(ctx, "SELECT id, note FROM orders WHERE id = ?", RunOptions{Args: []any{"x"}})
	if res.Err != nil || len(res.Columns) != 2 || len(res.Rows) != 3 || res.Rows[0][0].Text != "1" || !res.Rows[1][1].Null || res.Rows[2][1].Text != "c" {
		t.Fatalf("select = %+v", res)
	}
	f.mu.Lock()
	start := f.bodys[0]
	if start["WorkGroup"] != "analysts" {
		t.Errorf("start = %v", start)
	}
	if ctx, _ := start["QueryExecutionContext"].(map[string]any); ctx["Database"] != "sales" || ctx["Catalog"] != "AwsDataCatalog" {
		t.Errorf("context = %v", start["QueryExecutionContext"])
	}
	if rc, _ := start["ResultConfiguration"].(map[string]any); rc["OutputLocation"] != "s3://bucket/results/" {
		t.Errorf("output = %v", start["ResultConfiguration"])
	}
	if ps, _ := start["ExecutionParameters"].([]any); len(ps) != 1 || ps[0] != "'x'" {
		t.Errorf("parameters = %v", start["ExecutionParameters"])
	}
	if a := f.auth[0]; !strings.HasPrefix(a, "AWS4-HMAC-SHA256 Credential=AKIDTEST/") || !strings.Contains(a, "/eu-west-1/athena/aws4_request") || strings.Contains(a, "secret-key-1") {
		t.Errorf("auth = %q", a)
	}
	f.mu.Unlock()
	if r := s.Run(ctx, "SELECT id, note FROM orders", RunOptions{MaxRows: 1}); len(r.Rows) != 1 || !r.Truncated {
		t.Fatalf("MaxRows = %+v", r)
	}
	if r := s.Run(ctx, "INSERT INTO orders VALUES (1, 'a'), (2, 'b')", RunOptions{}); r.Err != nil || r.Affected != 2 {
		t.Fatalf("insert = %+v", r)
	}
	if r := s.Run(ctx, "SELECT * FROM broken", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "TABLE_NOT_FOUND") {
		t.Fatalf("failed = %v", r.Err)
	}
	if r := s.Run(ctx, "SELEC 1", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "mismatched input") {
		t.Fatalf("error = %v", r.Err)
	}
	in, err := s.Introspect(ctx, IntrospectionRequest{})
	if err != nil || strings.Join(in.Databases, ",") != "AwsDataCatalog" {
		t.Fatalf("catalogs = %v %v", in.Databases, err)
	}
	if in, err = s.Introspect(ctx, IntrospectionRequest{Database: "AwsDataCatalog"}); err != nil || strings.Join(in.Schemas, ",") != "sales,web" {
		t.Fatalf("databases = %v %v", in.Schemas, err)
	}
	if in, err = s.Introspect(ctx, IntrospectionRequest{Database: "AwsDataCatalog", Schema: "sales"}); err != nil || len(in.Objects) != 2 || in.Objects[1].Kind != "VIEW" {
		t.Fatalf("tables = %v %v", in.Objects, err)
	}
	if in, err = s.Introspect(ctx, IntrospectionRequest{Database: "AwsDataCatalog", Schema: "sales", Object: "orders"}); err != nil || len(in.Columns) != 2 || in.Columns[1].Name != "dt" {
		t.Fatalf("columns = %v %v", in.Columns, err)
	}
	ro, _ := s.ReadOnlySession()
	if r := ro.Run(ctx, "DROP TABLE orders", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "read-only") {
		t.Fatalf("read-only = %v", r.Err)
	}
}

// athena://KEY@athena.eu-west-1.amazonaws.com/sales?workgroup=primary:
// the region comes from the host.
func TestAthenaURL(t *testing.T) {
	c, err := ParseURL("athena://AKIDTEST@athena.eu-west-1.amazonaws.com/sales?workgroup=primary&output=s3://b/r/")
	if err != nil {
		t.Fatal(err)
	}
	if c.Driver != "athena" || !c.TLS || c.Port != 443 || c.User != "AKIDTEST" || c.Database != "sales" || c.Params["region"] != "eu-west-1" || c.Params["workgroup"] != "primary" || c.Params["output"] != "s3://b/r/" {
		t.Fatalf("conn = %+v", c)
	}
}
