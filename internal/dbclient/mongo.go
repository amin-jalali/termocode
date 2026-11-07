package dbclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// MongoDB (DBX-68): one mongo-go-driver client per connection. A console
// line is parsed by ParseMongoShell and sent as the server command the
// shell itself would send, so what runs is exactly what the card showed.
// Results are read one batch at a time; an open cursor is killed.

// MongoPageSize is the batch size of a run without MaxRows.
const MongoPageSize = 500

// mongoSampleSize is how many documents the tree reads to list fields.
const mongoSampleSize = 20

// ErrMongoNoTransactions is returned by Begin, Commit and Rollback.
var ErrMongoNoTransactions = errors.New("MongoDB commands run on their own here; there are no SQL transactions")

// MongoExecutor runs one database command (tests use a fake).
type MongoExecutor interface {
	ListDatabases(ctx context.Context) ([]string, error)
	Command(ctx context.Context, db string, cmd MongoDoc) (MongoDoc, error)
	Close()
}

// MongoSession implements Session for MongoDB.
type MongoSession struct {
	cfg      Conn
	exec     MongoExecutor
	readonly bool
	shared   bool // a read-only view: Close leaves exec open

	mu     sync.Mutex
	db     string // the database of console commands ("use" changes it)
	closed bool
	cancel context.CancelFunc
}

// NewMongoSessionWith wraps exec (tests, and NewMongoSession).
func NewMongoSessionWith(c Conn, exec MongoExecutor) *MongoSession {
	db := c.Database
	if db == "" {
		db = "test"
	}
	return &MongoSession{cfg: c, exec: exec, readonly: c.ReadOnly, db: db}
}

// NewMongoSession connects to c. c.Dial (the SSH tunnel) is used when set;
// c.SSL or "tls": true turn on TLS. The password is never kept or shown.
func NewMongoSession(_ context.Context, c Conn, password string) (*MongoSession, error) {
	uri, err := mongoURI(c, password)
	if err != nil {
		return nil, err
	}
	opts := options.Client().ApplyURI(uri).SetAppName("termocode").
		SetServerSelectionTimeout(10 * time.Second).SetConnectTimeout(10 * time.Second)
	if c.Dial != nil {
		if strings.HasPrefix(uri, "mongodb+srv://") {
			return nil, errors.New("a mongodb+srv:// address cannot go through an SSH tunnel; use the host and port of one server")
		}
		// Through a tunnel only the given address is reachable.
		opts.SetDialer(mongoDialer(c.Dial)).SetDirect(true)
	}
	if c.SSL != nil || c.TLS {
		cfg, err := cassandraTLS(c)
		if err != nil {
			return nil, err
		}
		if cfg != nil {
			opts.SetTLSConfig(cfg)
		}
	}
	cl, err := mongo.Connect(opts)
	if err != nil {
		return nil, mongoScrub(fmt.Errorf("mongodb: %w", err), password)
	}
	return NewMongoSessionWith(c, &mongoDriverExec{c: cl, password: password}), nil
}

type mongoDialer DialFunc

func (d mongoDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return d(ctx, network, addr)
}

// mongoURI is the address the driver gets: the saved URL (or one made
// from host, port and database) with the password put in.
func mongoURI(c Conn, password string) (string, error) {
	var u *url.URL
	if c.URL != "" {
		p, err := url.Parse(c.URL)
		if err != nil || (p.Scheme != "mongodb" && p.Scheme != "mongodb+srv") {
			return "", errors.New("mongodb: the connection URL must start with mongodb:// or mongodb+srv://")
		}
		u = p
	} else {
		if c.Host == "" {
			return "", errors.New("mongodb: a host is needed")
		}
		port := c.Port
		if port == 0 {
			port = 27017
		}
		u = &url.URL{Scheme: "mongodb", Host: net.JoinHostPort(c.Host, strconv.Itoa(port)), Path: "/" + c.Database}
		q := url.Values{}
		for k, v := range c.Params {
			q.Set(k, v)
		}
		if c.User != "" && q.Get("authSource") == "" {
			// Users live in admin on most servers, not in the database
			// the console starts in.
			q.Set("authSource", "admin")
		}
		u.RawQuery = q.Encode()
	}
	user := c.User
	if user == "" && u.User != nil {
		user = u.User.Username()
	}
	if user != "" {
		if password != "" {
			u.User = url.UserPassword(user, password)
		} else {
			u.User = url.User(user)
		}
	}
	return u.String(), nil
}

// mongoScrub removes the password (plain or URL-escaped) from err.
func mongoScrub(err error, password string) error {
	if err == nil || password == "" {
		return err
	}
	text := err.Error()
	forms := []string{password, url.QueryEscape(password), url.PathEscape(password)}
	if s := url.UserPassword("x", password).String(); len(s) > 2 {
		forms = append(forms, s[2:])
	}
	sort.Slice(forms, func(i, j int) bool { return len(forms[i]) > len(forms[j]) })
	for _, f := range forms {
		if f != "" {
			text = strings.ReplaceAll(text, f, "***")
		}
	}
	return errors.New(text)
}

