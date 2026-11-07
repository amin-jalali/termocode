package dbclient

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeMongo is a MongoExecutor that answers each command by its name (the
// first key of the command document).
type fakeMongo struct {
	mu      sync.Mutex
	dbs     []string
	answers map[string]MongoDoc
	calls   []fakeMongoCall
	err     error
	closed  bool
}

type fakeMongoCall struct {
	db  string
	cmd MongoDoc
}

func (f *fakeMongo) ListDatabases(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.dbs, f.err
}

func (f *fakeMongo) Command(_ context.Context, db string, cmd MongoDoc) (MongoDoc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeMongoCall{db: db, cmd: cmd})
	if f.err != nil {
		return nil, f.err
	}
	if len(cmd) == 0 {
		return nil, errors.New("empty command")
	}
	if a, ok := f.answers[cmd[0].Key]; ok {
		return a, nil
	}
	return MongoDoc{{Key: "ok", Value: 1.0}}, nil
}

func (f *fakeMongo) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
}

func (f *fakeMongo) last(t *testing.T) fakeMongoCall {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		t.Fatal("no command was sent")
	}
	return f.calls[len(f.calls)-1]
}

func (f *fakeMongo) names() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		out = append(out, c.cmd[0].Key)
	}
	return out
}

func cursorAnswer(id int64, docs ...MongoDoc) MongoDoc {
	batch := make([]any, len(docs))
	for i, d := range docs {
		batch[i] = d
	}
	return MongoDoc{{Key: "cursor", Value: MongoDoc{{Key: "id", Value: id}, {Key: "firstBatch", Value: batch}}}, {Key: "ok", Value: 1.0}}
}

