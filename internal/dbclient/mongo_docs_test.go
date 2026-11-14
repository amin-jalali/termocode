package dbclient

import (
	"strings"
	"testing"
	"time"
)

// DBX-68: a MongoDB result reads back as shell documents: _id as ObjectId,
// numbers and booleans bare, nested documents and arrays nested, dates as
// ISODate, missing fields left out.
func TestToMongoDocsWritesShellDocuments(t *testing.T) {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	docs := []any{
		MongoDoc{{Key: "_id", Value: MongoObjectID("6501")}, {Key: "n", Value: int32(3)}, {Key: "ok", Value: true},
			{Key: "name", Value: "42"}, {Key: "at", Value: when},
			{Key: "tags", Value: []any{"x", MongoDoc{{Key: "k", Value: 1.5}}}}},
		MongoDoc{{Key: "_id", Value: MongoObjectID("6502")}, {Key: "name", Value: "b"}},
	}
	var res Result
	res.Columns, res.Rows = mongoRows(docs)
	got := ToMongoDocs(res)
	want := `{
  "_id": ObjectId("6501"),
  "n": 3,
  "ok": true,
  "name": "42",
  "at": ISODate("2026-01-02T03:04:05Z"),
  "tags": [
    "x",
    {
      "k": 1.5
    }
  ]
}
{
  "_id": ObjectId("6502"),
  "name": "b"
}`
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A string that holds brackets or commas stays one string.
func TestToMongoDocsKeepsStringsWhole(t *testing.T) {
	res := Result{Columns: []Column{{Name: "s"}, {Name: "d"}}, Rows: [][]Cell{{
		{Text: `a, {b}`}, {Text: MongoShellJSON(MongoDoc{{Key: "q", Value: `x, [y]"`}}), Kind: "json"},
	}}}
	got := ToMongoDocs(res)
	if !strings.Contains(got, `"s": "a, {b}"`) || !strings.Contains(got, `"q": "x, [y]\""`) {
		t.Fatalf("strings were split:\n%s", got)
	}
}
