package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DBX-68: a DynamoDB result in the JSON view is a document view: each item
// is a JSON document with its sets, lists and maps nested, and the chip says
// Documents. Other engines keep the plain JSON view.
func docViewPage(t *testing.T, driver string) string {
	t.Helper()
	s := newDBState()
	s.conns = []dbclient.Conn{{ID: "c", Driver: driver}}
	m := dbTestModel(s)
	m.w, m.h = 120, 40
	res := dbclient.DocsResult(nil, 0)
	res.Columns = []dbclient.Column{{Name: "id"}, {Name: "tags"}}
	res.Rows = [][]dbclient.Cell{{{Text: "a1"}, {Text: `["x","y"]`, Kind: "json"}}}
	c := m.dbConsole()
	c.shown, c.size, c.sortCol, c.view = &dbRun{conn: "c", sql: "SELECT * FROM t", result: res}, 100, -1, dbViewJSON
	return strings.Join(m.databasePage().lines, "\n")
}

func TestDynamoDBJSONViewIsADocumentView(t *testing.T) {
	text := docViewPage(t, "dynamodb")
	if !strings.Contains(text, "Documents") {
		t.Fatalf("no Documents chip:\n%s", text)
	}
	if !strings.Contains(text, `"tags": [`) || strings.Contains(text, `\"x\"`) {
		t.Fatalf("tags are not nested JSON:\n%s", text)
	}
}

func TestOtherEnginesKeepThePlainJSONView(t *testing.T) {
	text := docViewPage(t, "postgres")
	if strings.Contains(text, "Documents") || !strings.Contains(text, `\"x\"`) {
		t.Fatalf("plain JSON view changed:\n%s", text)
	}
}

// DBX-68: a MongoDB result in the JSON view is the same document view, in
// shell syntax: ObjectId and nested documents as the shell writes them.
func TestMongoJSONViewIsADocumentView(t *testing.T) {
	s := newDBState()
	s.conns = []dbclient.Conn{{ID: "c", Driver: "mongodb"}}
	m := dbTestModel(s)
	m.w, m.h = 120, 40
	res := dbclient.Result{Columns: []dbclient.Column{{Name: "_id", PK: true}, {Name: "n"}, {Name: "addr"}}}
	res.Rows = [][]dbclient.Cell{{{Text: `ObjectId("6501")`, Kind: "literal"}, {Text: "7", Kind: "number"}, {Text: `{"city": "Rome"}`, Kind: "json"}}}
	c := m.dbConsole()
	c.shown, c.size, c.sortCol, c.view = &dbRun{conn: "c", sql: "db.users.find()", result: res}, 100, -1, dbViewJSON
	text := strings.Join(m.databasePage().lines, "\n")
	for _, want := range []string{"Documents", `"_id": ObjectId("6501")`, `"n": 7`, `"city": "Rome"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("no %q:\n%s", want, text)
		}
	}
}