// mongoDriverExec is the MongoExecutor of a real client.
type mongoDriverExec struct {
	c        *mongo.Client
	password string
}

func (m *mongoDriverExec) ListDatabases(ctx context.Context) ([]string, error) {
	names, err := m.c.ListDatabaseNames(ctx, bson.D{})
	return names, mongoScrub(err, m.password)
}

func (m *mongoDriverExec) Command(ctx context.Context, db string, cmd MongoDoc) (MongoDoc, error) {
	var out bson.D
	if err := m.c.Database(db).RunCommand(ctx, mongoToBSON(cmd)).Decode(&out); err != nil {
		return nil, mongoScrub(err, m.password)
	}
	d, _ := mongoFromBSON(out).(MongoDoc)
	return d, nil
}

func (m *mongoDriverExec) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = m.c.Disconnect(ctx)
}

// mongoToBSON turns parsed shell values into driver values. Whole numbers
// that fit become int32, as the shell sends them.
func mongoToBSON(v any) any {
	switch x := v.(type) {
	case MongoDoc:
		d := make(bson.D, len(x))
		for i, f := range x {
			d[i] = bson.E{Key: f.Key, Value: mongoToBSON(f.Value)}
		}
		return d
	case []any:
		a := make(bson.A, len(x))
		for i, e := range x {
			a[i] = mongoToBSON(e)
		}
		return a
	case float64:
		if x == math.Trunc(x) && x >= math.MinInt32 && x <= math.MaxInt32 && !(x == 0 && math.Signbit(x)) {
			return int32(x)
		}
		return x
	case MongoObjectID:
		if id, err := bson.ObjectIDFromHex(string(x)); err == nil {
			return id
		}
		return string(x)
	case time.Time:
		return bson.NewDateTimeFromTime(x)
	}
	return v
}

// mongoFromBSON turns driver values into MongoDoc values.
func mongoFromBSON(v any) any {
	switch x := v.(type) {
	case bson.D:
		d := make(MongoDoc, len(x))
		for i, e := range x {
			d[i] = MongoField{Key: e.Key, Value: mongoFromBSON(e.Value)}
		}
		return d
	case bson.M:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		d := make(MongoDoc, len(keys))
		for i, k := range keys {
			d[i] = MongoField{Key: k, Value: mongoFromBSON(x[k])}
		}
		return d
	case bson.Raw:
		var d bson.D
		if err := bson.Unmarshal(x, &d); err != nil {
			return fmt.Sprint(x)
		}
		return mongoFromBSON(d)
	case bson.A:
		a := make([]any, len(x))
		for i, e := range x {
			a[i] = mongoFromBSON(e)
		}
		return a
	case []any:
		a := make([]any, len(x))
		for i, e := range x {
			a[i] = mongoFromBSON(e)
		}
		return a
	case bson.ObjectID:
		return MongoObjectID(x.Hex())
	case bson.DateTime:
		return x.Time().UTC()
	case bson.Decimal128:
		return x.String()
	case bson.Binary:
		return append([]byte(nil), x.Data...)
	case bson.Timestamp:
		return fmt.Sprintf("Timestamp(%d, %d)", x.T, x.I)
	case bson.Regex:
		return "/" + x.Pattern + "/" + x.Options
	case nil, bool, string, int32, int64, float64:
		return v
	}
	return fmt.Sprint(v)
}

// Get is the value of key ("" path parts are not split).
func (d MongoDoc) Get(key string) (any, bool) {
	for _, f := range d {
		if f.Key == key {
			return f.Value, true
		}
	}
	return nil, false
}

func mongoInt(v any) (int64, bool) {
	switch x := v.(type) {
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case int:
		return int64(x), true
	case float64:
		return int64(x), x == math.Trunc(x)
	}
	return 0, false
}

// MongoServerCommand is the server command of a parsed shell line. max is
// the batch size (0: MongoPageSize).
func MongoServerCommand(c MongoCommand, max int64) (MongoDoc, error) {
	if max <= 0 {
		max = MongoPageSize
	}
	cmd, err := mongoBaseCommand(c, max)
	if err != nil || !c.Explain {
		return cmd, err
	}
	mode := c.ExplainMode
	if mode == "" {
		mode = "queryPlanner"
	}
	switch mode {
	case "queryPlanner", "executionStats", "allPlansExecution":
	default:
		return nil, fmt.Errorf("mongo: explain mode %q is not one of queryPlanner, executionStats, allPlansExecution", mode)
	}
	switch c.Method {
	case "find", "findOne", "aggregate", "count", "countDocuments", "distinct",
		"updateOne", "updateMany", "replaceOne", "deleteOne", "deleteMany",
		"findOneAndUpdate", "findOneAndReplace", "findOneAndDelete":
	default:
		return nil, fmt.Errorf("mongo: %s cannot be explained", c.Method)
	}
	// The server plans an explained write but never applies it.
	return MongoDoc{{Key: "explain", Value: cmd}, {Key: "verbosity", Value: mode}}, nil
}

