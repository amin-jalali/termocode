// Package grpcreq sends gRPC requests written in .http files (API-19):
// the target URL, a small .proto parser, server reflection, and a call
// that sends JSON messages and returns the replies as JSON.
package grpcreq

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// ParseProto reads one .proto source (proto2 or proto3) into a file
// descriptor named name. It reads what a client needs to call a
// service: packages, imports, messages (nested, maps, oneofs, proto3
// optional), enums and services. Options, reserved ranges, extensions
// and extend blocks are skipped; editions and groups are refused.
func ParseProto(name, src string) (*descriptorpb.FileDescriptorProto, error) {
	toks, err := lexProto(src)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	p := &protoParser{toks: toks, file: name}
	fd, err := p.parseFile()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	fd.Name = proto.String(name)
	return fd, nil
}

// token is one lexical token of a .proto file.
type token struct {
	kind byte // 'i' ident, 'n' number, 's' string, 'p' punctuation
	text string
	line int
}

func lexProto(src string) ([]token, error) {
	var out []token
	line := 1
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r' || c == '\f' || c == '\v':
			i++
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return nil, fmt.Errorf("line %d: unclosed /* comment", line)
			}
			line += strings.Count(src[i:i+2+end], "\n")
			i += end + 4
		case c == '"' || c == '\'':
			j := i + 1
			var b strings.Builder
			for j < len(src) && src[j] != c {
				if src[j] == '\n' {
					return nil, fmt.Errorf("line %d: unclosed string", line)
				}
				if src[j] == '\\' && j+1 < len(src) {
					j++
					switch src[j] {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					default:
						b.WriteByte(src[j])
					}
					j++
					continue
				}
				b.WriteByte(src[j])
				j++
			}
			if j >= len(src) {
				return nil, fmt.Errorf("line %d: unclosed string", line)
			}
			out = append(out, token{kind: 's', text: b.String(), line: line})
			i = j + 1
		case isIdentStart(c):
			j := i
			for j < len(src) && (isIdentStart(src[j]) || isDigit(src[j]) || src[j] == '.') {
				j++
			}
			out = append(out, token{kind: 'i', text: src[i:j], line: line})
			i = j
		case isDigit(c) || (c == '.' && i+1 < len(src) && isDigit(src[i+1])):
			j := i
			for j < len(src) && (isDigit(src[j]) || isIdentStart(src[j]) || src[j] == '.' ||
				((src[j] == '+' || src[j] == '-') && (src[j-1] == 'e' || src[j-1] == 'E'))) {
				j++
			}
			out = append(out, token{kind: 'n', text: src[i:j], line: line})
			i = j
		case c == '.' && i+1 < len(src) && isIdentStart(src[i+1]):
			// A fully qualified type name: .pkg.Msg
			j := i + 1
			for j < len(src) && (isIdentStart(src[j]) || isDigit(src[j]) || src[j] == '.') {
				j++
			}
			out = append(out, token{kind: 'i', text: src[i:j], line: line})
			i = j
		default:
			if c > unicode.MaxASCII {
				return nil, fmt.Errorf("line %d: unexpected character", line)
			}
			out = append(out, token{kind: 'p', text: string(c), line: line})
			i++
		}
	}
	return out, nil
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

type protoParser struct {
	toks   []token
	pos    int
	file   string
	proto3 bool
}

func (p *protoParser) peek() token {
	if p.pos < len(p.toks) {
		return p.toks[p.pos]
	}
	line := 0
	if n := len(p.toks); n > 0 {
		line = p.toks[n-1].line
	}
	return token{kind: 0, line: line}
}

func (p *protoParser) next() token {
	t := p.peek()
	if p.pos < len(p.toks) {
		p.pos++
	}
	return t
}

func (p *protoParser) errf(t token, format string, a ...any) error {
	return fmt.Errorf("line %d: %s", t.line, fmt.Sprintf(format, a...))
}

// is reports whether the next token is the punctuation or word s.
func (p *protoParser) is(s string) bool {
	t := p.peek()
	return t.kind != 's' && t.kind != 0 && t.text == s
}

func (p *protoParser) expect(s string) error {
	t := p.next()
	if t.kind == 's' || t.text != s {
		if t.kind == 0 {
			return p.errf(t, "expected %q, found the end of the file", s)
		}
		return p.errf(t, "expected %q, found %q", s, t.text)
	}
	return nil
}