func mustParse(t *testing.T, src string) MongoCommand {
	t.Helper()
	c, err := ParseMongoShell(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return c
}

// DBX-68: a shell line becomes the server command the shell itself sends.
func TestMongoServerCommandFind(t *testing.T) {
	cmd, err := MongoServerCommand(mustParse(t, `db.users.find({age: {$gt: 30}}, {name: 1}).sort({name: -1}).skip(5).limit(10)`), 500)
	if err != nil {
		t.Fatal(err)
	}
	got := MongoShellJSON(cmd)
	want := `{"find": "users", "filter": {"age": {"$gt": 30}}, "projection": {"name": 1}, "sort": {"name": -1}, "skip": 5, "limit": 10, "batchSize": 10}`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestMongoServerCommandShapes(t *testing.T) {
	cases := []struct{ src, want string }{
		{`db.users.find()`, `{"find": "users", "filter": {}, "batchSize": 500}`},
		{`db.users.findOne({a: 1})`, `{"find": "users", "filter": {"a": 1}, "limit": 1, "singleBatch": true}`},
		{`db.orders.aggregate([{$match: {s: "x"}}, {$group: {_id: "$c", n: {$sum: 1}}}])`, `{"aggregate": "orders", "pipeline": [{"$match": {"s": "x"}}, {"$group": {"_id": "$c", "n": {"$sum": 1}}}], "cursor": {"batchSize": 500}}`},
		{`db.users.countDocuments({a: 1})`, `{"count": "users", "query": {"a": 1}}`},
		{`db.users.estimatedDocumentCount()`, `{"count": "users"}`},
		{`db.users.distinct("city", {a: 1})`, `{"distinct": "users", "key": "city", "query": {"a": 1}}`},
		{`db.users.getIndexes()`, `{"listIndexes": "users", "cursor": {}}`},
		{`show collections`, `{"listCollections": 1, "nameOnly": false}`},
		{`db.users.stats()`, `{"collStats": "users"}`},
		{`db.users.insertOne({name: "a"})`, `{"insert": "users", "documents": [{"name": "a"}]}`},
		{`db.users.insertMany([{n: 1}, {n: 2}])`, `{"insert": "users", "documents": [{"n": 1}, {"n": 2}]}`},
		{`db.users.updateOne({a: 1}, {$set: {b: 2}})`, `{"update": "users", "updates": [{"q": {"a": 1}, "u": {"$set": {"b": 2}}, "multi": false, "upsert": false}]}`},
		{`db.users.updateMany({}, {$inc: {n: 1}}, {upsert: true})`, `{"update": "users", "updates": [{"q": {}, "u": {"$inc": {"n": 1}}, "multi": true, "upsert": true}]}`},
		{`db.users.replaceOne({a: 1}, {a: 2})`, `{"update": "users", "updates": [{"q": {"a": 1}, "u": {"a": 2}, "multi": false, "upsert": false}]}`},
		{`db.users.deleteOne({a: 1})`, `{"delete": "users", "deletes": [{"q": {"a": 1}, "limit": 1}]}`},
		{`db.users.deleteMany({})`, `{"delete": "users", "deletes": [{"q": {}, "limit": 0}]}`},
		{`db.users.findOneAndDelete({a: 1})`, `{"findAndModify": "users", "query": {"a": 1}, "remove": true}`},
		{`db.users.findOneAndUpdate({a: 1}, {$set: {b: 1}})`, `{"findAndModify": "users", "query": {"a": 1}, "update": {"$set": {"b": 1}}}`},
		{`db.users.createIndex({email: 1, at: -1}, {unique: true})`, `{"createIndexes": "users", "indexes": [{"key": {"email": 1, "at": -1}, "name": "email_1_at_-1", "unique": true}]}`},
		{`db.users.createIndex({email: 1}, {name: "by_mail"})`, `{"createIndexes": "users", "indexes": [{"key": {"email": 1}, "name": "by_mail"}]}`},
		{`db.users.dropIndex("email_1")`, `{"dropIndexes": "users", "index": "email_1"}`},
		{`db.users.dropIndexes()`, `{"dropIndexes": "users", "index": "*"}`},
		{`db.users.drop()`, `{"drop": "users"}`},
		{`db.users.find({a: 1}).explain("executionStats")`, `{"explain": {"find": "users", "filter": {"a": 1}, "batchSize": 500}, "verbosity": "executionStats"}`},
		{`db.users.find().explain()`, `{"explain": {"find": "users", "filter": {}, "batchSize": 500}, "verbosity": "queryPlanner"}`},
		{`db.users.find({_id: ObjectId("650000000000000000000001")})`, `{"find": "users", "filter": {"_id": ObjectId("650000000000000000000001")}, "batchSize": 500}`},
	}
	for _, c := range cases {
		cmd, err := MongoServerCommand(mustParse(t, c.src), 500)
		if err != nil {
			t.Errorf("%s: %v", c.src, err)
			continue
		}
		if got := MongoShellJSON(cmd); got != c.want {
			t.Errorf("%s\n got  %s\n want %s", c.src, got, c.want)
		}
	}
}

func TestMongoServerCommandRejects(t *testing.T) {
	for _, src := range []string{
		`db.users.bulkWrite([])`,
		`db.users.aggregate([]).limit(3)`,
		`db.users.find(1)`,
		`db.users.insertOne()`,
		`db.users.updateOne({a: 1})`,
		`db.users.renameCollection("x")`,
		`db.users.mapReduce("f", "g")`,
		`db.users.find().explain("everything")`,
		`db.users.dropIndex()`,
	} {
		if _, err := MongoServerCommand(mustParse(t, src), 500); err == nil {
			t.Errorf("%s: no error", src)
		}
	}
}

// The shell's ObjectId("…") and ISODate("…") parse into real values.
func TestParseMongoShellObjectIDAndDate(t *testing.T) {
	c := mustParse(t, `db.e.find({_id: ObjectId('650000000000000000000001'), at: {$gte: ISODate("2026-01-02T03:04:05Z")}})`)
	f := c.Args[0].(MongoDoc)
	if id, ok := f[0].Value.(MongoObjectID); !ok || id != "650000000000000000000001" {
		t.Fatalf("id = %#v", f[0].Value)
	}
	at := f[1].Value.(MongoDoc)[0].Value
	if tm, ok := at.(time.Time); !ok || !tm.Equal(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("date = %#v", at)
	}
	for _, bad := range []string{`db.e.find({_id: ObjectId("zz")})`, `db.e.find({_id: ObjectId()})`, `db.e.find({at: ISODate("yesterday")})`} {
		if _, err := ParseMongoShell(bad); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}
}

// find shows documents as rows: _id first, then every top-level key in the
// order first seen; nested values as compact shell JSON.
func TestMongoRunFindRows(t *testing.T) {
	f := &fakeMongo{answers: map[string]MongoDoc{
		"find": cursorAnswer(0,
			MongoDoc{{Key: "name", Value: "ann"}, {Key: "_id", Value: MongoObjectID("650000000000000000000001")}, {Key: "tags", Value: []any{"a", "b"}}},
			MongoDoc{{Key: "_id", Value: MongoObjectID("650000000000000000000002")}, {Key: "addr", Value: MongoDoc{{Key: "city", Value: "Rome"}}}, {Key: "n", Value: int32(3)}},
		),
	}}
	s := NewMongoSessionWith(Conn{ID: "m", Driver: "mongodb", Database: "shop"}, f)
	res := s.Run(context.Background(), `db.users.find()`, RunOptions{})
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	var cols []string
	for _, c := range res.Columns {
		cols = append(cols, c.Name)
	}
	if strings.Join(cols, ",") != "_id,name,tags,addr,n" {
		t.Fatalf("columns = %v", cols)
	}
	if !res.Columns[0].PK {
		t.Fatal("_id is not marked as the key")
	}
	r0, r1 := res.Rows[0], res.Rows[1]
	if r0[0].Text != `ObjectId("650000000000000000000001")` || r0[1].Text != "ann" || r0[2].Text != `["a", "b"]` || r0[2].Kind != "json" || !r0[3].Null {
		t.Fatalf("row 0 = %+v", r0)
	}
	if r1[3].Text != `{"city": "Rome"}` || r1[4].Text != "3" {
		t.Fatalf("row 1 = %+v", r1)
	}
	if c := f.last(t); c.db != "shop" {
		t.Fatalf("ran in %q, want shop", c.db)
	}
	if res.Truncated {
		t.Fatal("cursor 0 means no more documents")
	}
}

// A cursor that has more documents marks the page truncated and is killed,
// so the server does not keep it open.
func TestMongoRunKillsOpenCursor(t *testing.T) {
	f := &fakeMongo{answers: map[string]MongoDoc{"find": cursorAnswer(77, MongoDoc{{Key: "_id", Value: 1.0}})}}
	s := NewMongoSessionWith(Conn{Driver: "mongodb", Database: "shop"}, f)
	res := s.Run(context.Background(), `db.users.find()`, RunOptions{MaxRows: 1})
	if res.Err != nil || !res.Truncated {
		t.Fatalf("err=%v truncated=%v", res.Err, res.Truncated)
	}
	c := f.last(t)
	if MongoShellJSON(c.cmd) != `{"killCursors": "users", "cursors": [77]}` {
		t.Fatalf("last command = %s", MongoShellJSON(c.cmd))
	}
}

func TestMongoRunCountsAndWrites(t *testing.T) {
	f := &fakeMongo{answers: map[string]MongoDoc{
		"count":  {{Key: "n", Value: int32(42)}, {Key: "ok", Value: 1.0}},
		"update": {{Key: "n", Value: int32(3)}, {Key: "nModified", Value: int32(2)}, {Key: "ok", Value: 1.0}},
		"delete": {{Key: "n", Value: int32(0)}, {Key: "writeErrors", Value: []any{MongoDoc{{Key: "index", Value: int32(0)}, {Key: "errmsg", Value: "not allowed"}}}}, {Key: "ok", Value: 1.0}},
	}}
	s := NewMongoSessionWith(Conn{Driver: "mongodb", Database: "shop"}, f)
	res := s.Run(context.Background(), `db.users.countDocuments({})`, RunOptions{})
	if res.Err != nil || len(res.Rows) != 1 || res.Columns[0].Name != "count" || res.Rows[0][0].Text != "42" {
		t.Fatalf("count = %+v", res)
	}
	res = s.Run(context.Background(), `db.users.updateMany({}, {$set: {a: 1}})`, RunOptions{})
	if res.Err != nil || res.Affected != 2 || len(res.Notices) == 0 || !strings.Contains(res.Notices[0], "matched 3") {
		t.Fatalf("update = %+v", res)
	}
	res = s.Run(context.Background(), `db.users.deleteMany({})`, RunOptions{})
	if res.Err == nil || !strings.Contains(res.Err.Error(), "not allowed") {
		t.Fatalf("write error not shown: %v", res.Err)
	}
}

// explain shows the plan as one pretty JSON cell.
func TestMongoRunExplain(t *testing.T) {
	f := &fakeMongo{answers: map[string]MongoDoc{"explain": {{Key: "queryPlanner", Value: MongoDoc{{Key: "winningPlan", Value: MongoDoc{{Key: "stage", Value: "COLLSCAN"}}}}}, {Key: "ok", Value: 1.0}}}}
	s := NewMongoSessionWith(Conn{Driver: "mongodb", Database: "shop"}, f)
	res := s.Run(context.Background(), `db.users.find({a: 1}).explain()`, RunOptions{})
	if res.Err != nil || len(res.Rows) != 1 || res.Columns[0].Name != "plan" || res.Rows[0][0].Kind != "json" || !strings.Contains(res.Rows[0][0].Text, "COLLSCAN") {
		t.Fatalf("explain = %+v", res)
	}
}

// Comment lines (the console header) are skipped; "use" switches database.
func TestMongoRunCommentsAndUse(t *testing.T) {
	f := &fakeMongo{answers: map[string]MongoDoc{"find": cursorAnswer(0)}}
	s := NewMongoSessionWith(Conn{Driver: "mongodb", Database: "shop"}, f)
	if res := s.Run(context.Background(), "-- @conn m\n// note\nuse logs", RunOptions{}); res.Err != nil {
		t.Fatal(res.Err)
	}
	if res := s.Run(context.Background(), "-- @conn m\ndb.events.find()", RunOptions{}); res.Err != nil {
		t.Fatal(res.Err)
	}
	if c := f.last(t); c.db != "logs" {
		t.Fatalf("ran in %q, want logs", c.db)
	}
	if res := s.Run(context.Background(), "use bad/name", RunOptions{}); res.Err == nil {
		t.Fatal("a bad database name was accepted")
	}
}

// A read-only session (and the AI's) never sends a write.
func TestMongoReadOnlyRefusesWrites(t *testing.T) {
	f := &fakeMongo{answers: map[string]MongoDoc{"find": cursorAnswer(0)}}
	s := NewMongoSessionWith(Conn{Driver: "mongodb", Database: "shop"}, f)
	ro, err := s.ReadOnlySession()
	if err != nil {
		t.Fatal(err)
	}
	for _, src := range []string{`db.u.insertOne({a: 1})`, `db.u.drop()`, `db.u.aggregate([{$out: "x"}])`, `db.u.deleteMany({})`} {
		if res := ro.Run(context.Background(), src, RunOptions{}); res.Err == nil {
			t.Errorf("%s ran on a read-only session", src)
		}
	}
	if len(f.calls) != 0 {
		t.Fatalf("commands sent: %v", f.names())
	}
	if res := ro.Run(context.Background(), `db.u.find()`, RunOptions{}); res.Err != nil {
		t.Fatal(res.Err)
	}
	_ = ro.Close()
	if f.closed {
		t.Fatal("closing the read-only view closed the shared client")
	}
	_ = s.Close()
	if !f.closed {
		t.Fatal("Close did not close the client")
	}
}

func TestMongoNoTransactions(t *testing.T) {
	s := NewMongoSessionWith(Conn{Driver: "mongodb"}, &fakeMongo{})
	for _, err := range []error{s.Begin(context.Background()), s.Commit(context.Background()), s.Rollback(context.Background())} {
		if !errors.Is(err, ErrMongoNoTransactions) {
			t.Fatalf("err = %v", err)
		}
	}
}

func TestMongoPing(t *testing.T) {
	f := &fakeMongo{}
	s := NewMongoSessionWith(Conn{Driver: "mongodb"}, f)
	if err := s.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := f.last(t); c.db != "admin" || MongoShellJSON(c.cmd) != `{"ping": 1}` {
		t.Fatalf("ping = %s on %s", MongoShellJSON(c.cmd), c.db)
	}
}

// The tree: databases, then collections and views, then the fields of the
// first documents, the indexes and their createIndex lines.
func TestMongoIntrospect(t *testing.T) {
	f := &fakeMongo{
		dbs: []string{"shop", "admin", "logs"},
		answers: map[string]MongoDoc{
			"listCollections": cursorAnswer(0,
				MongoDoc{{Key: "name", Value: "users"}, {Key: "type", Value: "collection"}},
				MongoDoc{{Key: "name", Value: "active"}, {Key: "type", Value: "view"}},
			),
			"find": cursorAnswer(0,
				MongoDoc{{Key: "_id", Value: MongoObjectID("650000000000000000000001")}, {Key: "email", Value: "a@x"}},
				MongoDoc{{Key: "_id", Value: MongoObjectID("650000000000000000000002")}, {Key: "age", Value: int32(3)}},
			),
			"listIndexes": cursorAnswer(0,
				MongoDoc{{Key: "v", Value: int32(2)}, {Key: "key", Value: MongoDoc{{Key: "_id", Value: int32(1)}}}, {Key: "name", Value: "_id_"}},
				MongoDoc{{Key: "v", Value: int32(2)}, {Key: "key", Value: MongoDoc{{Key: "email", Value: int32(1)}}}, {Key: "name", Value: "email_1"}, {Key: "unique", Value: true}},
			),
		},
	}
	s := NewMongoSessionWith(Conn{Driver: "mongodb", Database: "shop"}, f)
	ctx := context.Background()
	top, err := s.Introspect(ctx, IntrospectionRequest{})
	if err != nil || strings.Join(top.Databases, ",") != "admin,logs,shop" {
		t.Fatalf("databases = %v, %v", top.Databases, err)
	}
	objs, err := s.Introspect(ctx, IntrospectionRequest{Database: "shop", Schema: "shop"})
	if err != nil || len(objs.Objects) != 2 || objs.Objects[0] != (SchemaObject{Name: "users", Kind: "table"}) || objs.Objects[1] != (SchemaObject{Name: "active", Kind: "view"}) {
		t.Fatalf("objects = %+v, %v", objs.Objects, err)
	}
	if c := f.last(t); c.db != "shop" {
		t.Fatalf("listCollections ran in %q", c.db)
	}
	one, err := s.Introspect(ctx, IntrospectionRequest{Database: "shop", Schema: "shop", Object: "users"})
	if err != nil {
		t.Fatal(err)
	}
	var cols []string
	for _, c := range one.Columns {
		cols = append(cols, c.Name+":"+c.Type)
	}
	if strings.Join(cols, ",") != "_id:objectId,email:string,age:int" {
		t.Fatalf("columns = %v", cols)
	}
	if len(one.Indexes) != 2 || one.Indexes[1].Name != "email_1" || !one.Indexes[1].Unique || strings.Join(one.Indexes[1].Columns, ",") != "email" {
		t.Fatalf("indexes = %+v", one.Indexes)
	}
	if !strings.Contains(one.DDL, `db.getCollection("users").createIndex({"email": 1}, {"name": "email_1", "unique": true})`) || strings.Contains(one.DDL, `"_id_"`) {
		t.Fatalf("DDL = %s", one.DDL)
	}
}

// Classify judges a shell line, so reads run at once and writes ask first.
func TestClassifyMongoShell(t *testing.T) {
	cases := map[string]Kind{
		`db.u.find({})`:                 KindRead,
		"-- @conn m\ndb.u.find()":       KindRead,
		`use logs`:                      KindRead,
		`show collections`:              KindRead,
		`db.u.insertOne({a: 1})`:        KindWrite,
		`db.u.aggregate([{$merge: 1}])`: KindWrite,
		`db.u.drop()`:                   KindDDL,
		`db.u.find(`:                    KindWrite,
	}
	for src, want := range cases {
		if got, _ := Classify(src, "mongodb"); got != want {
			t.Errorf("%q: got %v, want %v", src, got, want)
		}
	}
}

func TestParseURLMongo(t *testing.T) {
	c, err := ParseURL("mongodb://app:s3cret@db.local:27018/shop?authSource=admin")
	if err != nil {
		t.Fatal(err)
	}
	if c.Driver != "mongodb" || c.Host != "db.local" || c.Port != 27018 || c.User != "app" || c.Password != "s3cret" || c.Database != "shop" {
		t.Fatalf("conn = %+v", c)
	}
	if strings.Contains(c.URL, "s3cret") {
		t.Fatal("the password stayed in the saved URL")
	}
	srv, err := ParseURL("mongodb+srv://app@cluster0.example.net/shop")
	if err != nil || srv.Driver != "mongodb" || srv.Host != "cluster0.example.net" || srv.Port != 0 {
		t.Fatalf("srv = %+v, %v", srv, err)
	}
	if err := Validate(Conn{ID: "m1", Driver: "mongodb", Host: "db.local", Port: 27017}); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if err := Validate(Conn{ID: "m2", Driver: "mongodb", URL: "mongodb+srv://cluster0.example.net/shop"}); err != nil {
		t.Fatalf("validate srv: %v", err)
	}
	if err := Validate(Conn{ID: "m3", Driver: "mongodb"}); err == nil {
		t.Fatal("a MongoDB connection with no host passed")
	}
}

// The URL the driver gets carries the password, but the session never
// shows it: errors are scrubbed.
func TestMongoURIAndScrub(t *testing.T) {
	uri, err := mongoURI(Conn{Driver: "mongodb", Host: "db.local", Port: 27017, User: "app", Database: "shop"}, "p@ss/word")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(uri, "mongodb://app:p%40ss%2Fword@db.local:27017/shop") {
		t.Fatalf("uri = %s", uri)
	}
	e := mongoScrub(errors.New("auth failed for mongodb://app:p%40ss%2Fword@db.local p@ss/word"), "p@ss/word")
	if strings.Contains(e.Error(), "p@ss") || strings.Contains(e.Error(), "p%40ss") {
		t.Fatalf("not scrubbed: %v", e)
	}
	uri, err = mongoURI(Conn{Driver: "mongodb", URL: "mongodb+srv://app@cluster0.example.net/shop?retryWrites=true"}, "pw")
	if err != nil || uri != "mongodb+srv://app:pw@cluster0.example.net/shop?retryWrites=true" {
		t.Fatalf("uri = %s, %v", uri, err)
	}
}