// mongoArgDoc is argument i as a document (absent: nil, ok).
func mongoArgDoc(c MongoCommand, i int, what string) (MongoDoc, error) {
	if i >= len(c.Args) {
		return nil, nil
	}
	d, ok := c.Args[i].(MongoDoc)
	if !ok {
		return nil, fmt.Errorf("mongo: the %s of %s must be a document", what, c.Method)
	}
	return d, nil
}

func mongoNeedDoc(c MongoCommand, i int, what string) (MongoDoc, error) {
	d, err := mongoArgDoc(c, i, what)
	if err == nil && d == nil {
		err = fmt.Errorf("mongo: %s needs a %s", c.Method, what)
	}
	return d, err
}

func mongoMaxArgs(c MongoCommand, n int) error {
	if len(c.Args) > n {
		return fmt.Errorf("mongo: %s takes at most %d arguments", c.Method, n)
	}
	return nil
}

func mongoBaseCommand(c MongoCommand, max int64) (MongoDoc, error) {
	coll := c.Collection
	if (len(c.Sort) > 0 || c.Skip > 0 || c.Limit > 0) && c.Method != "find" {
		return nil, errors.New("mongo: sort, skip and limit work only after find")
	}
	switch c.Method {
	case "find":
		if err := mongoMaxArgs(c, 2); err != nil {
			return nil, err
		}
		filter, err := mongoArgDoc(c, 0, "filter")
		if err != nil {
			return nil, err
		}
		proj, err := mongoArgDoc(c, 1, "projection")
		if err != nil {
			return nil, err
		}
		if filter == nil {
			filter = MongoDoc{}
		}
		cmd := MongoDoc{{Key: "find", Value: coll}, {Key: "filter", Value: filter}}
		if proj != nil {
			cmd = append(cmd, MongoField{Key: "projection", Value: proj})
		}
		if len(c.Sort) > 0 {
			cmd = append(cmd, MongoField{Key: "sort", Value: c.Sort})
		}
		if c.Skip > 0 {
			cmd = append(cmd, MongoField{Key: "skip", Value: c.Skip})
		}
		batch := max
		if c.Limit > 0 {
			cmd = append(cmd, MongoField{Key: "limit", Value: c.Limit})
			batch = min(batch, c.Limit)
		}
		return append(cmd, MongoField{Key: "batchSize", Value: batch}), nil
	case "findOne":
		if err := mongoMaxArgs(c, 2); err != nil {
			return nil, err
		}
		filter, err := mongoArgDoc(c, 0, "filter")
		if err != nil {
			return nil, err
		}
		proj, err := mongoArgDoc(c, 1, "projection")
		if err != nil {
			return nil, err
		}
		if filter == nil {
			filter = MongoDoc{}
		}
		cmd := MongoDoc{{Key: "find", Value: coll}, {Key: "filter", Value: filter}}
		if proj != nil {
			cmd = append(cmd, MongoField{Key: "projection", Value: proj})
		}
		return append(cmd, MongoField{Key: "limit", Value: int64(1)}, MongoField{Key: "singleBatch", Value: true}), nil
	case "aggregate":
		if err := mongoMaxArgs(c, 2); err != nil {
			return nil, err
		}
		if len(c.Args) == 0 {
			return nil, errors.New("mongo: aggregate needs a pipeline")
		}
		pipe, ok := c.Args[0].([]any)
		if !ok {
			return nil, errors.New("mongo: the pipeline of aggregate must be an array")
		}
		opts, err := mongoArgDoc(c, 1, "options")
		if err != nil {
			return nil, err
		}
		cmd := MongoDoc{{Key: "aggregate", Value: coll}, {Key: "pipeline", Value: pipe}, {Key: "cursor", Value: MongoDoc{{Key: "batchSize", Value: max}}}}
		for _, f := range opts {
			if f.Key != "cursor" && f.Key != "explain" {
				cmd = append(cmd, f)
			}
		}
		return cmd, nil
	case "count", "countDocuments":
		if err := mongoMaxArgs(c, 1); err != nil {
			return nil, err
		}
		filter, err := mongoArgDoc(c, 0, "filter")
		if err != nil {
			return nil, err
		}
		cmd := MongoDoc{{Key: "count", Value: coll}}
		if filter != nil {
			cmd = append(cmd, MongoField{Key: "query", Value: filter})
		}
		return cmd, nil
	case "estimatedDocumentCount":
		return MongoDoc{{Key: "count", Value: coll}}, mongoMaxArgs(c, 0)
	case "distinct":
		if err := mongoMaxArgs(c, 2); err != nil {
			return nil, err
		}
		key, ok := firstString(c.Args)
		if !ok || key == "" {
			return nil, errors.New("mongo: distinct needs a field name")
		}
		filter, err := mongoArgDoc(c, 1, "filter")
		if err != nil {
			return nil, err
		}
		cmd := MongoDoc{{Key: "distinct", Value: coll}, {Key: "key", Value: key}}
		if filter != nil {
			cmd = append(cmd, MongoField{Key: "query", Value: filter})
		}
		return cmd, nil
	case "getIndexes":
		return MongoDoc{{Key: "listIndexes", Value: coll}, {Key: "cursor", Value: MongoDoc{}}}, mongoMaxArgs(c, 0)
	case "listCollections":
		return MongoDoc{{Key: "listCollections", Value: int32(1)}, {Key: "nameOnly", Value: false}}, nil
	case "stats":
		return MongoDoc{{Key: "collStats", Value: coll}}, nil
	case "insertOne":
		doc, err := mongoNeedDoc(c, 0, "document")
		if err != nil {
			return nil, err
		}
		return MongoDoc{{Key: "insert", Value: coll}, {Key: "documents", Value: []any{doc}}}, nil
	case "insertMany":
		if len(c.Args) == 0 {
			return nil, errors.New("mongo: insertMany needs an array of documents")
		}
		docs, ok := c.Args[0].([]any)
		if !ok || len(docs) == 0 {
			return nil, errors.New("mongo: insertMany needs an array of documents")
		}
		for _, d := range docs {
			if _, ok := d.(MongoDoc); !ok {
				return nil, errors.New("mongo: insertMany takes documents only")
			}
		}
		return MongoDoc{{Key: "insert", Value: coll}, {Key: "documents", Value: docs}}, nil
	case "updateOne", "updateMany", "replaceOne":
		if err := mongoMaxArgs(c, 3); err != nil {
			return nil, err
		}
		q, err := mongoNeedDoc(c, 0, "filter")
		if err != nil {
			return nil, err
		}
		if len(c.Args) < 2 {
			return nil, fmt.Errorf("mongo: %s needs a filter and an update", c.Method)
		}
		u := c.Args[1]
		switch u.(type) {
		case MongoDoc:
		case []any:
			if c.Method == "replaceOne" {
				return nil, errors.New("mongo: replaceOne needs a document")
			}
		default:
			return nil, fmt.Errorf("mongo: the update of %s must be a document", c.Method)
		}
		opts, err := mongoArgDoc(c, 2, "options")
		if err != nil {
			return nil, err
		}
		upsert, _ := mongoOpt(opts, "upsert").(bool)
		entry := MongoDoc{{Key: "q", Value: q}, {Key: "u", Value: u}, {Key: "multi", Value: c.Method == "updateMany"}, {Key: "upsert", Value: upsert}}
		for _, k := range []string{"arrayFilters", "collation", "hint"} {
			if v := mongoOpt(opts, k); v != nil {
				entry = append(entry, MongoField{Key: k, Value: v})
			}
		}
		return MongoDoc{{Key: "update", Value: coll}, {Key: "updates", Value: []any{entry}}}, nil
	case "deleteOne", "deleteMany":
		if err := mongoMaxArgs(c, 2); err != nil {
			return nil, err
		}
		q, err := mongoNeedDoc(c, 0, "filter")
		if err != nil {
			return nil, err
		}
		limit := int32(0)
		if c.Method == "deleteOne" {
			limit = 1
		}
		return MongoDoc{{Key: "delete", Value: coll}, {Key: "deletes", Value: []any{MongoDoc{{Key: "q", Value: q}, {Key: "limit", Value: limit}}}}}, nil
	case "findOneAndDelete", "findOneAndUpdate", "findOneAndReplace":
		q, err := mongoNeedDoc(c, 0, "filter")
		if err != nil {
			return nil, err
		}
		cmd := MongoDoc{{Key: "findAndModify", Value: coll}, {Key: "query", Value: q}}
		optAt := 1
		if c.Method == "findOneAndDelete" {
			if err := mongoMaxArgs(c, 2); err != nil {
				return nil, err
			}
			cmd = append(cmd, MongoField{Key: "remove", Value: true})
		} else {
			if err := mongoMaxArgs(c, 3); err != nil {
				return nil, err
			}
			if len(c.Args) < 2 {
				return nil, fmt.Errorf("mongo: %s needs a filter and an update", c.Method)
			}
			switch c.Args[1].(type) {
			case MongoDoc, []any:
			default:
				return nil, fmt.Errorf("mongo: the update of %s must be a document", c.Method)
			}
			cmd = append(cmd, MongoField{Key: "update", Value: c.Args[1]})
			optAt = 2
		}
		opts, err := mongoArgDoc(c, optAt, "options")
		if err != nil {
			return nil, err
		}
		if v := mongoOpt(opts, "sort"); v != nil {
			cmd = append(cmd, MongoField{Key: "sort", Value: v})
		}
		if v := mongoOpt(opts, "projection"); v != nil {
			cmd = append(cmd, MongoField{Key: "fields", Value: v})
		}
		if b, _ := mongoOpt(opts, "returnNewDocument").(bool); b || mongoOpt(opts, "returnDocument") == "after" {
			cmd = append(cmd, MongoField{Key: "new", Value: true})
		}
		if b, _ := mongoOpt(opts, "upsert").(bool); b {
			cmd = append(cmd, MongoField{Key: "upsert", Value: true})
		}
		return cmd, nil
	case "createIndex", "createIndexes":
		if err := mongoMaxArgs(c, 2); err != nil {
			return nil, err
		}
		var keys []MongoDoc
		if c.Method == "createIndex" {
			k, err := mongoNeedDoc(c, 0, "key")
			if err != nil {
				return nil, err
			}
			keys = []MongoDoc{k}
		} else {
			arr, ok := firstArray(c.Args)
			if !ok || len(arr) == 0 {
				return nil, errors.New("mongo: createIndexes needs an array of keys")
			}
			for _, a := range arr {
				k, ok := a.(MongoDoc)
				if !ok {
					return nil, errors.New("mongo: createIndexes takes key documents only")
				}
				keys = append(keys, k)
			}
		}
		opts, err := mongoArgDoc(c, 1, "options")
		if err != nil {
			return nil, err
		}
		var specs []any
		for _, k := range keys {
			if len(k) == 0 {
				return nil, errors.New("mongo: an index key needs at least one field")
			}
			name, _ := mongoOpt(opts, "name").(string)
			if name == "" || len(keys) > 1 {
				name = mongoIndexName(k)
			}
			spec := MongoDoc{{Key: "key", Value: k}, {Key: "name", Value: name}}
			for _, f := range opts {
				if f.Key != "name" && f.Key != "key" {
					spec = append(spec, f)
				}
			}
			specs = append(specs, spec)
		}
		return MongoDoc{{Key: "createIndexes", Value: coll}, {Key: "indexes", Value: specs}}, nil
	case "dropIndex":
		if len(c.Args) != 1 {
			return nil, errors.New("mongo: dropIndex needs an index name or key")
		}
		switch c.Args[0].(type) {
		case string, MongoDoc:
		default:
			return nil, errors.New("mongo: dropIndex needs an index name or key")
		}
		return MongoDoc{{Key: "dropIndexes", Value: coll}, {Key: "index", Value: c.Args[0]}}, nil
	case "dropIndexes":
		if err := mongoMaxArgs(c, 1); err != nil {
			return nil, err
		}
		idx := any("*")
		if len(c.Args) == 1 {
			idx = c.Args[0]
		}
		return MongoDoc{{Key: "dropIndexes", Value: coll}, {Key: "index", Value: idx}}, nil
	case "drop":
		return MongoDoc{{Key: "drop", Value: coll}}, mongoMaxArgs(c, 0)
	case "bulkWrite", "renameCollection":
		return nil, fmt.Errorf("mongo: %s is not supported in the console; use the single methods", c.Method)
	}
	return nil, fmt.Errorf("mongo: unsupported method %q", c.Method)
}