func (p *protoParser) ident() (string, error) {
	t := p.next()
	if t.kind != 'i' {
		if t.kind == 0 {
			return "", p.errf(t, "expected a name, found the end of the file")
		}
		return "", p.errf(t, "expected a name, found %q", t.text)
	}
	return t.text, nil
}

func (p *protoParser) str() (string, error) {
	t := p.next()
	if t.kind != 's' {
		return "", p.errf(t, "expected a quoted string, found %q", t.text)
	}
	s := t.text
	for p.peek().kind == 's' { // "a" "b" joins
		s += p.next().text
	}
	return s, nil
}

func (p *protoParser) number() (int64, error) {
	t := p.next()
	neg := false
	if t.kind == 'p' && t.text == "-" {
		neg = true
		t = p.next()
	}
	if t.kind != 'n' {
		return 0, p.errf(t, "expected a number, found %q", t.text)
	}
	v, err := strconv.ParseInt(t.text, 0, 64)
	if err != nil {
		return 0, p.errf(t, "bad number %q", t.text)
	}
	if neg {
		v = -v
	}
	return v, nil
}

// skipStatement skips up to and including the next ';' at this level,
// or a balanced { } block when one comes first.
func (p *protoParser) skipStatement() error {
	depth := 0
	for {
		t := p.next()
		switch {
		case t.kind == 0:
			return p.errf(t, "unexpected end of the file")
		case t.kind == 'p' && (t.text == "{" || t.text == "[" || t.text == "("):
			depth++
		case t.kind == 'p' && (t.text == "}" || t.text == "]" || t.text == ")"):
			depth--
			if depth == 0 && t.text == "}" {
				if p.is(";") {
					p.next()
				}
				return nil
			}
			if depth < 0 {
				return p.errf(t, "unexpected %q", t.text)
			}
		case t.kind == 'p' && t.text == ";" && depth == 0:
			return nil
		}
	}
}

// skipFieldOptions skips a `[ … ]` list after a field or enum value.
func (p *protoParser) skipFieldOptions() error {
	if !p.is("[") {
		return nil
	}
	depth := 0
	for {
		t := p.next()
		switch {
		case t.kind == 0:
			return p.errf(t, "unclosed [")
		case t.kind == 'p' && (t.text == "[" || t.text == "{"):
			depth++
		case t.kind == 'p' && (t.text == "]" || t.text == "}"):
			depth--
			if depth == 0 {
				return nil
			}
		}
	}
}

func (p *protoParser) parseFile() (*descriptorpb.FileDescriptorProto, error) {
	fd := &descriptorpb.FileDescriptorProto{}
	if p.is("syntax") || p.is("edition") {
		kw := p.next()
		if err := p.expect("="); err != nil {
			return nil, err
		}
		v, err := p.str()
		if err != nil {
			return nil, err
		}
		if kw.text == "edition" {
			return nil, p.errf(kw, "protobuf editions are not supported yet (use syntax = \"proto3\")")
		}
		switch v {
		case "proto3":
			p.proto3 = true
			fd.Syntax = proto.String("proto3")
		case "proto2":
			fd.Syntax = proto.String("proto2")
		default:
			return nil, p.errf(kw, "unknown syntax %q", v)
		}
		if err := p.expect(";"); err != nil {
			return nil, err
		}
	}
	for p.peek().kind != 0 {
		t := p.peek()
		switch {
		case p.is(";"):
			p.next()
		case p.is("package"):
			p.next()
			name, err := p.ident()
			if err != nil {
				return nil, err
			}
			fd.Package = proto.String(name)
			if err := p.expect(";"); err != nil {
				return nil, err
			}
		case p.is("import"):
			p.next()
			public := false
			if p.is("public") {
				p.next()
				public = true
			} else if p.is("weak") {
				p.next()
			}
			path, err := p.str()
			if err != nil {
				return nil, err
			}
			if public {
				fd.PublicDependency = append(fd.PublicDependency, int32(len(fd.Dependency)))
			}
			fd.Dependency = append(fd.Dependency, path)
			if err := p.expect(";"); err != nil {
				return nil, err
			}
		case p.is("option"), p.is("extend"):
			if err := p.skipStatement(); err != nil {
				return nil, err
			}
		case p.is("message"):
			p.next()
			m, err := p.parseMessage()
			if err != nil {
				return nil, err
			}
			fd.MessageType = append(fd.MessageType, m)
		case p.is("enum"):
			p.next()
			e, err := p.parseEnum()
			if err != nil {
				return nil, err
			}
			fd.EnumType = append(fd.EnumType, e)
		case p.is("service"):
			p.next()
			s, err := p.parseService()
			if err != nil {
				return nil, err
			}
			fd.Service = append(fd.Service, s)
		default:
			return nil, p.errf(t, "unexpected %q", t.text)
		}
	}
	return fd, nil
}

