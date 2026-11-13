package dbclient

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func mongoSess(f MongoExecutor) *MongoSession {
	return NewMongoSessionWith(Conn{Driver: "mongodb", Database: "app"}, f)
}

func okDoc(fields ...MongoField) MongoDoc {
	return append(MongoDoc{{Key: "ok", Value: 1.0}}, fields...)
}

func TestMongoRunResultKinds(t *testing.T) {
	oid := MongoObjectID("650000000000000000000001")
	f := &fakeMongo{answers: map[string]MongoDoc{
		"count":         okDoc(MongoField{Key: "n", Value: int32(7)}),
		"distinct":      okDoc(MongoField{Key: "values", Value: []any{"a", MongoDoc{{Key: "k", Value: 1.0}}, nil, 2.5}}),
		"collStats":     okDoc(MongoField{Key: "ns", Value: "app.c"}, MongoField{Key: "size", Value: int64(10)}, MongoField{Key: "$clusterTime", Value: "x"}),
		"insert":        okDoc(MongoField{Key: "n", Value: int32(2)}),
		"update":        okDoc(MongoField{Key: "n", Value: int32(3)}, MongoField{Key: "nModified", Value: int32(2)}, MongoField{Key: "upserted", Value: []any{MongoDoc{}}}),
		"delete":        okDoc(MongoField{Key: "n", Value: int32(1)}),
		"findAndModify": okDoc(MongoField{Key: "lastErrorObject", Value: MongoDoc{{Key: "n", Value: int32(1)}}}, MongoField{Key: "value", Value: MongoDoc{{Key: "_id", Value: oid}, {Key: "a", Value: 1.0}}}),
		"createIndexes": okDoc(MongoField{Key: "numIndexesBefore", Value: int32(1)}, MongoField{Key: "numIndexesAfter", Value: int32(2)}),
		"drop":          okDoc(),
		"explain":       okDoc(MongoField{Key: "queryPlanner", Value: MongoDoc{{Key: "winning", Value: "ix"}}}),
	}}
	s := mongoSess(f)
	defer s.Close()
	ctx := context.Background()
	run := func(src string) Result {
		t.Helper()
		return s.Run(ctx, src, RunOptions{})
	}
	if r := run("db.c.count()"); r.Err != nil || r.Rows[0][0].Text != "7" {
		t.Errorf("count = %+v", r)
	}
	if r := run(`db.c.distinct("a")`); r.Err != nil || len(r.Rows) != 4 || r.Rows[1][0].Kind != "json" {
		t.Errorf("distinct = %+v", r)
	}
	if r := run("db.c.stats()"); r.Err != nil || len(r.Rows) != 2 {
		t.Errorf("stats = %+v", r)
	}
	if r := run(`db.c.insertOne({a: 1})`); r.Err != nil || r.Affected != 2 || !strings.Contains(r.Notices[0], "inserted 2") {
		t.Errorf("insert = %+v", r)
	}
	if r := run(`db.c.updateOne({a: 1}, {$set: {b: 2}})`); r.Err != nil || r.Affected != 2 || !strings.Contains(r.Notices[0], "matched 2, modified 2, upserted 1") {
		t.Errorf("update = %+v", r)
	}
	if r := run(`db.c.deleteMany({})`); r.Err != nil || r.Affected != 1 || !strings.Contains(r.Notices[0], "deleted 1") {
		t.Errorf("delete = %+v", r)
	}
	if r := run(`db.c.findOneAndUpdate({a: 1}, {$set: {b: 1}})`); r.Err != nil || r.Affected != 1 || len(r.Rows) != 1 {
		t.Errorf("findAndModify = %+v", r)
	}
	if r := run(`db.c.createIndex({a: 1})`); r.Err != nil || !strings.Contains(r.Notices[0], "1 before, 2 after") {
		t.Errorf("createIndex = %+v", r)
	}
	if r := run("db.c.drop()"); r.Err != nil || r.Notices[0] != "done" {
		t.Errorf("drop = %+v", r)
	}
	if r := run(`db.c.find({a: 1}).explain()`); r.Err != nil || r.Columns[0].Name != "plan" || !strings.Contains(r.Rows[0][0].Text, "winning") {
		t.Errorf("explain = %+v", r)
	}
	// An open cursor is killed; a kill that fails says so.
	f.answers["find"] = cursorAnswer(55, MongoDoc{{Key: "_id", Value: 1.0}})
	if r := run("db.c.find()"); r.Err != nil || !r.Truncated || !strings.Contains(strings.Join(f.names(), ","), "killCursors") {
		t.Errorf("open cursor = %+v, calls %v", r, f.names())
	}
	// A cursor of a view or aggregate with no collection name uses the namespace.
	f.answers["aggregate"] = MongoDoc{{Key: "cursor", Value: MongoDoc{{Key: "id", Value: int64(9)}, {Key: "ns", Value: "app.v"}, {Key: "firstBatch", Value: []any{}}}}}
	if r := run("db.c.aggregate([])"); r.Err != nil || !r.Truncated {
		t.Errorf("aggregate cursor = %+v", r)
	}
	// Write errors in the reply.
	f.answers["insert"] = okDoc(MongoField{Key: "writeErrors", Value: []any{MongoDoc{{Key: "errmsg", Value: "dup key"}}, MongoDoc{}}})
	if r := run(`db.c.insertOne({a: 1})`); r.Err == nil || !strings.Contains(r.Err.Error(), "dup key (and 1 more)") {
		t.Errorf("write errors = %v", r.Err)
	}
	f.answers["insert"] = okDoc(MongoField{Key: "writeErrors", Value: []any{"odd"}})
	if r := run(`db.c.insertOne({a: 1})`); r.Err == nil || !strings.Contains(r.Err.Error(), "the write failed") {
		t.Errorf("odd write error = %v", r.Err)
	}
	f.answers["insert"] = okDoc(MongoField{Key: "writeConcernError", Value: MongoDoc{{Key: "errmsg", Value: "timeout"}}})
	if r := run(`db.c.insertOne({a: 1})`); r.Err == nil || !strings.Contains(r.Err.Error(), "write concern: timeout") {
		t.Errorf("write concern = %v", r.Err)
	}
	f.err = errors.New("link down")
	if r := run("db.c.find()"); r.Err == nil {
		t.Error("a failing server: want error")
	}
	f.err = nil
	if _, err := s.Introspect(ctx, IntrospectionRequest{}); err != nil {
		t.Errorf("databases = %v", err)
	}
	f.err = errors.New("down")
	if _, err := s.Introspect(ctx, IntrospectionRequest{}); err == nil {
		t.Error("databases on a failing server: want error")
	}
	f.err = nil
	s.Cancel()
	_ = s.Close()
	_ = s.Close()
	if !f.closed {
		t.Error("Close closes the client")
	}
	if r := s.Run(ctx, "db.c.find()", RunOptions{}); r.Err == nil {
		t.Error("a closed session: want error")
	}
}

