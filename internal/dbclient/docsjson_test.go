package dbclient

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// DBX-68: items as documents. The grid keeps nested values as compact JSON
// text; the document form puts them back as JSON values, and a typed result
// keeps numbers and booleans apart from strings.
func TestToDocsJSONNestsDocumentCells(t *testing.T) {
	res := TypedDocsResult([]json.RawMessage{
		json.RawMessage(`{"id":"a1","s":"3","qty":3,"ok":true,"tags":["x","y"],"meta":{"n":1,"deep":{"k":true}},"gone":null}`),
		json.RawMessage(`{"id":"b2","qty":4}`),
	}, 0)
	got := ToDocsJSON(res)
	want := `[{"id":"a1","s":"3","qty":3,"ok":true,"tags":["x","y"],"meta":{"n":1,"deep":{"k":true}},"gone":null},{"id":"b2","s":null,"qty":4,"ok":null,"tags":null,"meta":null,"gone":null}]`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if plain := DocsResult([]json.RawMessage{json.RawMessage(`{"q":3}`)}, 0); plain.Rows[0][0].Kind != "" {
		t.Errorf("DocsResult changed: %+v", plain.Rows[0][0])
	}
}

// DynamoDB items come out as typed documents.
func TestDynamoDBItemsAsDocuments(t *testing.T) {
	f := &fakeDynamo{}
	srv := httptest.NewServer(f.handler(t))
	defer srv.Close()
	s, err := OpenHTTPEngine(Conn{Driver: "dynamodb", URL: srv.URL, User: "AKIDTEST", Params: map[string]string{"region": "eu-west-1"}}, "secret-key-1")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	res := s.Run(context.Background(), `SELECT * FROM "users" WHERE pk = ?`, RunOptions{Args: []any{"u#1"}})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	docs := ToDocsJSON(res)
	for _, want := range []string{`"n":1.5`, `"tags":["a","b"]`, `"meta":{"gone":null,"ok":true}`, `"list":[1,"x"]`} {
		if !strings.Contains(docs, want) {
			t.Errorf("documents lack %s:\n%s", want, docs)
		}
	}
}

// A json cell that is not valid JSON (a shell form such as ObjectId("…"))
// stays a string; plain ToJSON keeps every json cell a string.
func TestToDocsJSONKeepsInvalidJSONAsText(t *testing.T) {
	res := Result{Columns: []Column{{Name: "_id"}, {Name: "doc"}}, Rows: [][]Cell{{
		{Text: `ObjectId("5f")`, Kind: "json"}, {Text: `{"a":1}`, Kind: "json"},
	}}}
	if got := ToDocsJSON(res); got != `[{"_id":"ObjectId(\"5f\")","doc":{"a":1}}]` {
		t.Fatalf("docs = %s", got)
	}
	if got := ToJSON(res); got != `[{"_id":"ObjectId(\"5f\")","doc":"{\"a\":1}"}]` {
		t.Fatalf("plain = %s", got)
	}
}