var scalarTypes = map[string]descriptorpb.FieldDescriptorProto_Type{
	"double":   descriptorpb.FieldDescriptorProto_TYPE_DOUBLE,
	"float":    descriptorpb.FieldDescriptorProto_TYPE_FLOAT,
	"int32":    descriptorpb.FieldDescriptorProto_TYPE_INT32,
	"int64":    descriptorpb.FieldDescriptorProto_TYPE_INT64,
	"uint32":   descriptorpb.FieldDescriptorProto_TYPE_UINT32,
	"uint64":   descriptorpb.FieldDescriptorProto_TYPE_UINT64,
	"sint32":   descriptorpb.FieldDescriptorProto_TYPE_SINT32,
	"sint64":   descriptorpb.FieldDescriptorProto_TYPE_SINT64,
	"fixed32":  descriptorpb.FieldDescriptorProto_TYPE_FIXED32,
	"fixed64":  descriptorpb.FieldDescriptorProto_TYPE_FIXED64,
	"sfixed32": descriptorpb.FieldDescriptorProto_TYPE_SFIXED32,
	"sfixed64": descriptorpb.FieldDescriptorProto_TYPE_SFIXED64,
	"bool":     descriptorpb.FieldDescriptorProto_TYPE_BOOL,
	"string":   descriptorpb.FieldDescriptorProto_TYPE_STRING,
	"bytes":    descriptorpb.FieldDescriptorProto_TYPE_BYTES,
}

// setType fills the type of f: a scalar, or a type name the descriptor
// builder resolves later (message or enum).
func setType(f *descriptorpb.FieldDescriptorProto, typ string) {
	if st, ok := scalarTypes[typ]; ok {
		f.Type = st.Enum()
		return
	}
	f.TypeName = proto.String(typ)
}

// mapEntryName is protoc's name for the entry message of a map field:
// "tag_counts" → "TagCountsEntry".
func mapEntryName(field string) string {
	var b strings.Builder
	up := true
	for _, r := range field {
		if r == '_' {
			up = true
			continue
		}
		if up {
			b.WriteRune(unicode.ToUpper(r))
			up = false
			continue
		}
		b.WriteRune(r)
	}
	return b.String() + "Entry"
}

func (p *protoParser) parseMessage() (*descriptorpb.DescriptorProto, error) {
	name, err := p.ident()
	if err != nil {
		return nil, err
	}
	m := &descriptorpb.DescriptorProto{Name: proto.String(name)}
	if err := p.expect("{"); err != nil {
		return nil, err
	}
	var synthetic []*descriptorpb.FieldDescriptorProto // proto3 optional fields
	for !p.is("}") {
		t := p.peek()
		switch {
		case t.kind == 0:
			return nil, p.errf(t, "message %s is not closed with }", name)
		case p.is(";"):
			p.next()
		case p.is("option"), p.is("reserved"), p.is("extensions"), p.is("extend"):
			if err := p.skipStatement(); err != nil {
				return nil, err
			}
		case p.is("message"):
			p.next()
			nm, err := p.parseMessage()
			if err != nil {
				return nil, err
			}
			m.NestedType = append(m.NestedType, nm)
		case p.is("enum"):
			p.next()
			e, err := p.parseEnum()
			if err != nil {
				return nil, err
			}
			m.EnumType = append(m.EnumType, e)
		case p.is("oneof"):
			p.next()
			if err := p.parseOneof(m); err != nil {
				return nil, err
			}
		case p.is("map") && p.pos+1 < len(p.toks) && p.toks[p.pos+1].text == "<":
			if err := p.parseMapField(m); err != nil {
				return nil, err
			}
		case p.is("group"):
			return nil, p.errf(t, "proto2 groups are not supported")
		default:
			f, optional, err := p.parseField()
			if err != nil {
				return nil, err
			}
			m.Field = append(m.Field, f)
			if optional && p.proto3 {
				synthetic = append(synthetic, f)
			}
		}
	}
	p.next() // }
	for _, f := range synthetic {
		f.Proto3Optional = proto.Bool(true)
		f.OneofIndex = proto.Int32(int32(len(m.OneofDecl)))
		m.OneofDecl = append(m.OneofDecl, &descriptorpb.OneofDescriptorProto{Name: proto.String("_" + f.GetName())})
	}
	return m, nil
}

