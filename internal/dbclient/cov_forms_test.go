package dbclient

import (
	"strings"
	"testing"
)

func TestMongoShellCommandForms(t *testing.T) {
	for src, want := range map[string]string{
		"show collections": "listCollections", "  show tables  ": "listCollections",
		`db.getCollection("my coll").find({a: 1})`: "find",
	} {
		c, err := ParseMongoShell(src)
		if err != nil || c.Method != want {
			t.Errorf("%q = %+v, %v", src, c, err)
		}
	}
	if c, _ := ParseMongoShell(`db.getCollection("my coll").find()`); c.Collection != "my coll" {
		t.Errorf("collection = %q", c.Collection)
	}
	for src, why := range map[string]string{
		"show databases":                    "show supports only collections",
		"show collections extra":            "",
		"use app":                           "starts with db.",
		"db":                                "",
		"db find()":                         "",
		"db.":                               "collection name expected",
		"db.c.":                             "method name expected",
		`db.getCollection(1).find()`:        "getCollection needs one name",
		`db.getCollection("a", "b").find()`: "getCollection needs one name",
		`db.getCollection("").find()`:       "getCollection needs one name",
		`db.getCollection("a"`:              "",
		`db.getCollection("a")`:             "",
		`db.c.find().pretty(1)`:             "takes no arguments",
		`db.c.find().toArray(1)`:            "takes no arguments",
	} {
		_, err := ParseMongoShell(src)
		if err == nil || (why != "" && !strings.Contains(err.Error(), why)) {
			t.Errorf("%q = %v, want %q", src, err, why)
		}
	}
	if _, err := ParseMongoShell("db.c.find().pretty().toArray()"); err != nil {
		t.Errorf("display modifiers are accepted: %v", err)
	}
	for args, want := range map[string]bool{
		`db.c.aggregate([{$out: "x"}])`: true, `db.c.aggregate([{$merge: {into: "x"}}])`: true,
		`db.c.aggregate([{$match: {}}])`: false, `db.c.aggregate([])`: false, `db.c.aggregate()`: false,
		`db.c.aggregate({a: 1})`: true, `db.c.aggregate([1, {$out: "x"}])`: true,
	} {
		c, err := ParseMongoShell(args)
		if err != nil {
			t.Errorf("%s: %v", args, err)
			continue
		}
		if got := aggregateWrites(c.Args); got != want {
			t.Errorf("aggregateWrites(%s) = %v, want %v", args, got, want)
		}
	}
}

func TestJoinSelectMoreShapes(t *testing.T) {
	for sql, why := range map[string]string{
		"SELECT a FROM t JOIN u USING a":            "not a plain SELECT",
		"SELECT a FROM t JOIN u USING (a":           "not a plain SELECT",
		"SELECT a FROM t JOIN u ON (a = b))":        "not a plain SELECT",
		"SELECT a FROM t, JOIN u":                   "not a plain SELECT",
		"SELECT a FROM t x y JOIN u ON 1 = 1":       "not a plain SELECT",
		"SELECT a FROM s. JOIN u ON 1 = 1":          "not a plain SELECT",
		"SELECT a FROM t AS JOIN u ON 1 = 1":        "not a plain SELECT",
		"SELECT a FROM t JOIN u. ON 1 = 1":          "not a plain SELECT",
		"SELECT a FROM t JOIN u ON 1 = 1; SELECT 1": "not a plain SELECT",
		"SELECT a FROM t INNER u ON 1 = 1":          "not a plain SELECT",
	} {
		if _, got := JoinSelect(sql, ""); got == "" || !strings.Contains(got, why) && !strings.Contains(got, "one table") {
			t.Errorf("JoinSelect(%q) = %q", sql, got)
		}
	}
	good := []string{
		"SELECT * FROM a JOIN b ON a.id = b.id WHERE a.x = 1 ORDER BY a.id LIMIT 3 OFFSET 1",
		"SELECT a.id, b.id FROM s.a AS a INNER JOIN s.b b USING (id)",
		"SELECT a.* FROM a LEFT OUTER JOIN b ON a.id = b.id FOR UPDATE",
		"SELECT ALL a.id FROM a, b WHERE a.id = b.id",
		"SELECT id FROM a CROSS JOIN b",
		"SELECT a.id FROM a JOIN b ON (a.id = (SELECT 1)) FETCH FIRST 5 ROWS ONLY",
	}
	for _, sql := range good {
		if jt, why := JoinSelect(sql, "postgres"); why != "" || len(jt.Tables) < 2 {
			t.Errorf("JoinSelect(%q) = %+v, %q", sql, jt, why)
		}
	}
	jt, _ := JoinSelect("SELECT * FROM a x JOIN a y ON 1 = 1", "")
	if jt.TableOf("a") != -1 || jt.TableOf("x") != 0 || jt.TableOf("y") != 1 {
		t.Errorf("a name that is on two aliases is ambiguous: %d %d %d", jt.TableOf("a"), jt.TableOf("x"), jt.TableOf("y"))
	}
}
