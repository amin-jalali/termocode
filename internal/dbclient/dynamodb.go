package dbclient

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// DynamoDBSession runs PartiQL on Amazon DynamoDB over its JSON API
// (DBX-68), signed with SigV4: the connection's user is the access key
// id and the password the secret key. params.region (or the regional
// host) picks the region; a URL on localhost reaches DynamoDB Local over
// plain HTTP. A SELECT is a scan, or a query when its WHERE names the
// key; items show as rows (sets, lists and maps as JSON). The tree is
// the region, its tables, and a table's keys, attributes and indexes.
// DynamoDB has no read-only session: the read-only twin lets only reads
// through (warehouseGuard).
type DynamoDBSession struct {
	*awsJSONEngine
	conn     Conn
	region   string
	readOnly bool
}

func init() {
	registerHTTPEngine(HTTPEngine{
		ID: "dynamodb", Title: "Amazon DynamoDB", Port: 443, TLSPort: 443,
		Hint:        "PartiQL over the JSON API, SigV4 · access key id as user, secret key as password",
		Schemes:     []string{"dynamodb"},
		QueryParams: true,
		URLExample:  "dynamodb://ACCESS_KEY_ID@dynamodb.eu-west-1.amazonaws.com",
		Open: func(c Conn, password string) (Session, error) {
			return NewDynamoDBSession(c, password)
		},
		FixURL: func(c *Conn) {
			if r := awsHostRegion(c.Host, "dynamodb"); r != "" && c.Params["region"] == "" {
				c.Params["region"] = r
			}
			if h := strings.ToLower(c.Host); h == "localhost" || strings.HasPrefix(h, "127.") {
				c.TLS = false // DynamoDB Local
				return
			}
			c.TLS = true
			if c.Port == 0 || c.Port == 80 {
				c.Port = 443
			}
		},
	})
}

// NewDynamoDBSession makes a session of c; it does not connect.
func NewDynamoDBSession(c Conn, secret string) (*DynamoDBSession, error) {
	local := c.URL == "" && (strings.EqualFold(c.Host, "localhost") || strings.HasPrefix(c.Host, "127."))
	if local {
		// DynamoDB Local: plain HTTP on its own port.
		scheme := "http"
		if c.TLS {
			scheme = "https"
		}
		port := c.Port
		if port == 0 {
			port = 8000
		}
		c.URL = fmt.Sprintf("%s://%s:%d", scheme, c.Host, port)
	}
	a, err := newAWSJSONEngine("DynamoDB", "dynamodb", c, secret, "DynamoDB_20120810.", "application/x-amz-json-1.0")
	if err != nil {
		return nil, err
	}
	region := warehouseParam(c, "region")
	if region == "" {
		region = awsHostRegion(c.Host, "dynamodb")
	}
	return &DynamoDBSession{awsJSONEngine: a, conn: c, region: region}, nil
}

func (s *DynamoDBSession) Ping(ctx context.Context) error {
	return s.call(ctx, "ListTables", map[string]any{"Limit": 1}, nil)
}

// dynamoValue turns a DynamoDB attribute value into plain JSON.
func dynamoValue(av map[string]json.RawMessage) any {
	for typ, raw := range av {
		switch typ {
		case "S", "B":
			var s string
			_ = json.Unmarshal(raw, &s)
			return s
		case "N":
			var s string
			_ = json.Unmarshal(raw, &s)
			return json.Number(s)
		case "BOOL":
			var b bool
			_ = json.Unmarshal(raw, &b)
			return b
		case "NULL":
			return nil
		case "SS", "BS":
			var ss []string
			_ = json.Unmarshal(raw, &ss)
			return ss
		case "NS":
			var ss []string
			_ = json.Unmarshal(raw, &ss)
			out := make([]json.Number, len(ss))
			for i, n := range ss {
				out[i] = json.Number(n)
			}
			return out
		case "L":
			var l []map[string]json.RawMessage
			_ = json.Unmarshal(raw, &l)
			out := make([]any, len(l))
			for i, v := range l {
				out[i] = dynamoValue(v)
			}
			return out
		case "M":
			var m map[string]map[string]json.RawMessage
			_ = json.Unmarshal(raw, &m)
			out := make(map[string]any, len(m))
			for k, v := range m {
				out[k] = dynamoValue(v)
			}
			return out
		}
	}
	return nil
}

// dynamoItem is one item as a JSON object, its keys in name order.
func dynamoItem(item map[string]map[string]json.RawMessage) json.RawMessage {
	keys := make([]string, 0, len(item))
	for k := range item {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(dynamoValue(item[k]))
		b.Write(kb)
		b.WriteByte(':')
		b.Write(vb)
	}
	b.WriteByte('}')
	return json.RawMessage(b.String())
}

// dynamoParam is a statement argument as an attribute value.
func dynamoParam(a any) map[string]any {
	switch v := a.(type) {
	case nil:
		return map[string]any{"NULL": true}
	case bool:
		return map[string]any{"BOOL": v}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return map[string]any{"N": fmt.Sprint(v)}
	}
	return map[string]any{"S": fmt.Sprint(a)}
}

