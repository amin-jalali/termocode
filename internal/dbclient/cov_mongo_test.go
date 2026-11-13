package dbclient

import (
	"math"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func TestMongoServerCommandMoreShapes(t *testing.T) {
	cases := []struct{ src, want string }{
		{`db.users.findOneAndUpdate({a: 1}, {$set: {b: 1}}, {sort: {a: 1}, projection: {b: 1}, returnNewDocument: true, upsert: true})`,
			`{"findAndModify": "users", "query": {"a": 1}, "update": {"$set": {"b": 1}}, "sort": {"a": 1}, "fields": {"b": 1}, "new": true, "upsert": true}`},
		{`db.users.findOneAndReplace({a: 1}, {a: 2}, {returnDocument: "after"})`,
			`{"findAndModify": "users", "query": {"a": 1}, "update": {"a": 2}, "new": true}`},
		{`db.users.findOneAndDelete({a: 1}, {sort: {a: -1}})`,
			`{"findAndModify": "users", "query": {"a": 1}, "remove": true, "sort": {"a": -1}}`},
		{`db.users.createIndexes([{a: 1}, {b: -1}])`,
			`{"createIndexes": "users", "indexes": [{"key": {"a": 1}, "name": "a_1"}, {"key": {"b": -1}, "name": "b_-1"}]}`},
		{`db.orders.aggregate([{$match: {}}], {allowDiskUse: true, cursor: {batchSize: 1}})`,
			`{"aggregate": "orders", "pipeline": [{"$match": {}}], "cursor": {"batchSize": 500}, "allowDiskUse": true}`},
		{`db.users.updateOne({a: 1}, [{$set: {b: 2}}], {arrayFilters: [{"x.b": 1}], hint: {a: 1}})`,
			`{"update": "users", "updates": [{"q": {"a": 1}, "u": [{"$set": {"b": 2}}], "multi": false, "upsert": false, "arrayFilters": [{"x.b": 1}], "hint": {"a": 1}}]}`},
		{`db.users.dropIndex({a: 1})`, `{"dropIndexes": "users", "index": {"a": 1}}`},
		{`db.users.dropIndexes("by_mail")`, `{"dropIndexes": "users", "index": "by_mail"}`},
		{`db.users.count()`, `{"count": "users"}`},
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

func TestMongoServerCommandMoreRejects(t *testing.T) {
	for src, why := range map[string]string{
		`db.users.find({}, {}, 1)`:                     "at most 2",
		`db.users.findOne({}, {}, 1)`:                  "at most 2",
		`db.users.findOne(1)`:                          "filter",
		`db.users.aggregate()`:                         "needs a pipeline",
		`db.users.aggregate({a: 1})`:                   "must be an array",
		`db.users.aggregate([], 1)`:                    "options",
		`db.users.count({}, 1)`:                        "at most 1",
		`db.users.count(1)`:                            "filter",
		`db.users.estimatedDocumentCount(1)`:           "at most 0",
		`db.users.distinct()`:                          "field name",
		`db.users.distinct(1)`:                         "field name",
		`db.users.distinct("a", 1)`:                    "filter",
		`db.users.getIndexes(1)`:                       "at most 0",
		`db.users.insertMany()`:                        "array of documents",
		`db.users.insertMany([])`:                      "array of documents",
		`db.users.insertMany(1)`:                       "array of documents",
		`db.users.insertMany([1])`:                     "documents only",
		`db.users.updateOne({a: 1}, 5)`:                "must be a document",
		`db.users.replaceOne({a: 1}, [{a: 1}])`:        "needs a document",
		`db.users.updateOne({a: 1}, {$set: {}}, 5)`:    "options",
		`db.users.updateOne(1, {})`:                    "filter",
		`db.users.deleteOne(1)`:                        "filter",
		`db.users.deleteOne({}, {}, 1)`:                "at most 2",
		`db.users.findOneAndDelete(1)`:                 "filter",
		`db.users.findOneAndDelete({}, {}, 1)`:         "at most 2",
		`db.users.findOneAndUpdate({a: 1})`:            "needs a filter and an update",
		`db.users.findOneAndUpdate({a: 1}, 5)`:         "must be a document",
		`db.users.findOneAndUpdate({a: 1}, {}, {}, 1)`: "at most 3",
		`db.users.findOneAndUpdate({a: 1}, {}, 5)`:     "options",
		`db.users.createIndex()`:                       "key",
		`db.users.createIndex({})`:                     "at least one field",
		`db.users.createIndex({a: 1}, {}, 1)`:          "at most 2",
		`db.users.createIndexes()`:                     "array of keys",
		`db.users.createIndexes([])`:                   "array of keys",
		`db.users.createIndexes([1])`:                  "key documents only",
		`db.users.dropIndex(5)`:                        "index name or key",
		`db.users.dropIndex("a", "b")`:                 "index name or key",
		`db.users.dropIndexes("a", "b")`:               "at most 1",
		`db.users.drop(1)`:                             "at most 0",
		`db.users.insertOne({a: 1}).sort({a: 1})`:      "work only after find",
	} {
		_, err := MongoServerCommand(mustParse(t, src), 500)
		if err == nil {
			if why != "" {
				t.Errorf("%s: no error", src)
			}
			continue
		}
		if !strings.Contains(err.Error(), why) {
			t.Errorf("%s: %v, want %q", src, err, why)
		}
	}
}

func TestMongoBSONRoundTrip(t *testing.T) {
	id := bson.NewObjectID()
	when := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)
	in := MongoDoc{
		{Key: "i", Value: float64(3)}, {Key: "big", Value: float64(1 << 40)}, {Key: "frac", Value: 1.5},
		{Key: "negzero", Value: math.Copysign(0, -1)},
		{Key: "id", Value: MongoObjectID(id.Hex())}, {Key: "badid", Value: MongoObjectID("zz")},
		{Key: "when", Value: when}, {Key: "arr", Value: []any{float64(1), MongoDoc{{Key: "k", Value: "v"}}}},
		{Key: "s", Value: "text"}, {Key: "n", Value: nil}, {Key: "b", Value: true},
	}
	b := mongoToBSON(in).(bson.D)
	if v, ok := b[0].Value.(int32); !ok || v != 3 {
		t.Errorf("a whole number is an int32, got %#v", b[0].Value)
	}
	if _, ok := b[1].Value.(float64); !ok {
		t.Errorf("a number past int32 stays a double, got %#v", b[1].Value)
	}
	if _, ok := b[3].Value.(float64); !ok {
		t.Errorf("negative zero stays a double, got %#v", b[3].Value)
	}
	if _, ok := b[4].Value.(bson.ObjectID); !ok {
		t.Errorf("an object id is an ObjectID, got %#v", b[4].Value)
	}
	if s, ok := b[5].Value.(string); !ok || s != "zz" {
		t.Errorf("a bad object id stays text, got %#v", b[5].Value)
	}
	if _, ok := b[6].Value.(bson.DateTime); !ok {
		t.Errorf("a time is a DateTime, got %#v", b[6].Value)
	}
	out, ok := mongoFromBSON(b).(MongoDoc)
	if !ok || len(out) != len(in) {
		t.Fatalf("round trip = %#v", out)
	}
	if v, _ := out.Get("id"); v != MongoObjectID(id.Hex()) {
		t.Errorf("id = %#v", v)
	}
	if v, _ := out.Get("when"); !v.(time.Time).Equal(when) {
		t.Errorf("when = %#v", v)
	}
}

func TestMongoFromBSONKinds(t *testing.T) {
	dec, _ := bson.ParseDecimal128("1.5")
	raw, _ := bson.Marshal(bson.D{{Key: "x", Value: int32(1)}})
	for name, c := range map[string]struct {
		in   any
		want string
	}{
		"map":       {bson.M{"b": int32(1), "a": int32(2)}, `{"a": 2, "b": 1}`},
		"raw":       {bson.Raw(raw), `{"x": 1}`},
		"bad raw":   {bson.Raw([]byte{1, 2}), ""},
		"anys":      {[]any{int64(1), "s"}, `[1, "s"]`},
		"decimal":   {dec, `"1.5"`},
		"binary":    {bson.Binary{Data: []byte("ab")}, ""},
		"timestamp": {bson.Timestamp{T: 5, I: 2}, `"Timestamp(5, 2)"`},
		"regex":     {bson.Regex{Pattern: "^a", Options: "i"}, `"/^a/i"`},
		"other":     {struct{ A int }{4}, ""},
	} {
		got := mongoFromBSON(c.in)
		if got == nil {
			t.Errorf("%s gave nil", name)
			continue
		}
		if c.want != "" {
			if s := MongoShellJSON(got); s != c.want {
				t.Errorf("%s = %s, want %s", name, s, c.want)
			}
		}
	}
	if mongoFromBSON(nil) != nil || mongoFromBSON(int32(2)) != int32(2) {
		t.Error("plain values stay as they are")
	}
}

func TestMongoIntAndScalarHelpers(t *testing.T) {
	for _, c := range []struct {
		in   any
		want int64
		ok   bool
	}{{int32(2), 2, true}, {int64(3), 3, true}, {4, 4, true}, {5.0, 5, true}, {5.5, 5, false}, {"x", 0, false}} {
		if got, ok := mongoInt(c.in); got != c.want || ok != c.ok {
			t.Errorf("mongoInt(%v) = %d, %v", c.in, got, ok)
		}
	}
	if got, ok := firstArray([]any{"x", []any{1}}); ok || got != nil {
		t.Errorf("firstArray of a non-array first item = %v, %v", got, ok)
	}
	if got, ok := firstArray([]any{[]any{1}}); !ok || len(got) != 1 {
		t.Errorf("firstArray = %v, %v", got, ok)
	}
	if _, ok := firstArray(nil); ok {
		t.Error("firstArray of nothing")
	}
}
