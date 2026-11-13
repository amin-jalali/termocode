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
)

// fakeDynamo is the DynamoDB JSON API of one region.
type fakeDynamo struct {
	mu    sync.Mutex
	calls []string
	bodys []map[string]any
	auth  []string
}

func (f *fakeDynamo) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Content-Type") != "application/x-amz-json-1.0" {
			t.Errorf("content type = %q", r.Header.Get("Content-Type"))
		}
		target := strings.TrimPrefix(r.Header.Get("X-Amz-Target"), "DynamoDB_20120810.")
		f.calls = append(f.calls, target)
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.bodys = append(f.bodys, body)
		switch target {
		case "ExecuteStatement":
			stmt, _ := body["Statement"].(string)
			switch {
			case strings.HasPrefix(stmt, "SELEC "):
				w.WriteHeader(400)
				_, _ = io.WriteString(w, `{"__type":"com.amazonaws.dynamodb.v20120810#ValidationException","message":"Statement wasn't well formed, can't be processed: unexpected keyword"}`)
			case strings.HasPrefix(stmt, "INSERT"):
				_, _ = io.WriteString(w, `{}`)
			case body["NextToken"] == "t2":
				_, _ = io.WriteString(w, `{"Items":[{"pk":{"S":"u#3"},"n":{"N":"3"}}]}`)
			default:
				_, _ = io.WriteString(w, `{"NextToken":"t2","Items":[{"pk":{"S":"u#1"},"n":{"N":"1.5"},"tags":{"SS":["a","b"]},"meta":{"M":{"ok":{"BOOL":true},"gone":{"NULL":true}}}},{"pk":{"S":"u#2"},"list":{"L":[{"N":"1"},{"S":"x"}]}}]}`)
			}
		case "ListTables":
			if body["ExclusiveStartTableName"] == "orders" {
				_, _ = io.WriteString(w, `{"TableNames":["users"]}`)
				return
			}
			_, _ = io.WriteString(w, `{"TableNames":["events","orders"],"LastEvaluatedTableName":"orders"}`)
		case "DescribeTable":
			_, _ = io.WriteString(w, `{"Table":{"TableName":"users","KeySchema":[{"AttributeName":"pk","KeyType":"HASH"},{"AttributeName":"sk","KeyType":"RANGE"}],"AttributeDefinitions":[{"AttributeName":"pk","AttributeType":"S"},{"AttributeName":"sk","AttributeType":"N"},{"AttributeName":"email","AttributeType":"S"}],"GlobalSecondaryIndexes":[{"IndexName":"by-email","KeySchema":[{"AttributeName":"email","KeyType":"HASH"}]}]}}`)
		default:
			t.Errorf("unexpected target %q", target)
			w.WriteHeader(400)
		}
	}
}

// DBX-68: DynamoDB over its JSON API with SigV4: PartiQL statements
// (scan and query) with typed parameters, items as rows (sets, lists
// and maps as JSON), pages followed to MaxRows, the error's type and
// text, the tree from ListTables (all pages) and DescribeTable (keys,
// attributes, indexes), and a read-only twin.
func TestDynamoDBPartiQL(t *testing.T) {
	f := &fakeDynamo{}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	s, err := OpenHTTPEngine(Conn{Driver: "dynamodb", URL: srv.URL, User: "AKIDTEST", Params: map[string]string{"region": "eu-west-1"}}, "secret-key-1")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	res := s.Run(ctx, `SELECT * FROM "users" WHERE pk = ?`, RunOptions{Args: []any{"u#1"}})
	if res.Err != nil || len(res.Rows) != 3 {
		t.Fatalf("select = %+v", res)
	}
	cell := func(row int, col string) Cell {
		for i, c := range res.Columns {
			if c.Name == col {
				return res.Rows[row][i]
			}
		}
		t.Fatalf("no column %s in %v", col, res.Columns)
		return Cell{}
	}
	if cell(0, "pk").Text != "u#1" || cell(0, "n").Text != "1.5" || cell(0, "tags").Text != `["a","b"]` || cell(0, "meta").Text != `{"gone":null,"ok":true}` ||
		cell(1, "list").Text != `[1,"x"]` || !cell(1, "n").Null || cell(2, "pk").Text != "u#3" {
		t.Fatalf("items = %+v", res)
	}
	f.mu.Lock()
	ps, _ := f.bodys[0]["Parameters"].([]any)
	if len(ps) != 1 || ps[0].(map[string]any)["S"] != "u#1" {
		t.Errorf("parameters = %v", f.bodys[0]["Parameters"])
	}
	if a := f.auth[0]; !strings.Contains(a, "Credential=AKIDTEST/") || !strings.Contains(a, "/eu-west-1/dynamodb/aws4_request") {
		t.Errorf("auth = %q", a)
	}
	f.mu.Unlock()
	if r := s.Run(ctx, `SELECT * FROM "users"`, RunOptions{MaxRows: 2}); len(r.Rows) != 2 || !r.Truncated {
		t.Fatalf("MaxRows = %+v", r)
	}
	if r := s.Run(ctx, `INSERT INTO "users" VALUE {'pk': 'u#9'}`, RunOptions{}); r.Err != nil || r.Affected != 1 {
		t.Fatalf("insert = %+v", r)
	}
	if r := s.Run(ctx, "SELEC 1", RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "ValidationException") || !strings.Contains(r.Err.Error(), "well formed") {
		t.Fatalf("error = %v", r.Err)
	}
	in, err := s.Introspect(ctx, IntrospectionRequest{})
	if err != nil || strings.Join(in.Databases, ",") != "eu-west-1" {
		t.Fatalf("databases = %v %v", in.Databases, err)
	}
	if in, err = s.Introspect(ctx, IntrospectionRequest{Database: "eu-west-1"}); err != nil || len(in.Objects) != 3 || in.Objects[2].Name != "users" {
		t.Fatalf("tables = %v %v", in.Objects, err)
	}
	in, err = s.Introspect(ctx, IntrospectionRequest{Database: "eu-west-1", Object: "users"})
	if err != nil || len(in.Columns) != 3 || !in.Columns[0].PK || !in.Columns[1].PK || in.Columns[1].Type != "N" || strings.Join(in.Keys, ",") != "pk,sk" ||
		len(in.Indexes) != 1 || in.Indexes[0].Name != "by-email" {
		t.Fatalf("table = %+v %v", in, err)
	}
	ro, _ := s.ReadOnlySession()
	if r := ro.Run(ctx, `DELETE FROM "users" WHERE pk = 'u#1'`, RunOptions{}); r.Err == nil || !strings.Contains(r.Err.Error(), "read-only") {
		t.Fatalf("read-only = %v", r.Err)
	}
}

// dynamodb://KEY@dynamodb.eu-west-1.amazonaws.com: the region from the
// host; DynamoDB Local on localhost stays on plain HTTP.
func TestDynamoDBURL(t *testing.T) {
	c, err := ParseURL("dynamodb://AKIDTEST@dynamodb.eu-west-1.amazonaws.com")
	if err != nil {
		t.Fatal(err)
	}
	if c.Driver != "dynamodb" || !c.TLS || c.Port != 443 || c.User != "AKIDTEST" || c.Params["region"] != "eu-west-1" {
		t.Fatalf("conn = %+v", c)
	}
	c, err = ParseURL("dynamodb://local@localhost:8000?region=us-east-1")
	if err != nil {
		t.Fatal(err)
	}
	if c.TLS || c.Port != 8000 || c.Host != "localhost" || c.Params["region"] != "us-east-1" {
		t.Fatalf("local = %+v", c)
	}
}