func firstArray(args []any) ([]any, bool) {
	if len(args) == 0 {
		return nil, false
	}
	a, ok := args[0].([]any)
	return a, ok
}

func mongoOpt(opts MongoDoc, key string) any {
	v, _ := opts.Get(key)
	return v
}

// mongoIndexName is the server's default name: field_dir joined by "_".
func mongoIndexName(key MongoDoc) string {
	parts := make([]string, 0, 2*len(key))
	for _, f := range key {
		parts = append(parts, f.Key, mongoScalar(f.Value))
	}
	return strings.Join(parts, "_")
}

func mongoScalar(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	}
	return fmt.Sprint(v)
}

// MongoShellJSON is v as compact shell JSON: keys in order, ObjectId("…")
// and ISODate("…") as the shell writes them, so a value can be pasted
// back into a filter.
func MongoShellJSON(v any) string {
	var b strings.Builder
	mongoJSON(&b, v, "", 0)
	return b.String()
}

// mongoPrettyJSON is MongoShellJSON with one field per line.
func mongoPrettyJSON(v any) string {
	var b strings.Builder
	mongoJSON(&b, v, "  ", 0)
	return b.String()
}

func mongoJSON(b *strings.Builder, v any, indent string, depth int) {
	sep := func(first bool, d int) {
		if !first {
			b.WriteByte(',')
			if indent == "" {
				b.WriteByte(' ')
			}
		}
		if indent != "" {
			b.WriteByte('\n')
			b.WriteString(strings.Repeat(indent, d))
		}
	}
	closeLine := func() {
		if indent != "" {
			b.WriteByte('\n')
			b.WriteString(strings.Repeat(indent, depth))
		}
	}
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case string:
		b.WriteString(mongoQuote(x))
	case float64:
		b.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
	case int32, int64, int:
		fmt.Fprint(b, x)
	case MongoObjectID:
		b.WriteString(`ObjectId("` + string(x) + `")`)
	case time.Time:
		b.WriteString(`ISODate("` + x.UTC().Format(time.RFC3339Nano) + `")`)
	case []byte:
		fmt.Fprintf(b, `BinData(0, "%x")`, x)
	case MongoDoc:
		if len(x) == 0 {
			b.WriteString("{}")
			return
		}
		b.WriteByte('{')
		for i, f := range x {
			sep(i == 0, depth+1)
			b.WriteString(mongoQuote(f.Key))
			b.WriteString(": ")
			mongoJSON(b, f.Value, indent, depth+1)
		}
		closeLine()
		b.WriteByte('}')
	case []any:
		if len(x) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteByte('[')
		for i, e := range x {
			sep(i == 0, depth+1)
			mongoJSON(b, e, indent, depth+1)
		}
		closeLine()
		b.WriteByte(']')
	default:
		b.WriteString(mongoQuote(fmt.Sprint(v)))
	}
}