func (s *DynamoDBSession) Run(ctx context.Context, stmt string, opts RunOptions) Result {
	start := time.Now()
	res := s.run(ctx, strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(stmt), ";")), opts)
	res.Elapsed = time.Since(start)
	return res
}

func (s *DynamoDBSession) run(ctx context.Context, stmt string, opts RunOptions) Result {
	if err := warehouseGuard(s.readOnly, "DynamoDB", stmt); err != nil {
		return Result{Err: err}
	}
	k, _ := Classify(stmt, "postgres")
	var items []json.RawMessage
	token := ""
	truncated := false
	for range 1000 {
		in := map[string]any{"Statement": stmt}
		if len(opts.Args) > 0 {
			var ps []map[string]any
			for _, a := range opts.Args {
				ps = append(ps, dynamoParam(a))
			}
			in["Parameters"] = ps
		}
		if token != "" {
			in["NextToken"] = token
		}
		var out struct {
			Items     []map[string]map[string]json.RawMessage `json:"Items"`
			NextToken string                                  `json:"NextToken"`
		}
		if err := s.call(ctx, "ExecuteStatement", in, &out); err != nil {
			return Result{Err: err}
		}
		for _, it := range out.Items {
			items = append(items, dynamoItem(it))
		}
		token = out.NextToken
		if opts.MaxRows > 0 && len(items) >= opts.MaxRows {
			truncated = len(items) > opts.MaxRows || token != ""
			break
		}
		if token == "" {
			break
		}
	}
	res := TypedDocsResult(items, opts.MaxRows)
	res.Truncated = res.Truncated || truncated
	if k != KindRead {
		res.Affected = 1 // a PartiQL write changes one item
	}
	return res
}

func (s *DynamoDBSession) Introspect(ctx context.Context, req IntrospectionRequest) (IntrospectionResult, error) {
	var out IntrospectionResult
	switch {
	case req.Database == "" && req.Object == "":
		out.Databases = []string{s.region}
	case req.Object == "":
		start := ""
		for range 100 {
			in := map[string]any{"Limit": 100}
			if start != "" {
				in["ExclusiveStartTableName"] = start
			}
			var r struct {
				TableNames             []string `json:"TableNames"`
				LastEvaluatedTableName string   `json:"LastEvaluatedTableName"`
			}
			if err := s.call(ctx, "ListTables", in, &r); err != nil {
				return out, err
			}
			for _, n := range r.TableNames {
				out.Objects = append(out.Objects, SchemaObject{Name: n, Kind: "table"})
			}
			if start = r.LastEvaluatedTableName; start == "" {
				break
			}
		}
	default:
		type keyElem struct {
			AttributeName string `json:"AttributeName"`
			KeyType       string `json:"KeyType"`
		}
		var r struct {
			Table struct {
				KeySchema            []keyElem `json:"KeySchema"`
				AttributeDefinitions []struct {
					AttributeName string `json:"AttributeName"`
					AttributeType string `json:"AttributeType"`
				} `json:"AttributeDefinitions"`
				GlobalSecondaryIndexes, LocalSecondaryIndexes []struct {
					IndexName string    `json:"IndexName"`
					KeySchema []keyElem `json:"KeySchema"`
				}
			} `json:"Table"`
		}
		if err := s.call(ctx, "DescribeTable", map[string]any{"TableName": req.Object}, &r); err != nil {
			return out, err
		}
		key := map[string]bool{}
		for _, k := range r.Table.KeySchema {
			key[k.AttributeName] = true
			out.Keys = append(out.Keys, k.AttributeName)
		}
		types := map[string]string{}
		for _, a := range r.Table.AttributeDefinitions {
			types[a.AttributeName] = a.AttributeType
		}
		for _, k := range out.Keys { // keys first, in key order
			out.Columns = append(out.Columns, Column{Name: k, Type: types[k], Table: req.Object, PK: true})
		}
		for _, a := range r.Table.AttributeDefinitions {
			if !key[a.AttributeName] {
				out.Columns = append(out.Columns, Column{Name: a.AttributeName, Type: a.AttributeType, Table: req.Object})
			}
		}
		for _, ix := range append(r.Table.GlobalSecondaryIndexes, r.Table.LocalSecondaryIndexes...) {
			si := SchemaIndex{Name: ix.IndexName}
			for _, k := range ix.KeySchema {
				si.Columns = append(si.Columns, k.AttributeName)
			}
			out.Indexes = append(out.Indexes, si)
		}
	}
	return out, nil
}

func (s *DynamoDBSession) Begin(context.Context) error    { return errNoTx("DynamoDB") }
func (s *DynamoDBSession) Commit(context.Context) error   { return errNoTx("DynamoDB") }
func (s *DynamoDBSession) Rollback(context.Context) error { return errNoTx("DynamoDB") }

// ReadOnlySession is a twin that runs reads only.
func (s *DynamoDBSession) ReadOnlySession() (Session, error) {
	twin := &awsJSONEngine{httpEngine: s.clone(), target: s.target, ctype: s.ctype}
	return &DynamoDBSession{awsJSONEngine: twin, conn: s.conn, region: s.region, readOnly: true}, nil
}
