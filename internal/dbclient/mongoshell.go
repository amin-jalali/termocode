package dbclient

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// MongoField is one key/value pair of a MongoDB document. Documents keep
// their key order because the server cares (sort specs, index keys).
type MongoField struct {
	Key   string
	Value any
}

// MongoDoc is an ordered document. Values are MongoDoc, []any, string,
// float64, bool or nil.
type MongoDoc []MongoField

// MongoCommand is one MongoDB shell console line:
// db.<coll>.<method>(args...)[.sort(..)][.skip(n)][.limit(n)][.explain(..)].
type MongoCommand struct {
	Collection  string
	Method      string
	Args        []any
	Sort        MongoDoc
	Skip        int64
	Limit       int64
	Explain     bool
	ExplainMode string
}

const mongoMaxDepth = 64

// ParseMongoShell parses one console line. It accepts relaxed JSON (bare
// keys, single quotes, trailing commas) and rejects anything after the
// command, so two commands on one line never run as one.
func ParseMongoShell(src string) (MongoCommand, error) {
	p := &mongoParser{s: src}
	p.ws()
	if p.word("show") {
		p.ws()
		if !(p.word("collections") || p.word("tables")) {
			return MongoCommand{}, errors.New("mongo: show supports only collections")
		}
		if err := p.end(); err != nil {
			return MongoCommand{}, err
		}
		return MongoCommand{Method: "listCollections"}, nil
	}
	if !p.word("db") {
		return MongoCommand{}, errors.New("mongo: a command starts with db.")
	}
	if err := p.expect('.'); err != nil {
		return MongoCommand{}, err
	}
	var cmd MongoCommand
	name := p.ident()
	if name == "" {
		return MongoCommand{}, p.errf("collection name expected")
	}
	if name == "getCollection" {
		args, err := p.args()
		if err != nil {
			return MongoCommand{}, err
		}
		coll, ok := firstString(args)
		if !ok || len(args) != 1 || coll == "" {
			return MongoCommand{}, p.errf("getCollection needs one name")
		}
		name = coll
	}
	cmd.Collection = name
	if err := p.expect('.'); err != nil {
		return MongoCommand{}, err
	}
	if cmd.Method = p.ident(); cmd.Method == "" {
		return MongoCommand{}, p.errf("method name expected")
	}
	args, err := p.args()
	if err != nil {
		return MongoCommand{}, err
	}
	cmd.Args = args
	for {
		p.ws()
		if p.done() || p.peek() == ';' {
			break
		}
		if err := p.expect('.'); err != nil {
			return MongoCommand{}, err
		}
		mod := p.ident()
		margs, err := p.args()
		if err != nil {
			return MongoCommand{}, err
		}
		if err := cmd.applyModifier(mod, margs); err != nil {
			return MongoCommand{}, err
		}
	}
	if err := p.end(); err != nil {
		return MongoCommand{}, err
	}
	return cmd, nil
}

func (c *MongoCommand) applyModifier(mod string, args []any) error {
	switch mod {
	case "sort":
		d, ok := oneDoc(args)
		if !ok {
			return errors.New("mongo: sort needs one document")
		}
		c.Sort = d
	case "skip", "limit":
		n, ok := oneCount(args)
		if !ok {
			return fmt.Errorf("mongo: %s needs one whole number", mod)
		}
		if mod == "skip" {
			c.Skip = n
		} else {
			c.Limit = n
		}
	case "explain":
		c.Explain = true
		if len(args) > 1 {
			return errors.New("mongo: explain takes at most one mode")
		}
		if len(args) == 1 {
			s, ok := args[0].(string)
			if !ok {
				return errors.New("mongo: explain mode must be a string")
			}
			c.ExplainMode = s
		}
	case "pretty", "toArray":
		if len(args) != 0 {
			return fmt.Errorf("mongo: %s takes no arguments", mod)
		}
	default:
		return fmt.Errorf("mongo: unknown cursor method %q", mod)
	}
	return nil
}