func TestMongoIntrospectCollectionsViewsAndIndexes(t *testing.T) {
	f := &fakeMongo{dbs: []string{"b", "a"}, answers: map[string]MongoDoc{
		"listCollections": cursorAnswer(0,
			MongoDoc{{Key: "name", Value: "zeta"}, {Key: "type", Value: "collection"}},
			MongoDoc{{Key: "name", Value: "alpha"}, {Key: "type", Value: "view"}},
			MongoDoc{{Key: "name", Value: ""}}, MongoDoc{{Key: "name", Value: "beta"}}),
		"find": cursorAnswer(0, MongoDoc{{Key: "_id", Value: 1.0}, {Key: "name", Value: "x"}}),
		"listIndexes": cursorAnswer(0,
			MongoDoc{{Key: "v", Value: int32(2)}, {Key: "key", Value: MongoDoc{{Key: "_id", Value: int32(1)}}}, {Key: "name", Value: "_id_"}},
			MongoDoc{{Key: "v", Value: int32(2)}, {Key: "key", Value: MongoDoc{{Key: "name", Value: int32(1)}}}, {Key: "name", Value: "name_1"}, {Key: "unique", Value: true}}),
	}}
	s := mongoSess(f)
	ctx := context.Background()
	top, err := s.Introspect(ctx, IntrospectionRequest{})
	if err != nil || strings.Join(top.Databases, ",") != "a,b" {
		t.Errorf("databases = %+v %v", top, err)
	}
	objs, err := s.Introspect(ctx, IntrospectionRequest{Schema: "app"})
	if err != nil || len(objs.Objects) != 3 || objs.Objects[2].Kind != "view" || objs.Objects[0].Name != "beta" {
		t.Errorf("objects = %+v %v", objs, err)
	}
	if _, err := s.Introspect(ctx, IntrospectionRequest{Object: "x"}); err != nil {
		t.Errorf("default database = %v", err)
	}
	cols, err := s.Introspect(ctx, IntrospectionRequest{Database: "app", Object: "beta"})
	if err != nil || len(cols.Columns) != 2 || len(cols.Indexes) != 2 || !cols.Indexes[1].Unique || !strings.Contains(cols.DDL, `createIndex({"name": 1}, {"name": "name_1", "unique": true})`) {
		t.Errorf("collection = %+v %v", cols, err)
	}
	view, err := s.Introspect(ctx, IntrospectionRequest{Database: "app", Object: "alpha", Kind: "view"})
	if err != nil || len(view.Indexes) != 0 || !strings.HasPrefix(view.DDL, "// app.alpha:") {
		t.Errorf("view = %+v %v", view, err)
	}
	for _, name := range []string{"listCollections", "find", "listIndexes"} {
		bad := &fakeMongo{answers: f.answers}
		bad.err = errors.New("denied")
		req := IntrospectionRequest{Database: "app"}
		if name != "listCollections" {
			req.Object = "beta"
		}
		if _, err := mongoSess(bad).Introspect(ctx, req); err == nil {
			t.Errorf("%s failing: want error", name)
		}
	}
	// listIndexes alone failing.
	one := &fakeMongo{answers: f.answers}
	s2 := mongoSess(&failOn{fakeMongo: one, key: "listIndexes"})
	if _, err := s2.Introspect(ctx, IntrospectionRequest{Database: "app", Object: "beta"}); err == nil {
		t.Error("listIndexes failing: want error")
	}
}

