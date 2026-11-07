package dbclient

import (
	"strings"
	"testing"
)

func TestSQLServerDialectSyntax(t *testing.T) {
	d := SQLServerDialect{}
	if got := d.QuoteIdent("we]ird name"); got != "[we]]ird name]" {
		t.Errorf("QuoteIdent = %q", got)
	}
	if got := d.Placeholder(2); got != "@p2" {
		t.Errorf("Placeholder = %q", got)
	}
	if got := d.Limit(50, 100); got != " OFFSET 100 ROWS FETCH NEXT 50 ROWS ONLY" {
		t.Errorf("Limit = %q", got)
	}
	if d.Limit(0, 10) != "" {
		t.Error("Limit(0) not empty")
	}
	// Explain never returns the statement itself: SQL Server has no
	// EXPLAIN prefix, and running the bare statement would change data.
	if got := d.Explain("DELETE FROM t", false); strings.Contains(got, "DELETE") {
		t.Errorf("Explain runs the statement: %q", got)
	}
	ddl, args := d.DDL("orders")
	if !strings.Contains(ddl, "@p1") || len(args) != 1 || args[0] != "orders" {
		t.Errorf("DDL binds the name: %q %v", ddl, args)
	}
}

func TestSQLServerCatalogQueriesUseItsPlaceholders(t *testing.T) {
	q := SQLServerDialect{}.CatalogQueries()
	for _, k := range []string{"databases", "schemas", "objects", "columns", "keys", "indexes", "foreign_keys", "routines", "sequences"} {
		s, ok := q[k]
		if !ok || s == "" {
			t.Errorf("no %s query", k)
			continue
		}
		if strings.Contains(s, "?") || strings.Contains(s, "$1") || strings.Contains(s, "LIMIT") {
			t.Errorf("%s uses another dialect's syntax: %s", k, s)
		}
	}
	for _, k := range []string{"columns", "keys", "indexes", "foreign_keys"} {
		if !strings.Contains(q[k], "@p1") || !strings.Contains(q[k], "@p2") {
			t.Errorf("%s does not bind schema and table: %s", k, q[k])
		}
	}
}

func TestSQLServerShowplanBatches(t *testing.T) {
	got := SQLServerShowplanBatches("SELECT 1;", false)
	if len(got) != 3 || got[0] != "SET SHOWPLAN_XML ON" || got[1] != "SELECT 1" || got[2] != "SET SHOWPLAN_XML OFF" {
		t.Errorf("plan batches = %q", got)
	}
	got = SQLServerShowplanBatches("SELECT 1", true)
	if got[0] != "SET STATISTICS XML ON" || got[2] != "SET STATISTICS XML OFF" {
		t.Errorf("analyze batches = %q", got)
	}
}

const showplanXML = `<?xml version="1.0" encoding="utf-16"?>
<ShowPlanXML xmlns="http://schemas.microsoft.com/sqlserver/2004/07/showplan" Version="1.564">
 <BatchSequence><Batch><Statements>
  <StmtSimple StatementText="SELECT * FROM o JOIN c ON o.cid=c.id" StatementSubTreeCost="10">
   <QueryPlan>
    <RelOp NodeId="0" PhysicalOp="Hash Match" LogicalOp="Inner Join" EstimateRows="120" EstimatedTotalSubtreeCost="10">
     <Hash>
      <RelOp NodeId="1" PhysicalOp="Clustered Index Scan" LogicalOp="Clustered Index Scan" EstimateRows="1000" EstimatedTotalSubtreeCost="7.5">
       <RunTimeInformation><RunTimeCountersPerThread Thread="0" ActualRows="998" ActualElapsedms="12"/></RunTimeInformation>
       <IndexScan><Object Database="[shop]" Schema="[dbo]" Table="[o]" Index="[PK_o]"/></IndexScan>
      </RelOp>
      <RelOp NodeId="2" PhysicalOp="Index Seek" LogicalOp="Index Seek" EstimateRows="10" EstimatedTotalSubtreeCost="0.5">
       <IndexScan><Object Schema="[dbo]" Table="[c]" Index="[IX_c]"/></IndexScan>
      </RelOp>
     </Hash>
    </RelOp>
   </QueryPlan>
  </StmtSimple>
 </Statements></Batch></BatchSequence>
</ShowPlanXML>`

func TestParseSQLServerShowplanXML(t *testing.T) {
	res := Result{Columns: []Column{{Name: "Microsoft SQL Server 2005 XML Showplan"}}, Rows: [][]Cell{{{Text: showplanXML}}}}
	p := ParsePlan("sqlserver", res, false)
	var got []string
	for _, l := range p.Lines {
		got = append(got, strings.Repeat("  ", l.Depth)+l.Text)
	}
	want := []string{
		"Hash Match (Inner Join)  cost=10 rows=120",
		"  Clustered Index Scan on [dbo].[o] using [PK_o]  cost=7.5 rows=1000 | actual 12 ms rows=998",
		"  Index Seek on [dbo].[c] using [IX_c]  cost=0.5 rows=10",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lines:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if !p.Lines[1].Costly || p.Lines[0].Costly || p.Lines[2].Costly {
		t.Errorf("costly flags: %+v", p.Lines)
	}
	if len(p.Summary) == 0 || !strings.Contains(p.Summary[0], "SELECT * FROM o") {
		t.Errorf("summary = %v", p.Summary)
	}
	// Not a showplan: shown as the server's text.
	txt := ParsePlan("sqlserver", Result{Rows: [][]Cell{{{Text: "StmtText\n  |--Scan"}}}}, false)
	if len(txt.Lines) == 0 || txt.Raw == "" {
		t.Errorf("text fallback: %+v", txt)
	}
}
