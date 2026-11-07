package dbclient

import (
	"reflect"
	"testing"
)

func TestParseMongoShellFindWithModifiers(t *testing.T) {
	cmd, err := ParseMongoShell(`db.users.find({age: {$gt: 30}, 'name': "a;b"}, {name: 1}).sort({age: -1}).skip(5).limit(10)`)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Collection != "users" || cmd.Method != "find" {
		t.Fatalf("got %q.%q", cmd.Collection, cmd.Method)
	}
	want := MongoDoc{
		{Key: "age", Value: MongoDoc{{Key: "$gt", Value: float64(30)}}},
		{Key: "name", Value: "a;b"},
	}
	if len(cmd.Args) != 2 || !reflect.DeepEqual(cmd.Args[0], want) {
		t.Fatalf("filter = %#v", cmd.Args)
	}
	if !reflect.DeepEqual(cmd.Sort, MongoDoc{{Key: "age", Value: float64(-1)}}) {
		t.Fatalf("sort = %#v", cmd.Sort)
	}
	if cmd.Skip != 5 || cmd.Limit != 10 || cmd.Explain {
		t.Fatalf("skip=%d limit=%d explain=%v", cmd.Skip, cmd.Limit, cmd.Explain)
	}
	if MongoKind(cmd) != KindRead {
		t.Fatalf("kind = %s", MongoKind(cmd))
	}
}

func TestParseMongoShellKeepsKeyOrder(t *testing.T) {
	cmd, err := ParseMongoShell(`db.t.find({}).sort({b: 1, a: 1, c: -1})`)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, f := range cmd.Sort {
		keys = append(keys, f.Key)
	}
	if !reflect.DeepEqual(keys, []string{"b", "a", "c"}) {
		t.Fatalf("keys = %v", keys)
	}
}

func TestParseMongoShellGetCollectionAndExplain(t *testing.T) {
	cmd, err := ParseMongoShell(`db.getCollection("my-coll").aggregate([{$match: {x: true}}, {$group: {_id: "$k", n: {$sum: 1}}}]).explain()`)
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Collection != "my-coll" || cmd.Method != "aggregate" || !cmd.Explain {
		t.Fatalf("got %+v", cmd)
	}
	pipe, ok := cmd.Args[0].([]any)
	if !ok || len(pipe) != 2 {
		t.Fatalf("pipeline = %#v", cmd.Args[0])
	}
	if MongoKind(cmd) != KindRead {
		t.Fatalf("kind = %s", MongoKind(cmd))
	}
}

func TestMongoKindWritesAndDDL(t *testing.T) {
	cases := map[string]Kind{
		`db.a.insertOne({x: 1})`:                         KindWrite,
		`db.a.updateMany({}, {$set: {x: 1}})`:            KindWrite,
		`db.a.deleteOne({_id: 1})`:                       KindWrite,
		`db.a.aggregate([{$match: {}}, {$out: "b"}])`:    KindWrite,
		`db.a.aggregate([{$merge: {into: "b"}}])`:        KindWrite,
		`db.a.createIndex({x: 1})`:                       KindDDL,
		`db.a.drop()`:                                    KindDDL,
		`db.a.countDocuments({})`:                        KindRead,
		`db.a.distinct("x")`:                             KindRead,
		`db.a.getIndexes()`:                              KindRead,
		`db.a.somethingNew()`:                            KindWrite,
		`show collections`:                               KindRead,
		`db.a.find({}, null).limit(3)`:                   KindRead,
		`db.a.find({re: "x"}).explain("executionStats")`: KindRead,
	}
	for src, want := range cases {
		cmd, err := ParseMongoShell(src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		if got := MongoKind(cmd); got != want {
			t.Errorf("%s: kind = %s, want %s", src, got, want)
		}
	}
}

func TestParseMongoShellShowCollections(t *testing.T) {
	cmd, err := ParseMongoShell("  show collections ; ")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Method != "listCollections" || cmd.Collection != "" {
		t.Fatalf("got %+v", cmd)
	}
}

func TestParseMongoShellRejectsBadInput(t *testing.T) {
	for _, src := range []string{
		``,
		`users.find({})`,
		`db.users.find({a: 1}`,
		`db.users.find({a: })`,
		`db.users.find({}).limit("x")`,
		`db.users.find({}).bogus(1)`,
		`db.users.find({}) extra`,
		`db.users.find({a: 1}); db.users.drop()`,
		`db..find()`,
	} {
		if _, err := ParseMongoShell(src); err == nil {
			t.Errorf("%q: want error", src)
		}
	}
}

func FuzzParseMongoShell(f *testing.F) {
	f.Add(`db.users.find({a: [1, "x", {b: null}]}).limit(2)`)
	f.Add(`db.getCollection('x').aggregate([{$out: 'y'}])`)
	f.Fuzz(func(t *testing.T, src string) {
		cmd, err := ParseMongoShell(src)
		if err == nil {
			_ = MongoKind(cmd)
		}
	})
}