func mongoQuote(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return strconv.Quote(s)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// mongoCell is the grid cell of one value: nested values as compact JSON.
func mongoCell(v any) Cell {
	switch x := v.(type) {
	case MongoDoc, []any:
		return Cell{Text: MongoShellJSON(x), Kind: "json"}
	case MongoObjectID:
		return Cell{Text: MongoShellJSON(x)}
	case float64:
		return Cell{Text: strconv.FormatFloat(x, 'g', -1, 64)}
	}
	return CellOf(v)
}

// mongoTypeName is the shell's name of the type of v.
func mongoTypeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case MongoObjectID:
		return "objectId"
	case string:
		return "string"
	case int32:
		return "int"
	case int64:
		return "long"
	case float64:
		return "double"
	case bool:
		return "bool"
	case time.Time:
		return "date"
	case MongoDoc:
		return "object"
	case []any:
		return "array"
	case []byte:
		return "binData"
	}
	return ""
}

// mongoRows lays documents out as a grid: _id first, then every top-level
// key in the order first seen. A missing field is null.
func mongoRows(docs []any) ([]Column, [][]Cell) {
	seen := map[string]bool{}
	var names []string
	hasID := false
	for _, d := range docs {
		doc, ok := d.(MongoDoc)
		if !ok {
			continue
		}
		for _, f := range doc {
			if f.Key == "_id" {
				hasID = true
				continue
			}
			if !seen[f.Key] {
				seen[f.Key] = true
				names = append(names, f.Key)
			}
		}
	}
	if hasID {
		names = append([]string{"_id"}, names...)
	}
	cols := make([]Column, len(names))
	types := make([]string, len(names))
	idx := make(map[string]int, len(names))
	for i, n := range names {
		cols[i] = Column{Name: n, PK: n == "_id"}
		idx[n] = i
	}
	rows := make([][]Cell, 0, len(docs))
	for _, d := range docs {
		doc, ok := d.(MongoDoc)
		if !ok {
			continue
		}
		row := make([]Cell, len(names))
		for i := range row {
			row[i] = Cell{Null: true}
		}
		for _, f := range doc {
			i := idx[f.Key]
			row[i] = mongoCell(f.Value)
			if types[i] == "" && f.Value != nil {
				types[i] = mongoTypeName(f.Value)
			}
		}
		rows = append(rows, row)
	}
	for i := range cols {
		cols[i].Type = types[i]
	}
	return cols, rows
}

