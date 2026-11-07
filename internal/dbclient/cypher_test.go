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
