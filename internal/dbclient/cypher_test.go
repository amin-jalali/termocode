package dbclient

import "testing"

func TestClassifyCypherReadsAndWrites(t *testing.T) {
	reads := []string{
		"MATCH (n:Person) RETURN n LIMIT 10",
		"match (a)-[r:KNOWS]->(b) where a.name = 'Ann' return a, r, b",
		"OPTIONAL MATCH (n) RETURN count(n)",
		"RETURN 1",
		"WITH 1 AS x UNWIND [1,2] AS y RETURN x, y",
		"EXPLAIN MATCH (n) RETURN n",
		"SHOW DATABASES",
		"CALL db.labels()",
		"CALL db.relationshipTypes() YIELD relationshipType RETURN relationshipType",
		"MATCH (n) WHERE n.note = 'CREATE (x)' RETURN n",
		"MATCH (n) // DELETE n\nRETURN n",
		"MATCH (n) /* SET n.x = 1 */ RETURN n.`set`",
		"MATCH (n) RETURN n.created, n.settled",
	}
	for _, q := range reads {
		if got := ClassifyCypher(q); got != KindRead {
			t.Errorf("ClassifyCypher(%q) = %s, want read", q, got)
		}
	}
	writes := []string{
		"CREATE (n:Person {name: 'Ann'})",
		"MATCH (n) DETACH DELETE n",
		"MATCH (n) SET n.age = 3",
		"MATCH (n) REMOVE n:Person",
		"MERGE (n:City {name: 'Rome'})",
		"MATCH (n) RETURN n; CREATE (m)",
		"LOAD CSV FROM 'file:///x.csv' AS row RETURN row",
		"MATCH (n) FOREACH (x IN [1] | SET n.v = x)",
		"CALL apoc.periodic.iterate('a', 'b', {})",
		"CALL db.createLabel('X')",
		"PROFILE MATCH (n) DELETE n",
		"DROP INDEX my_index",
		"CREATE INDEX FOR (n:Person) ON (n.name)",
		"STOP DATABASE neo4j",
		"MATCH (n) RETURN n /* unterminated",
		"MATCH (n) WHERE n.x = 'open RETURN n",
		"FROBNICATE (n)",
	}
	for _, q := range writes {
		if got := ClassifyCypher(q); got != KindWrite {
			t.Errorf("ClassifyCypher(%q) = %s, want write", q, got)
		}
	}
	if got := ClassifyCypher("  // only a comment\n"); got != KindUnknown {
		t.Errorf("empty query = %s, want unknown", got)
	}
}

func TestGraphTextDrawsNodesAndRelationships(t *testing.T) {
	nodes := []GraphNode{
		{ID: "2", Labels: []string{"Person"}, Props: map[string]string{"name": "Bob"}},
		{ID: "1", Labels: []string{"Person", "Admin"}, Props: map[string]string{"name": "Ann", "age": "41"}},
		{ID: "3", Labels: []string{"City"}, Props: map[string]string{"name": "Rome"}},
		{ID: "4"},
	}
	rels := []GraphRel{
		{From: "1", To: "3", Type: "LIVES_IN"},
		{From: "1", To: "2", Type: "KNOWS", Props: map[string]string{"since": "2020"}},
		{From: "9", To: "2", Type: "LIKES"},
	}
	want := "(1:Person:Admin {age: 41, name: Ann})\n" +
		"(2:Person {name: Bob})\n" +
		"(3:City {name: Rome})\n" +
		"(4)\n" +
		"\n" +
		"(1)-[:KNOWS {since: 2020}]->(2)\n" +
		"(1)-[:LIVES_IN]->(3)\n" +
		"(9)-[:LIKES]->(2)\n"
	if got := GraphText(nodes, rels); got != want {
		t.Errorf("GraphText =\n%s\nwant\n%s", got, want)
	}
	if got := GraphText(nil, nil); got != "(no nodes)\n" {
		t.Errorf("empty graph = %q", got)
	}
	evil := []GraphNode{{ID: "1", Props: map[string]string{"x": "a\nb\x1b[31m"}}}
	if got := GraphText(evil, nil); got != "(1 {x: a\\nb\\x1b[31m})\n" {
		t.Errorf("control characters not escaped: %q", got)
	}
}