// mongoStripComments drops whole-line // and -- comments (the console
// header is one).
func mongoStripComments(src string) string {
	var keep []string
	for _, l := range strings.Split(src, "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "//") || strings.HasPrefix(t, "--") {
			continue
		}
		keep = append(keep, l)
	}
	return strings.TrimSpace(strings.Join(keep, "\n"))
}

// mongoUse reads "use <db>": ok is false for any other line.
func mongoUse(src string) (db string, ok bool, err error) {
	f := strings.Fields(strings.TrimSuffix(strings.TrimSpace(src), ";"))
	if len(f) == 0 || f[0] != "use" {
		return "", false, nil
	}
	if len(f) != 2 || !mongoDBName(f[1]) {
		return "", true, errors.New("mongo: use needs one database name (no spaces, / \\ . \" $)")
	}
	return f[1], true, nil
}

func mongoDBName(s string) bool {
	return s != "" && len(s) < 64 && !strings.ContainsAny(s, "/\\. \"$*<>:|?\x00")
}

// classifyMongo is Classify for a MongoDB shell line (fail closed).
func classifyMongo(src string) Kind {
	text := mongoStripComments(src)
	if _, ok, err := mongoUse(text); ok {
		if err != nil {
			return KindWrite
		}
		return KindRead
	}
	cmd, err := ParseMongoShell(text)
	if err != nil {
		return KindWrite
	}
	return MongoKind(cmd)
}

func (s *MongoSession) database() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.db
}

func (s *MongoSession) start(ctx context.Context) (context.Context, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, nil, errors.New("the MongoDB session is closed")
	}
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	return ctx, cancel, nil
}