var mongoReadMethods = map[string]bool{
	"find": true, "findOne": true, "aggregate": true, "countDocuments": true,
	"estimatedDocumentCount": true, "count": true, "distinct": true,
	"getIndexes": true, "listCollections": true, "stats": true,
}

var mongoWriteMethods = map[string]bool{
	"insertOne": true, "insertMany": true, "updateOne": true, "updateMany": true,
	"replaceOne": true, "deleteOne": true, "deleteMany": true, "bulkWrite": true,
	"findOneAndUpdate": true, "findOneAndReplace": true, "findOneAndDelete": true,
}

var mongoDDLMethods = map[string]bool{
	"createIndex": true, "createIndexes": true, "dropIndex": true,
	"dropIndexes": true, "drop": true, "renameCollection": true,
}

// MongoKind classifies a command for read-only and production rules. It is a
// hint like Classify: unknown methods count as writes (fail closed), and an
// aggregate with a $out or $merge stage is a write.
func MongoKind(c MongoCommand) Kind {
	switch {
	case mongoDDLMethods[c.Method]:
		return KindDDL
	case mongoWriteMethods[c.Method]:
		return KindWrite
	case c.Method == "aggregate":
		if aggregateWrites(c.Args) {
			return KindWrite
		}
		return KindRead
	case mongoReadMethods[c.Method]:
		return KindRead
	}
	return KindWrite
}

func aggregateWrites(args []any) bool {
	if len(args) == 0 {
		return false
	}
	stages, ok := args[0].([]any)
	if !ok {
		return true
	}
	for _, st := range stages {
		d, ok := st.(MongoDoc)
		if !ok {
			return true
		}
		for _, f := range d {
			if f.Key == "$out" || f.Key == "$merge" {
				return true
			}
		}
	}
	return false
}

func firstString(args []any) (string, bool) {
	if len(args) == 0 {
		return "", false
	}
	s, ok := args[0].(string)
	return s, ok
}

func oneDoc(args []any) (MongoDoc, bool) {
	if len(args) != 1 {
		return nil, false
	}
	d, ok := args[0].(MongoDoc)
	return d, ok
}

func oneCount(args []any) (int64, bool) {
	if len(args) != 1 {
		return 0, false
	}
	f, ok := args[0].(float64)
	if !ok || f < 0 || f > 1<<53 || f != float64(int64(f)) {
		return 0, false
	}
	return int64(f), true
}

type mongoParser struct {
	s     string
	i     int
	depth int
}

func (p *mongoParser) errf(format string, a ...any) error {
	return fmt.Errorf("mongo: at %d: %s", p.i, fmt.Sprintf(format, a...))
}

func (p *mongoParser) done() bool { return p.i >= len(p.s) }

func (p *mongoParser) peek() byte {
	if p.done() {
		return 0
	}
	return p.s[p.i]
}

func (p *mongoParser) ws() {
	for !p.done() {
		switch p.s[p.i] {
		case ' ', '\t', '\n', '\r':
			p.i++
		default:
			return
		}
	}
}

func (p *mongoParser) expect(c byte) error {
	p.ws()
	if p.peek() != c {
		return p.errf("%q expected", c)
	}
	p.i++
	return nil
}

// end allows one trailing ';' and nothing else.
func (p *mongoParser) end() error {
	p.ws()
	if p.peek() == ';' {
		p.i++
		p.ws()
	}
	if !p.done() {
		return p.errf("unexpected text after the command")
	}
	return nil
}

func isIdentByte(c byte, first bool) bool {
	switch {
	case c == '_' || c == '$':
		return true
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return !first
	}
	return false
}

func (p *mongoParser) ident() string {
	p.ws()
	start := p.i
	for !p.done() && isIdentByte(p.s[p.i], p.i == start) {
		p.i++
	}
	return p.s[start:p.i]
}

// word consumes w only when it is a whole identifier.
func (p *mongoParser) word(w string) bool {
	if !strings.HasPrefix(p.s[p.i:], w) {
		return false
	}
	end := p.i + len(w)
	if end < len(p.s) && isIdentByte(p.s[end], false) {
		return false
	}
	p.i = end
	return true
}