// parseField reads `[label] type name = number [opts];`. optional
// reports an explicit `optional` label.
func (p *protoParser) parseField() (*descriptorpb.FieldDescriptorProto, bool, error) {
	f := &descriptorpb.FieldDescriptorProto{Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()}
	optional := false
	switch {
	case p.is("repeated"):
		p.next()
		f.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	case p.is("optional"):
		p.next()
		optional = true
	case p.is("required"):
		t := p.next()
		if p.proto3 {
			return nil, false, p.errf(t, "required fields are not allowed in proto3")
		}
		f.Label = descriptorpb.FieldDescriptorProto_LABEL_REQUIRED.Enum()
	}
	typ, err := p.ident()
	if err != nil {
		return nil, false, err
	}
	name, err := p.ident()
	if err != nil {
		return nil, false, err
	}
	if err := p.expect("="); err != nil {
		return nil, false, err
	}
	n, err := p.number()
	if err != nil {
		return nil, false, err
	}
	setType(f, typ)
	f.Name = proto.String(name)
	f.Number = proto.Int32(int32(n))
	if err := p.skipFieldOptions(); err != nil {
		return nil, false, err
	}
	if err := p.expect(";"); err != nil {
		return nil, false, err
	}
	return f, optional, nil
}

func (p *protoParser) parseMapField(m *descriptorpb.DescriptorProto) error {
	p.next() // map
	if err := p.expect("<"); err != nil {
		return err
	}
	kt, err := p.ident()
	if err != nil {
		return err
	}
	if err := p.expect(","); err != nil {
		return err
	}
	vt, err := p.ident()
	if err != nil {
		return err
	}
	if err := p.expect(">"); err != nil {
		return err
	}
	name, err := p.ident()
	if err != nil {
		return err
	}
	if err := p.expect("="); err != nil {
		return err
	}
	n, err := p.number()
	if err != nil {
		return err
	}
	if err := p.skipFieldOptions(); err != nil {
		return err
	}
	if err := p.expect(";"); err != nil {
		return err
	}
	entry := mapEntryName(name)
	key := &descriptorpb.FieldDescriptorProto{Name: proto.String("key"), Number: proto.Int32(1), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()}
	setType(key, kt)
	val := &descriptorpb.FieldDescriptorProto{Name: proto.String("value"), Number: proto.Int32(2), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()}
	setType(val, vt)
	m.NestedType = append(m.NestedType, &descriptorpb.DescriptorProto{
		Name:    proto.String(entry),
		Field:   []*descriptorpb.FieldDescriptorProto{key, val},
		Options: &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)},
	})
	m.Field = append(m.Field, &descriptorpb.FieldDescriptorProto{
		Name:     proto.String(name),
		Number:   proto.Int32(int32(n)),
		Label:    descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(),
		Type:     descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(),
		TypeName: proto.String(entry),
	})
	return nil
}

func (p *protoParser) parseOneof(m *descriptorpb.DescriptorProto) error {
	name, err := p.ident()
	if err != nil {
		return err
	}
	if err := p.expect("{"); err != nil {
		return err
	}
	idx := int32(len(m.OneofDecl))
	m.OneofDecl = append(m.OneofDecl, &descriptorpb.OneofDescriptorProto{Name: proto.String(name)})
	for !p.is("}") {
		t := p.peek()
		switch {
		case t.kind == 0:
			return p.errf(t, "oneof %s is not closed with }", name)
		case p.is(";"):
			p.next()
		case p.is("option"):
			if err := p.skipStatement(); err != nil {
				return err
			}
		default:
			f, _, err := p.parseField()
			if err != nil {
				return err
			}
			f.OneofIndex = proto.Int32(idx)
			m.Field = append(m.Field, f)
		}
	}
	p.next()
	return nil
}