func (s *MongoSession) command(ctx context.Context, db string, cmd MongoDoc) (MongoDoc, error) {
	ctx, done, err := s.start(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	return s.exec.Command(ctx, db, cmd)
}

func (s *MongoSession) Ping(ctx context.Context) error {
	_, err := s.command(ctx, "admin", MongoDoc{{Key: "ping", Value: int32(1)}})
	return err
}

const mongoGridWhy = "MongoDB documents are read-only in the grid; change them with updateOne in the console"

// Run parses one shell line and runs it in the current database.
func (s *MongoSession) Run(ctx context.Context, stmt string, opts RunOptions) Result {
	start := time.Now()
	src := mongoStripComments(stmt)
	if db, ok, err := mongoUse(src); ok {
		if err != nil {
			return Result{Err: err}
		}
		s.mu.Lock()
		s.db = db
		s.mu.Unlock()
		return Result{Notices: []string{"switched to db " + db}, Elapsed: time.Since(start)}
	}
	cmd, err := ParseMongoShell(src)
	if err != nil {
		return Result{Err: err}
	}
	if s.readonly && MongoKind(cmd) != KindRead {
		return Result{Err: errors.New("read-only session: only reads run here (find, aggregate without $out or $merge, counts, distinct, indexes)")}
	}
	max := int64(opts.MaxRows)
	if max <= 0 {
		max = MongoPageSize
	}
	doc, err := MongoServerCommand(cmd, max)
	if err != nil {
		return Result{Err: err}
	}
	db := s.database()
	ctx, done, err := s.start(ctx)
	if err != nil {
		return Result{Err: err}
	}
	defer done()
	resp, err := s.exec.Command(ctx, db, doc)
	if err != nil {
		return Result{Err: err, Elapsed: time.Since(start), Why: mongoGridWhy}
	}
	res := s.result(ctx, db, cmd, resp)
	res.Elapsed = time.Since(start)
	res.Why = mongoGridWhy
	return res
}

// mongoWriteError is the first write error of a reply (nil: none).
func mongoWriteError(resp MongoDoc) error {
	if v, ok := resp.Get("writeErrors"); ok {
		if errs, ok := v.([]any); ok && len(errs) > 0 {
			if d, ok := errs[0].(MongoDoc); ok {
				msg, _ := d.Get("errmsg")
				more := ""
				if len(errs) > 1 {
					more = fmt.Sprintf(" (and %d more)", len(errs)-1)
				}
				return fmt.Errorf("mongo: %v%s", msg, more)
			}
			return errors.New("mongo: the write failed")
		}
	}
	if v, ok := resp.Get("writeConcernError"); ok {
		if d, ok := v.(MongoDoc); ok {
			msg, _ := d.Get("errmsg")
			return fmt.Errorf("mongo: write concern: %v", msg)
		}
	}
	return nil
}

// mongoReplyFields drops the reply's bookkeeping fields.
func mongoReplyFields(resp MongoDoc) MongoDoc {
	out := make(MongoDoc, 0, len(resp))
	for _, f := range resp {
		switch f.Key {
		case "ok", "$clusterTime", "operationTime":
			continue
		}
		out = append(out, f)
	}
	return out
}

func (s *MongoSession) result(ctx context.Context, db string, cmd MongoCommand, resp MongoDoc) Result {
	var res Result
	if err := mongoWriteError(resp); err != nil {
		res.Err = err
		return res
	}
	if cmd.Explain {
		res.Columns = []Column{{Name: "plan", Type: "json"}}
		res.Rows = [][]Cell{{{Text: mongoPrettyJSON(mongoReplyFields(resp)), Kind: "json"}}}
		return res
	}
	n := func(key string) int64 {
		v, _ := resp.Get(key)
		i, _ := mongoInt(v)
		return i
	}
	switch cmd.Method {
	case "find", "findOne", "aggregate", "getIndexes", "listCollections":
		docs, id, ns := mongoCursor(resp)
		res.Columns, res.Rows = mongoRows(docs)
		if id != 0 {
			res.Truncated = true
			coll := cmd.Collection
			if coll == "" {
				_, coll, _ = strings.Cut(ns, ".")
			}
			if _, err := s.exec.Command(ctx, db, MongoDoc{{Key: "killCursors", Value: coll}, {Key: "cursors", Value: []any{id}}}); err != nil {
				res.Notices = append(res.Notices, "the open cursor was not closed: "+err.Error())
			}
		}
	case "count", "countDocuments", "estimatedDocumentCount":
		res.Columns = []Column{{Name: "count", Type: "long"}}
		res.Rows = [][]Cell{{CellOf(n("n"))}}
	case "distinct":
		res.Columns = []Column{{Name: "value"}}
		vals, _ := resp.Get("values")
		list, _ := vals.([]any)
		for _, v := range list {
			res.Rows = append(res.Rows, []Cell{mongoCell(v)})
		}
	case "stats":
		res.Columns = []Column{{Name: "field"}, {Name: "value"}}
		for _, f := range mongoReplyFields(resp) {
			res.Rows = append(res.Rows, []Cell{{Text: f.Key}, mongoCell(f.Value)})
		}
	case "insertOne", "insertMany":
		res.Affected = n("n")
		res.Notices = []string{fmt.Sprintf("inserted %d", res.Affected)}
	case "updateOne", "updateMany", "replaceOne":
		res.Affected = n("nModified")
		up, _ := resp.Get("upserted")
		ups, _ := up.([]any)
		res.Notices = []string{fmt.Sprintf("matched %d, modified %d, upserted %d", n("n")-int64(len(ups)), res.Affected, len(ups))}
	case "deleteOne", "deleteMany":
		res.Affected = n("n")
		res.Notices = []string{fmt.Sprintf("deleted %d", res.Affected)}
	case "findOneAndDelete", "findOneAndUpdate", "findOneAndReplace":
		if leo, ok := resp.Get("lastErrorObject"); ok {
			if d, ok := leo.(MongoDoc); ok {
				v, _ := d.Get("n")
				res.Affected, _ = mongoInt(v)
			}
		}
		if v, _ := resp.Get("value"); v != nil {
			res.Columns, res.Rows = mongoRows([]any{v})
		}
	case "createIndex", "createIndexes":
		res.Notices = []string{fmt.Sprintf("indexes: %d before, %d after", n("numIndexesBefore"), n("numIndexesAfter"))}
	default:
		res.Notices = []string{"done"}
	}
	return res
}

// mongoCursor reads a cursor reply: its first batch, id and namespace.
func mongoCursor(resp MongoDoc) ([]any, int64, string) {
	v, _ := resp.Get("cursor")
	cur, _ := v.(MongoDoc)
	b, _ := cur.Get("firstBatch")
	docs, _ := b.([]any)
	idv, _ := cur.Get("id")
	id, _ := mongoInt(idv)
	nsv, _ := cur.Get("ns")
	ns, _ := nsv.(string)
	return docs, id, ns
}

func (s *MongoSession) Begin(context.Context) error    { return ErrMongoNoTransactions }
func (s *MongoSession) Commit(context.Context) error   { return ErrMongoNoTransactions }
func (s *MongoSession) Rollback(context.Context) error { return ErrMongoNoTransactions }

// ReadOnlySession shares the client and sends reads only.
func (s *MongoSession) ReadOnlySession() (Session, error) {
	return &MongoSession{cfg: s.cfg, exec: s.exec, readonly: true, shared: true, db: s.database()}, nil
}

func (s *MongoSession) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}