func (p *mongoParser) args() ([]any, error) {
	if err := p.expect('('); err != nil {
		return nil, err
	}
	var out []any
	p.ws()
	if p.peek() == ')' {
		p.i++
		return out, nil
	}
	for {
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		p.ws()
		switch p.peek() {
		case ',':
			p.i++
		case ')':
			p.i++
			return out, nil
		default:
			return nil, p.errf("',' or ')' expected")
		}
	}
}

func (p *mongoParser) value() (any, error) {
	p.ws()
	switch c := p.peek(); {
	case c == '{':
		return p.object()
	case c == '[':
		return p.array()
	case c == '"' || c == '\'':
		return p.str()
	case c == '-' || c == '+' || c == '.' || (c >= '0' && c <= '9'):
		return p.number()
	}
	switch w := p.ident(); w {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null", "undefined":
		return nil, nil
	case "":
		return nil, p.errf("value expected")
	default:
		return nil, p.errf("unsupported value %q", w)
	}
}

func (p *mongoParser) enter() error {
	p.depth++
	if p.depth > mongoMaxDepth {
		return p.errf("nested too deep")
	}
	return nil
}

func (p *mongoParser) object() (any, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer func() { p.depth-- }()
	p.i++ // '{'
	doc := MongoDoc{}
	for {
		p.ws()
		if p.peek() == '}' {
			p.i++
			return doc, nil
		}
		var key string
		if c := p.peek(); c == '"' || c == '\'' {
			s, err := p.str()
			if err != nil {
				return nil, err
			}
			key = s
		} else {
			key = p.dottedIdent()
			if key == "" {
				return nil, p.errf("key expected")
			}
		}
		if err := p.expect(':'); err != nil {
			return nil, err
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		doc = append(doc, MongoField{Key: key, Value: v})
		p.ws()
		switch p.peek() {
		case ',':
			p.i++
		case '}':
		default:
			return nil, p.errf("',' or '}' expected")
		}
	}
}

// dottedIdent reads a bare key; dotted paths like a.b are common in filters.
func (p *mongoParser) dottedIdent() string {
	p.ws()
	start := p.i
	for !p.done() && (isIdentByte(p.s[p.i], p.i == start) || (p.i > start && p.s[p.i] == '.')) {
		p.i++
	}
	return p.s[start:p.i]
}

func (p *mongoParser) array() (any, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer func() { p.depth-- }()
	p.i++ // '['
	out := []any{}
	for {
		p.ws()
		if p.peek() == ']' {
			p.i++
			return out, nil
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		p.ws()
		switch p.peek() {
		case ',':
			p.i++
		case ']':
		default:
			return nil, p.errf("',' or ']' expected")
		}
	}
}

func (p *mongoParser) str() (string, error) {
	q := p.s[p.i]
	p.i++
	var b strings.Builder
	for !p.done() {
		c := p.s[p.i]
		p.i++
		switch {
		case c == q:
			return b.String(), nil
		case c == '\\':
			if p.done() {
				return "", p.errf("unfinished escape")
			}
			e := p.s[p.i]
			p.i++
			switch e {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case 'u':
				if p.i+4 > len(p.s) {
					return "", p.errf("bad \\u escape")
				}
				r, err := strconv.ParseUint(p.s[p.i:p.i+4], 16, 32)
				if err != nil {
					return "", p.errf("bad \\u escape")
				}
				b.WriteRune(rune(r))
				p.i += 4
			default:
				b.WriteByte(e)
			}
		default:
			b.WriteByte(c)
		}
	}
	return "", p.errf("unfinished string")
}

func (p *mongoParser) number() (any, error) {
	start := p.i
	for !p.done() && strings.IndexByte("+-.0123456789eE", p.s[p.i]) >= 0 {
		p.i++
	}
	f, err := strconv.ParseFloat(p.s[start:p.i], 64)
	if err != nil {
		return nil, p.errf("bad number %q", p.s[start:p.i])
	}
	return f, nil
}