func (p *protoParser) parseEnum() (*descriptorpb.EnumDescriptorProto, error) {
	name, err := p.ident()
	if err != nil {
		return nil, err
	}
	e := &descriptorpb.EnumDescriptorProto{Name: proto.String(name)}
	if err := p.expect("{"); err != nil {
		return nil, err
	}
	for !p.is("}") {
		t := p.peek()
		switch {
		case t.kind == 0:
			return nil, p.errf(t, "enum %s is not closed with }", name)
		case p.is(";"):
			p.next()
		case p.is("option"):
			start := p.pos
			p.next()
			if p.is("allow_alias") {
				p.next()
				if err := p.expect("="); err != nil {
					return nil, err
				}
				v := p.next()
				if v.text == "true" {
					e.Options = &descriptorpb.EnumOptions{AllowAlias: proto.Bool(true)}
				}
				if err := p.expect(";"); err != nil {
					return nil, err
				}
				continue
			}
			p.pos = start
			if err := p.skipStatement(); err != nil {
				return nil, err
			}
		case p.is("reserved"):
			if err := p.skipStatement(); err != nil {
				return nil, err
			}
		default:
			vname, err := p.ident()
			if err != nil {
				return nil, err
			}
			if err := p.expect("="); err != nil {
				return nil, err
			}
			n, err := p.number()
			if err != nil {
				return nil, err
			}
			if err := p.skipFieldOptions(); err != nil {
				return nil, err
			}
			if err := p.expect(";"); err != nil {
				return nil, err
			}
			e.Value = append(e.Value, &descriptorpb.EnumValueDescriptorProto{Name: proto.String(vname), Number: proto.Int32(int32(n))})
		}
	}
	p.next()
	return e, nil
}

func (p *protoParser) parseService() (*descriptorpb.ServiceDescriptorProto, error) {
	name, err := p.ident()
	if err != nil {
		return nil, err
	}
	s := &descriptorpb.ServiceDescriptorProto{Name: proto.String(name)}
	if err := p.expect("{"); err != nil {
		return nil, err
	}
	for !p.is("}") {
		t := p.peek()
		switch {
		case t.kind == 0:
			return nil, p.errf(t, "service %s is not closed with }", name)
		case p.is(";"):
			p.next()
		case p.is("option"):
			if err := p.skipStatement(); err != nil {
				return nil, err
			}
		case p.is("rpc"):
			p.next()
			m, err := p.parseRPC()
			if err != nil {
				return nil, err
			}
			s.Method = append(s.Method, m)
		default:
			return nil, p.errf(t, "unexpected %q in service %s", t.text, name)
		}
	}
	p.next()
	return s, nil
}

func (p *protoParser) parseRPC() (*descriptorpb.MethodDescriptorProto, error) {
	name, err := p.ident()
	if err != nil {
		return nil, err
	}
	m := &descriptorpb.MethodDescriptorProto{Name: proto.String(name)}
	side := func(stream **bool) (string, error) {
		if err := p.expect("("); err != nil {
			return "", err
		}
		// "stream" is a type name only when no other name follows it.
		if p.is("stream") && p.pos+1 < len(p.toks) && p.toks[p.pos+1].kind == 'i' {
			p.next()
			*stream = proto.Bool(true)
		}
		typ, err := p.ident()
		if err != nil {
			return "", err
		}
		return typ, p.expect(")")
	}
	in, err := side(&m.ClientStreaming)
	if err != nil {
		return nil, err
	}
	if err := p.expect("returns"); err != nil {
		return nil, err
	}
	out, err := side(&m.ServerStreaming)
	if err != nil {
		return nil, err
	}
	m.InputType = proto.String(in)
	m.OutputType = proto.String(out)
	if p.is("{") { // rpc options block
		if err := p.skipStatement(); err != nil {
			return nil, err
		}
		return m, nil
	}
	return m, p.expect(";")
}
