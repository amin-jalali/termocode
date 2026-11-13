package dbclient

import (
	"strings"
	"testing"
)

func TestParseMongoShellStringsAndNumbers(t *testing.T) {
	c, err := ParseMongoShell(`db.c.insertOne({"a\n\t\rA\q": 'it\'s', b: -1.5e2, c: .5, "d.e": [1, 2,], f: {}})`)
	if err != nil {
		t.Fatal(err)
	}
	doc := c.Args[0].(MongoDoc)
	if k := doc[0].Key; k != "a\n\t\rAq" {
		t.Errorf("key = %q", k)
	}
	if v := doc[0].Value; v != "it's" {
		t.Errorf("value = %q", v)
	}
	if doc[1].Value != -150.0 || doc[2].Value != 0.5 {
		t.Errorf("numbers = %v %v", doc[1].Value, doc[2].Value)
	}
	for src, why := range map[string]string{
		`db.c.find({a: "unfinished`:        "unfinished string",
		`db.c.find({a: "bad \`:             "unfinished",
		`db.c.find({a: "\u12"})`:           "bad \\u escape",
		`db.c.find({a: "\uZZZZ"})`:         "bad \\u escape",
		`db.c.find({a: 1.2.3})`:            "bad number",
		`db.c.find({a: [1 2]})`:            "',' or ']' expected",
		`db.c.find({a: 1 b: 2})`:           "",
		`db.c.find({a: })`:                 "",
		`db.c.find({: 1})`:                 "",
		`db.c.find(`:                       "",
		`db.c.find({a: 1}`:                 "",
		`db.c.find({a: 1}).`:               "",
		`db.c.find().bogus()`:              "unknown cursor method",
		`db.c.find().sort(1)`:              "sort needs one document",
		`db.c.find().skip("x")`:            "needs one whole number",
		`db.c.find().limit(1.5)`:           "needs one whole number",
		`db.c.find().explain(1, 2)`:        "at most one mode",
		`db.c.find().explain(5)`:           "",
		`db.c.find()extra`:                 "",
		`db.c.find(ObjectId("zz"))`:        "",
		`db.c.find(ISODate("not a date"))`: "ISODate needs",
		`db.c.find(ISODate())`:             "",
		`db.c.find(Unknown(1))`:            "",
	} {
		_, err := ParseMongoShell(src)
		if err == nil {
			t.Errorf("%q: want an error", src)
			continue
		}
		if why != "" && !strings.Contains(err.Error(), why) {
			t.Errorf("%q: %v, want %q", src, err, why)
		}
	}
	// Nesting past the limit.
	deep := "db.c.find(" + strings.Repeat("{a:", mongoMaxDepth+2) + "1" + strings.Repeat("}", mongoMaxDepth+2) + ")"
	if _, err := ParseMongoShell(deep); err == nil || !strings.Contains(err.Error(), "nested too deep") {
		t.Errorf("deep nesting = %v", err)
	}
	deepArr := "db.c.find(" + strings.Repeat("[", mongoMaxDepth+2) + strings.Repeat("]", mongoMaxDepth+2) + ")"
	if _, err := ParseMongoShell(deepArr); err == nil {
		t.Error("deep arrays: want error")
	}
	c, err = ParseMongoShell(`db.c.find().explain("allPlansExecution").sort({a: 1}).skip(2).limit(3)`)
	if err != nil || !c.Explain || c.Skip != 2 || c.Limit != 3 || len(c.Sort) != 1 {
		t.Errorf("modifiers = %+v, %v", c, err)
	}
	if _, err := ParseMongoShell("  "); err == nil {
		t.Error("an empty line: want error")
	}
}
