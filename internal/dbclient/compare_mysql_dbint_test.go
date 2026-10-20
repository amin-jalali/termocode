//go:build dbint

package dbclient

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// A live MySQL or MariaDB (TERMOCODE_DBINT_MYSQL_URL, a root URL): two
// databases compared on read-only sessions; the script (run here by the
// test, never by the feature) makes the target match the source.
func TestMySQLCompareSchemasLive(t *testing.T) {
	url := os.Getenv("TERMOCODE_DBINT_MYSQL_URL")
	if url == "" {
		t.Skip("TERMOCODE_DBINT_MYSQL_URL is not set")
	}
	conn, err := ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	s, err := NewSQLSession(ctx, conn, conn.Password)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	run := func(q string) {
		t.Helper()
		if r := s.Run(ctx, q, RunOptions{}); r.Err != nil {
			t.Fatalf("%s: %v", q, r.Err)
		}
	}
	for _, q := range []string{
		"DROP DATABASE IF EXISTS cmp_src", "DROP DATABASE IF EXISTS cmp_tgt",
		"CREATE DATABASE cmp_src", "CREATE DATABASE cmp_tgt",
		"CREATE TABLE cmp_src.users (id int AUTO_INCREMENT PRIMARY KEY, email varchar(200) NOT NULL UNIQUE, plan varchar(20) NOT NULL DEFAULT 'free', created_at timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP, flag tinyint(1) NOT NULL DEFAULT 0)",
		"CREATE TABLE cmp_src.orders (id int PRIMARY KEY, user_id int, note varchar(100), CONSTRAINT orders_user_fk FOREIGN KEY (user_id) REFERENCES cmp_src.users(id) ON DELETE CASCADE)",
		"CREATE INDEX orders_note ON cmp_src.orders (note(10))",
		"CREATE VIEW cmp_src.paying AS SELECT id, email FROM cmp_src.users WHERE plan <> 'free'",
		"CREATE TABLE cmp_tgt.users (id int AUTO_INCREMENT PRIMARY KEY, email varchar(200) NOT NULL UNIQUE, created_at timestamp NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP, flag tinyint(1) NOT NULL DEFAULT 0, age int)",
		"CREATE TABLE cmp_tgt.legacy (token text)",
		"CREATE INDEX users_age ON cmp_tgt.users (age)",
		"CREATE VIEW cmp_tgt.paying AS SELECT id FROM cmp_tgt.users",
	} {
		run(q)
	}
	defer s.Run(context.Background(), "DROP DATABASE IF EXISTS cmp_src", RunOptions{})
	defer s.Run(context.Background(), "DROP DATABASE IF EXISTS cmp_tgt", RunOptions{})
	read := func(schema string) CanonSchema {
		t.Helper()
		ro, err := s.ReadOnlySession()
		if err != nil {
			t.Fatal(err)
		}
		defer ro.Close()
		out, err := ReadCanonSchema(ctx, ro, "mysql", schema)
		if err != nil {
			t.Fatalf("%s: %v", schema, err)
		}
		return out
	}
	if _, schemas, err := ListSchemas(ctx, s, "mysql"); err != nil || !strings.Contains(strings.Join(schemas, ","), "cmp_tgt") || strings.Contains(strings.Join(schemas, ","), "performance_schema") {
		t.Fatalf("schemas %v %v", schemas, err)
	}
	src, tgt := read("cmp_src"), read("cmp_tgt")
	all := ""
	for _, f := range CanonFiles(src) {
		all += f.Text
	}
	for _, want := range []string{"`id` int NOT NULL AUTO_INCREMENT", "`plan` varchar(20) NOT NULL DEFAULT 'free'", "`flag` tinyint(1) NOT NULL DEFAULT 0",
		"ON UPDATE CURRENT_TIMESTAMP", "  PRIMARY KEY (`id`)", "UNIQUE (`email`)", "CONSTRAINT `orders_user_fk` FOREIGN KEY (`user_id`) REFERENCES `users` (`id`) ON DELETE CASCADE",
		"CREATE INDEX `orders_note` ON `orders` (`note`(10));", "CREATE VIEW `paying` AS"} {
		if !strings.Contains(all, want) {
			t.Errorf("canonical DDL lacks %q:\n%s", want, all)
		}
	}
	if strings.Contains(all, "cmp_src") {
		t.Errorf("database name in canonical DDL:\n%s", all)
	}
	cmp := CompareSchemas(src, tgt)
	if cmp.Destructive != 4 {
		t.Errorf("destructive = %d:\n%s", cmp.Destructive, cmp.Script)
	}
	for _, stmt := range strings.Split(cmp.Script, ";\n") {
		var lines []string
		for _, l := range strings.Split(stmt, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(l), "--") {
				lines = append(lines, l)
			}
		}
		if q := strings.TrimSpace(strings.Join(lines, "\n")); q != "" {
			run(q)
		}
	}
	after := CompareSchemas(src, read("cmp_tgt"))
	if after.Changes != 0 {
		t.Fatalf("after the script, %d changes:\n%s", after.Changes, after.Script)
	}
}