type failOn struct {
	*fakeMongo
	key string
}

func (f *failOn) Command(ctx context.Context, db string, cmd MongoDoc) (MongoDoc, error) {
	if cmd[0].Key == f.key {
		return nil, errors.New("denied")
	}
	return f.fakeMongo.Command(ctx, db, cmd)
}

func TestMongoValueNamesAndJSON(t *testing.T) {
	for _, c := range []struct {
		v    any
		want string
	}{
		{nil, "null"}, {MongoObjectID("a"), "objectId"}, {"s", "string"}, {int32(1), "int"}, {int64(1), "long"}, {1.5, "double"},
		{true, "bool"}, {time.Now(), "date"}, {MongoDoc{}, "object"}, {[]any{}, "array"}, {[]byte("x"), "binData"}, {struct{}{}, ""},
	} {
		if got := mongoTypeName(c.v); got != c.want {
			t.Errorf("mongoTypeName(%T) = %q, want %q", c.v, got, c.want)
		}
	}
	doc := MongoDoc{{Key: "a", Value: []any{int32(1), MongoDoc{}, []any{}}}, {Key: "t", Value: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)},
		{Key: "b", Value: []byte("hi")}, {Key: "f", Value: 1.5}, {Key: "n", Value: nil}, {Key: "w", Value: struct{ X int }{1}}}
	compact, pretty := MongoShellJSON(doc), mongoPrettyJSON(doc)
	if !strings.Contains(compact, "ISODate") || !strings.Contains(pretty, "\n  ") {
		t.Errorf("json:\n%s\n%s", compact, pretty)
	}
	if mongoPrettyJSON(MongoDoc{}) != "{}" || mongoPrettyJSON([]any{}) != "[]" {
		t.Errorf("empty = %q %q", mongoPrettyJSON(MongoDoc{}), mongoPrettyJSON([]any{}))
	}
	for _, v := range []any{nil, MongoObjectID("65"), []any{1.0}, 2.5, "s", true} {
		_ = mongoCell(v)
	}
	if mongoQuote(`a"<b>`) != `"a\"<b>"` {
		t.Errorf("mongoQuote = %q", mongoQuote(`a"<b>`))
	}
}

func TestNewMongoSessionRefusals(t *testing.T) {
	ctx := context.Background()
	if _, err := NewMongoSession(ctx, Conn{Driver: "mongodb"}, ""); err == nil || !strings.Contains(err.Error(), "host is needed") {
		t.Errorf("no host = %v", err)
	}
	if _, err := NewMongoSession(ctx, Conn{Driver: "mongodb", URL: "http://x"}, ""); err == nil {
		t.Error("a wrong URL scheme: want error")
	}
	srv := Conn{Driver: "mongodb", URL: "mongodb+srv://u@cluster.example/db", Dial: func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("no") }}
	if _, err := NewMongoSession(ctx, srv, ""); err == nil || !strings.Contains(err.Error(), "SSH tunnel") {
		t.Errorf("srv through a tunnel = %v", err)
	}
	if _, err := NewMongoSession(ctx, Conn{Driver: "mongodb", Host: "h", TLS: true, SSL: &SSLConfig{Mode: "verify-full", CA: "/no/such.pem"}}, ""); err == nil {
		t.Error("an unreadable CA: want error")
	}
	// A client is made without connecting; closing it is safe.
	s, err := NewMongoSession(ctx, Conn{Driver: "mongodb", Host: "127.0.0.1", Port: 1, User: "u", Dial: func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("no") }}, "pw")
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
}