func (s *MongoSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	if !s.shared {
		s.exec.Close()
	}
	return nil
}

// Introspect lists databases, then the collections and views of one
// (Database, or Schema), then the fields seen in the first documents of a
// collection, its indexes and their createIndex lines.
func (s *MongoSession) Introspect(ctx context.Context, req IntrospectionRequest) (IntrospectionResult, error) {
	var out IntrospectionResult
	if req.Database == "" && req.Schema == "" && req.Object == "" {
		ctx, done, err := s.start(ctx)
		if err != nil {
			return out, err
		}
		defer done()
		names, err := s.exec.ListDatabases(ctx)
		sort.Strings(names)
		out.Databases = names
		return out, err
	}
	db := req.Database
	if db == "" {
		db = req.Schema
	}
	if db == "" {
		db = s.database()
	}
	if req.Object == "" {
		resp, err := s.command(ctx, db, MongoDoc{{Key: "listCollections", Value: int32(1)}, {Key: "nameOnly", Value: false}})
		if err != nil {
			return out, err
		}
		docs, _, _ := mongoCursor(resp)
		var colls, views []string
		for _, d := range docs {
			doc, _ := d.(MongoDoc)
			nv, _ := doc.Get("name")
			name, _ := nv.(string)
			if name == "" {
				continue
			}
			if t, _ := doc.Get("type"); t == "view" {
				views = append(views, name)
			} else {
				colls = append(colls, name)
			}
		}
		sort.Strings(colls)
		sort.Strings(views)
		for _, n := range colls {
			out.Objects = append(out.Objects, SchemaObject{Name: n, Kind: "table"})
		}
		for _, n := range views {
			out.Objects = append(out.Objects, SchemaObject{Name: n, Kind: "view"})
		}
		return out, nil
	}
	resp, err := s.command(ctx, db, MongoDoc{{Key: "find", Value: req.Object}, {Key: "filter", Value: MongoDoc{}}, {Key: "limit", Value: int64(mongoSampleSize)}, {Key: "singleBatch", Value: true}})
	if err != nil {
		return out, err
	}
	docs, _, _ := mongoCursor(resp)
	cols, _ := mongoRows(docs)
	for _, c := range cols {
		c.Table = req.Object
		out.Columns = append(out.Columns, c)
	}
	var ddl strings.Builder
	fmt.Fprintf(&ddl, "// %s.%s: %d fields in the first %d documents\n", db, req.Object, len(cols), mongoSampleSize)
	if req.Kind == "view" {
		out.DDL = ddl.String()
		return out, nil
	}
	resp, err = s.command(ctx, db, MongoDoc{{Key: "listIndexes", Value: req.Object}, {Key: "cursor", Value: MongoDoc{}}})
	if err != nil {
		return out, err
	}
	idx, _, _ := mongoCursor(resp)
	for _, d := range idx {
		doc, _ := d.(MongoDoc)
		nv, _ := doc.Get("name")
		name, _ := nv.(string)
		kv, _ := doc.Get("key")
		key, _ := kv.(MongoDoc)
		uv, _ := doc.Get("unique")
		unique, _ := uv.(bool)
		si := SchemaIndex{Name: name, Unique: unique}
		for _, f := range key {
			si.Columns = append(si.Columns, f.Key)
		}
		out.Indexes = append(out.Indexes, si)
		if name == "_id_" {
			continue
		}
		opts := MongoDoc{{Key: "name", Value: name}}
		for _, f := range doc {
			switch f.Key {
			case "v", "key", "name", "ns":
				continue
			}
			opts = append(opts, f)
		}
		fmt.Fprintf(&ddl, "db.getCollection(%s).createIndex(%s, %s)\n", mongoQuote(req.Object), MongoShellJSON(key), MongoShellJSON(opts))
	}
	out.DDL = strings.TrimSuffix(ddl.String(), "\n")
	return out, nil
}
