package dbclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Neo4jSession is a Cypher console on Neo4j (DBX-71) over its HTTP API
// (POST /db/<database>/tx/commit), port 7474 (7473 with TLS), basic
// auth, net/http only. Rows show as a grid; nodes and relationships in
// the answer are also drawn as text under it (GraphText). The console
// guard uses ClassifyCypher; the read-only twin sends "access-mode: READ"
// so the server refuses writes, and refuses non-reads itself first.
// The tree lists the databases, their labels (":Person") and relationship
// types ("[:KNOWS]"), and the property keys of a label or type.
type Neo4jSession struct {
	*httpEngine
	db       string
	readOnly bool
}

func init() {
	registerHTTPEngine(HTTPEngine{
		ID: "neo4j", Title: "Neo4j", Port: 7474, TLSPort: 7473, Database: "neo4j",
		Hint:     "a Cypher console over HTTP, default port 7474",
		Schemes:  []string{"neo4j", "neo4j+s", "neo4js", "bolt", "bolt+s"},
		Classify: ClassifyCypher,
		Open: func(c Conn, password string) (Session, error) {
			return NewNeo4jSession(c, password)
		},
		FixURL: func(c *Conn) {
			// A bolt port (7687) from a driver URL: the HTTP API is on
			// 7474 (7473 with TLS).
			if c.Port == 7687 {
				c.Port = 7474
				if c.TLS {
					c.Port = 7473
				}
			}
		},
	})
}

// NewNeo4jSession makes a session of c; it does not connect.
func NewNeo4jSession(c Conn, password string) (*Neo4jSession, error) {
	e, err := newHTTPEngine("Neo4j", c, password)
	if err != nil {
		return nil, err
	}
	db := c.Database
	if db == "" {
		db = "neo4j"
	}
	return &Neo4jSession{httpEngine: e, db: db}, nil
}

type neo4jStatement struct {
	Statement   string         `json:"statement"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	ResultData  []string       `json:"resultDataContents"`
	IncludeStat bool           `json:"includeStats"`
}

type neo4jResponse struct {
	Results []struct {
		Columns []string `json:"columns"`
		Data    []struct {
			Row   []json.RawMessage `json:"row"`
			Graph struct {
				Nodes []struct {
					ID         string                     `json:"id"`
					Labels     []string                   `json:"labels"`
					Properties map[string]json.RawMessage `json:"properties"`
				} `json:"nodes"`
				Relationships []struct {
					ID         string                     `json:"id"`
					Type       string                     `json:"type"`
					StartNode  string                     `json:"startNode"`
					EndNode    string                     `json:"endNode"`
					Properties map[string]json.RawMessage `json:"properties"`
				} `json:"relationships"`
			} `json:"graph"`
		} `json:"data"`
		Stats map[string]json.RawMessage `json:"stats"`
	} `json:"results"`
	Errors []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

// commit runs one statement in its own transaction on db.
func (s *Neo4jSession) commit(ctx context.Context, db, stmt string, params map[string]any) (neo4jResponse, error) {
	var out neo4jResponse
	body, _ := json.Marshal(map[string]any{"statements": []neo4jStatement{{Statement: stmt, Parameters: params, ResultData: []string{"row", "graph"}, IncludeStat: true}}})
	data, err := s.do(ctx, "POST", "/db/"+url.PathEscape(db)+"/tx/commit", body, "")
	if len(data) > 0 && json.Unmarshal(data, &out) == nil && len(out.Errors) > 0 {
		return out, fmt.Errorf("Neo4j %s: %s", out.Errors[0].Code, s.scrub(out.Errors[0].Message))
	}
	if err != nil {
		return out, err
	}
	return out, nil
}

func (s *Neo4jSession) Ping(ctx context.Context) error {
	_, err := s.commit(ctx, s.db, "RETURN 1", nil)
	return err
}

// neo4jParams binds RunOptions.Args to $p1, $p2, ….
func neo4jParams(args []any) map[string]any {
	if len(args) == 0 {
		return nil
	}
	p := make(map[string]any, len(args))
	for i, a := range args {
		p[fmt.Sprintf("p%d", i+1)] = a
	}
	return p
}

func (s *Neo4jSession) Run(ctx context.Context, text string, opts RunOptions) Result {
	start := time.Now()
	q := strings.TrimSuffix(strings.TrimSpace(stripLineComments(text)), ";")
	if q == "" {
		return Result{Err: errors.New("neo4j: write a Cypher query")}
	}
	if s.readOnly && ClassifyCypher(q) != KindRead {
		return Result{Err: errors.New("neo4j: the session is read-only")}
	}
	resp, err := s.commit(ctx, s.db, q, neo4jParams(opts.Args))
	if err != nil {
		return Result{Err: err, Elapsed: time.Since(start)}
	}
	res := Neo4jResult(resp, opts.MaxRows)
	res.Elapsed = time.Since(start)
	return res
}

// Neo4jResult is the grid of the first result, the graph as text and the
// change counters as notes.
func Neo4jResult(resp neo4jResponse, maxRows int) Result {
	var res Result
	if len(resp.Results) == 0 {
		return res
	}
	r := resp.Results[0]
	for _, c := range r.Columns {
		res.Columns = append(res.Columns, Column{Name: c})
	}
	var nodes []GraphNode
	var rels []GraphRel
	seenN, seenR := map[string]bool{}, map[string]bool{}
	props := func(p map[string]json.RawMessage) map[string]string {
		out := make(map[string]string, len(p))
		for k, v := range p {
			out[k] = JSONCell(v).Text
		}
		return out
	}
	for _, d := range r.Data {
		if maxRows > 0 && len(res.Rows) >= maxRows {
			res.Truncated = true
			break
		}
		row := make([]Cell, len(res.Columns))
		for i := range row {
			if i < len(d.Row) {
				row[i] = JSONCell(d.Row[i])
			} else {
				row[i] = Cell{Null: true}
			}
		}
		res.Rows = append(res.Rows, row)
		for _, n := range d.Graph.Nodes {
			if !seenN[n.ID] {
				seenN[n.ID] = true
				nodes = append(nodes, GraphNode{ID: n.ID, Labels: n.Labels, Props: props(n.Properties)})
			}
		}
		for _, rl := range d.Graph.Relationships {
			if !seenR[rl.ID] {
				seenR[rl.ID] = true
				rels = append(rels, GraphRel{From: rl.StartNode, To: rl.EndNode, Type: rl.Type, Props: props(rl.Properties)})
			}
		}
	}
	if len(nodes) > 0 || len(rels) > 0 {
		res.Notices = append(res.Notices, "Graph:\n"+GraphText(nodes, rels))
	}
	var changed int64
	var parts []string
	for _, k := range []string{"nodes_created", "nodes_deleted", "relationships_created", "relationships_deleted", "properties_set", "labels_added", "labels_removed", "indexes_added", "indexes_removed", "constraints_added", "constraints_removed"} {
		var n int64
		if v, ok := r.Stats[k]; ok && json.Unmarshal(v, &n) == nil && n > 0 {
			changed += n
			parts = append(parts, fmt.Sprintf("%s: %d", strings.ReplaceAll(k, "_", " "), n))
		}
	}
	if len(parts) > 0 {
		res.Affected = changed
		res.Notices = append(res.Notices, strings.Join(parts, ", "))
	}
	return res
}

// cypherIdent quotes a label, type or key with backticks.
func cypherIdent(s string) string { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }

// Neo4jSampleSize is how many nodes or relationships give their keys.
const Neo4jSampleSize = 50

func (s *Neo4jSession) Introspect(ctx context.Context, req IntrospectionRequest) (IntrospectionResult, error) {
	var out IntrospectionResult
	db := req.Database
	if db == "" {
		db = req.Schema
	}
	strs := func(resp neo4jResponse) []string {
		var names []string
		if len(resp.Results) == 0 {
			return nil
		}
		for _, d := range resp.Results[0].Data {
			if len(d.Row) > 0 {
				if c := JSONCell(d.Row[0]); !c.Null {
					names = append(names, c.Text)
				}
			}
		}
		return names
	}
	if db == "" && req.Object == "" {
		resp, err := s.commit(ctx, "system", "SHOW DATABASES YIELD name RETURN DISTINCT name ORDER BY name", nil)
		if err != nil {
			// Community editions before 4 have one database.
			out.Databases = []string{s.db}
			return out, nil
		}
		out.Databases = strs(resp)
		return out, nil
	}
	if db == "" {
		db = s.db
	}
	if req.Object == "" {
		labels, err := s.commit(ctx, db, "CALL db.labels() YIELD label RETURN label ORDER BY label", nil)
		if err != nil {
			return out, err
		}
		for _, l := range strs(labels) {
			out.Objects = append(out.Objects, SchemaObject{Name: ":" + l, Kind: "label"})
		}
		types, err := s.commit(ctx, db, "CALL db.relationshipTypes() YIELD relationshipType RETURN relationshipType ORDER BY relationshipType", nil)
		if err != nil {
			return out, err
		}
		for _, t := range strs(types) {
			out.Objects = append(out.Objects, SchemaObject{Name: "[:" + t + "]", Kind: "relationship type"})
		}
		return out, nil
	}
	var q string
	switch o := req.Object; {
	case strings.HasPrefix(o, "[:") && strings.HasSuffix(o, "]"):
		q = fmt.Sprintf("MATCH ()-[r:%s]->() WITH r LIMIT %d UNWIND keys(r) AS k RETURN DISTINCT k ORDER BY k", cypherIdent(o[2:len(o)-1]), Neo4jSampleSize)
	default:
		q = fmt.Sprintf("MATCH (n:%s) WITH n LIMIT %d UNWIND keys(n) AS k RETURN DISTINCT k ORDER BY k", cypherIdent(strings.TrimPrefix(o, ":")), Neo4jSampleSize)
	}
	resp, err := s.commit(ctx, db, q, nil)
	if err != nil {
		return out, err
	}
	out.Columns = []Column{{Name: "elementId", Type: "id", Table: req.Object, PK: true}}
	for _, k := range strs(resp) {
		out.Columns = append(out.Columns, Column{Name: k, Type: "property", Table: req.Object})
	}
	return out, nil
}

func (s *Neo4jSession) Begin(context.Context) error    { return errNoTx("Neo4j") }
func (s *Neo4jSession) Commit(context.Context) error   { return errNoTx("Neo4j") }
func (s *Neo4jSession) Rollback(context.Context) error { return errNoTx("Neo4j") }

// ReadOnlySession sends "access-mode: READ": the server refuses writes.
func (s *Neo4jSession) ReadOnlySession() (Session, error) {
	e := s.clone()
	e.header.Set("Access-Mode", "READ")
	return &Neo4jSession{httpEngine: e, db: s.db, readOnly: true}, nil
}
