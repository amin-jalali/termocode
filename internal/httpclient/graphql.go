package httpclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// GraphQL support for `.http` requests: the introspection request, the
// schema it answers with, field completion at the cursor, a field check
// of a query and a text docs view per type. It only reads text; it
// sends nothing itself.

// gqlIntrospectionQuery asks for every type with its fields, args,
// input fields and enum values (type refs four levels deep, enough for
// [T!]! and friends).
const gqlIntrospectionQuery = `query IntrospectionQuery { __schema { queryType { name } mutationType { name } subscriptionType { name } types { kind name description fields(includeDeprecated: true) { name description args { name description type { ...TypeRef } } type { ...TypeRef } } inputFields { name description type { ...TypeRef } } enumValues(includeDeprecated: true) { name description } possibleTypes { name } } } } fragment TypeRef on __Type { kind name ofType { kind name ofType { kind name ofType { kind name ofType { kind name } } } } }`

// GQLIntrospectionBody is the JSON body of an introspection POST.
func GQLIntrospectionBody() string {
	b, _ := json.Marshal(map[string]string{"query": gqlIntrospectionQuery})
	return string(b)
}

// GQLSchema is a schema read from an introspection answer.
type GQLSchema struct {
	Query, Mutation, Subscription string
	Types                         map[string]*GQLType
}

// GQLType is one named type.
type GQLType struct {
	Kind        string
	Name        string
	Description string
	Fields      []GQLField // object/interface fields, or input fields
	EnumValues  []string
	Possible    []string // union / interface members
}

// GQLField is one field (or arg / input field). Type is printed in
// GraphQL form ("[User!]!"); Named is the bare type name ("User").
type GQLField struct {
	Name        string
	Description string
	Type        string
	Named       string
	Args        []GQLField
}

// Field returns the field called name, or a zero field.
func (t *GQLType) Field(name string) GQLField {
	if t != nil {
		for _, f := range t.Fields {
			if f.Name == name {
				return f
			}
		}
	}
	return GQLField{}
}

func (t *GQLType) has(name string) bool {
	if t == nil {
		return false
	}
	for _, f := range t.Fields {
		if f.Name == name {
			return true
		}
	}
	return false
}

type gqlTypeRef struct {
	Kind   string      `json:"kind"`
	Name   *string     `json:"name"`
	OfType *gqlTypeRef `json:"ofType"`
}

type gqlRawField struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Args        []gqlRawField `json:"args"`
	Type        *gqlTypeRef   `json:"type"`
}

type gqlNamed struct {
	Name string `json:"name"`
}

type gqlRawType struct {
	Kind          string        `json:"kind"`
	Name          string        `json:"name"`
	Description   string        `json:"description"`
	Fields        []gqlRawField `json:"fields"`
	InputFields   []gqlRawField `json:"inputFields"`
	EnumValues    []gqlNamed    `json:"enumValues"`
	PossibleTypes []gqlNamed    `json:"possibleTypes"`
}

// ParseGQLSchema reads an introspection answer ({"data":{"__schema":…}}).
func ParseGQLSchema(body []byte) (*GQLSchema, error) {
	var ans struct {
		Data *struct {
			Schema *struct {
				QueryType        *gqlNamed    `json:"queryType"`
				MutationType     *gqlNamed    `json:"mutationType"`
				SubscriptionType *gqlNamed    `json:"subscriptionType"`
				Types            []gqlRawType `json:"types"`
			} `json:"__schema"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &ans); err != nil {
		return nil, fmt.Errorf("the answer is not JSON: %v", err)
	}
	if len(ans.Errors) > 0 {
		msgs := make([]string, 0, len(ans.Errors))
		for _, e := range ans.Errors {
			msgs = append(msgs, e.Message)
		}
		return nil, errors.New("the server said: " + strings.Join(msgs, "; "))
	}
	if ans.Data == nil || ans.Data.Schema == nil {
		return nil, errors.New("no schema in the answer")
	}
	raw := ans.Data.Schema
	s := &GQLSchema{Types: make(map[string]*GQLType, len(raw.Types))}
	if raw.QueryType != nil {
		s.Query = raw.QueryType.Name
	}
	if raw.MutationType != nil {
		s.Mutation = raw.MutationType.Name
	}
	if raw.SubscriptionType != nil {
		s.Subscription = raw.SubscriptionType.Name
	}
	for _, rt := range raw.Types {
		if rt.Name == "" {
			continue
		}
		t := &GQLType{Kind: rt.Kind, Name: rt.Name, Description: rt.Description}
		fields := rt.Fields
		if rt.Kind == "INPUT_OBJECT" {
			fields = rt.InputFields
		}
		for _, f := range fields {
			t.Fields = append(t.Fields, convField(f))
		}
		for _, v := range rt.EnumValues {
			t.EnumValues = append(t.EnumValues, v.Name)
		}
		for _, v := range rt.PossibleTypes {
			t.Possible = append(t.Possible, v.Name)
		}
		s.Types[t.Name] = t
	}
	return s, nil
}

func convField(f gqlRawField) GQLField {
	out := GQLField{Name: f.Name, Description: f.Description, Type: typeString(f.Type, 0), Named: namedType(f.Type)}
	for _, a := range f.Args {
		out.Args = append(out.Args, convField(a))
	}
	return out
}

func typeString(r *gqlTypeRef, depth int) string {
	if r == nil || depth > 16 {
		return "?"
	}
	switch r.Kind {
	case "NON_NULL":
		return typeString(r.OfType, depth+1) + "!"
	case "LIST":
		return "[" + typeString(r.OfType, depth+1) + "]"
	}
	if r.Name == nil {
		return "?"
	}
	return *r.Name
}

func namedType(r *gqlTypeRef) string {
	for i := 0; r != nil && i < 16; i++ {
		if r.Name != nil && r.Kind != "NON_NULL" && r.Kind != "LIST" {
			return *r.Name
		}
		r = r.OfType
	}
	return ""
}

// gqlTok is one token: a name, a punctuator ({ } ( ) : ... @ $ etc.),
// or "" for a string / number value.
type gqlTok struct {
	text string
	pos  int
}

func gqlTokens(src string) []gqlTok {
	var out []gqlTok
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == ',':
			i++
		case c == '#':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '"':
			start := i
			if strings.HasPrefix(src[i:], `"""`) {
				end := strings.Index(src[i+3:], `"""`)
				if end < 0 {
					i = len(src)
				} else {
					i += end + 6
				}
			} else {
				i++
				for i < len(src) && src[i] != '"' && src[i] != '\n' {
					if src[i] == '\\' {
						i++
					}
					i++
				}
				i++
			}
			out = append(out, gqlTok{text: "", pos: start})
		case strings.HasPrefix(src[i:], "..."):
			out = append(out, gqlTok{text: "...", pos: i})
			i += 3
		case isNameStart(c):
			start := i
			for i < len(src) && (isNameStart(src[i]) || (src[i] >= '0' && src[i] <= '9')) {
				i++
			}
			out = append(out, gqlTok{text: src[start:i], pos: start})
		case c == '-' || (c >= '0' && c <= '9'):
			start := i
			i++
			for i < len(src) && (src[i] == '.' || src[i] == 'e' || src[i] == 'E' || src[i] == '+' || src[i] == '-' || (src[i] >= '0' && src[i] <= '9')) {
				i++
			}
			out = append(out, gqlTok{text: "", pos: start})
		default:
			out = append(out, gqlTok{text: string(c), pos: i})
			i++
		}
	}
	return out
}

func isNameStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isGQLName(s string) bool { return s != "" && isNameStart(s[0]) }

// gqlWalk runs over a query and calls field for each selected field
// with the type it is selected on. It returns the type stack at the end
// and the brace errors.
func (s *GQLSchema) gqlWalk(src string, field func(on *GQLType, name string)) (stack []string, errs []string) {
	toks := gqlTokens(src)
	var head []string // names at depth 0 since the last block
	last := ""        // last field at this depth
	for i := 0; i < len(toks); i++ {
		t := toks[i].text
		switch t {
		case "(":
			depth := 1
			for i++; i < len(toks) && depth > 0; i++ {
				switch toks[i].text {
				case "(":
					depth++
				case ")":
					depth--
				}
			}
			i--
		case "{":
			if len(stack) == 0 {
				stack = append(stack, s.rootFor(head))
				head = nil
			} else {
				next := ""
				if on := s.Types[stack[len(stack)-1]]; on != nil {
					next = on.Field(last).Named
				}
				stack = append(stack, next)
			}
			last = ""
		case "}":
			if len(stack) == 0 {
				errs = append(errs, fmt.Sprintf("extra } at offset %d", toks[i].pos))
				continue
			}
			stack = stack[:len(stack)-1]
			last = ""
		case "...":
			// "...Name" (spread) or "... on Type {" (inline fragment).
			if i+2 < len(toks) && toks[i+1].text == "on" && isGQLName(toks[i+2].text) {
				last = ""
				if i+3 < len(toks) && toks[i+3].text == "{" {
					stack = append(stack, toks[i+2].text)
					i += 3
					continue
				}
				i += 2
			} else if i+1 < len(toks) && isGQLName(toks[i+1].text) {
				i++
			}
		case "@", "$":
			if i+1 < len(toks) && isGQLName(toks[i+1].text) {
				i++
			}
		default:
			if !isGQLName(t) {
				continue
			}
			if len(stack) == 0 {
				head = append(head, t)
				continue
			}
			if i+1 < len(toks) && toks[i+1].text == ":" {
				i++ // an alias; the field name follows
				continue
			}
			last = t
			if field != nil {
				field(s.Types[stack[len(stack)-1]], t)
			}
		}
	}
	return stack, errs
}

// rootFor picks the type of a top-level block from the words before it
// ("query X", "mutation", "fragment F on T", or nothing).
func (s *GQLSchema) rootFor(head []string) string {
	for i, w := range head {
		switch w {
		case "mutation":
			return s.Mutation
		case "subscription":
			return s.Subscription
		case "query":
			return s.Query
		case "fragment":
			for j := i + 1; j+1 < len(head); j++ {
				if head[j] == "on" {
					return head[j+1]
				}
			}
			return ""
		}
	}
	return s.Query
}

// Complete returns the fields that fit at the end of src (the text up
// to the cursor), sorted by name; a word right before the cursor
// filters them by prefix.
func (s *GQLSchema) Complete(src string) []GQLField {
	prefix := ""
	if n := len(src); n > 0 && (isNameStart(src[n-1]) || (src[n-1] >= '0' && src[n-1] <= '9')) {
		k := n
		for k > 0 && (isNameStart(src[k-1]) || (src[k-1] >= '0' && src[k-1] <= '9')) {
			k--
		}
		prefix, src = src[k:], src[:k]
	}
	stack, _ := s.gqlWalk(src, nil)
	if len(stack) == 0 {
		return nil
	}
	t := s.Types[stack[len(stack)-1]]
	if t == nil {
		return nil
	}
	var out []GQLField
	for _, f := range t.Fields {
		if strings.HasPrefix(f.Name, prefix) {
			out = append(out, f)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Validate checks that every selected field exists on its type and that
// the braces match. Fields on unknown types are not checked.
func (s *GQLSchema) Validate(src string) []string {
	var errs []string
	stack, braceErrs := s.gqlWalk(src, func(on *GQLType, name string) {
		if on == nil || name == "__typename" || on.has(name) {
			return
		}
		errs = append(errs, fmt.Sprintf("no field %q on %s", name, on.Name))
	})
	errs = append(errs, braceErrs...)
	if len(stack) > 0 {
		errs = append(errs, fmt.Sprintf("%d { not closed", len(stack)))
	}
	return errs
}

// Docs is a type in GraphQL SDL form, with descriptions as # comments,
// or "" for an unknown type.
func (s *GQLSchema) Docs(name string) string {
	t := s.Types[name]
	if t == nil {
		return ""
	}
	var b strings.Builder
	if t.Description != "" {
		b.WriteString("# " + t.Description + "\n")
	}
	switch t.Kind {
	case "SCALAR":
		b.WriteString("scalar " + t.Name + "\n")
		return b.String()
	case "UNION":
		b.WriteString("union " + t.Name + " = " + strings.Join(t.Possible, " | ") + "\n")
		return b.String()
	case "ENUM":
		b.WriteString("enum " + t.Name + " {\n")
		for _, v := range t.EnumValues {
			b.WriteString("  " + v + "\n")
		}
		b.WriteString("}\n")
		return b.String()
	case "INPUT_OBJECT":
		b.WriteString("input ")
	case "INTERFACE":
		b.WriteString("interface ")
	default:
		b.WriteString("type ")
	}
	b.WriteString(t.Name + " {\n")
	for _, f := range t.Fields {
		b.WriteString("  " + f.Name)
		if len(f.Args) > 0 {
			args := make([]string, 0, len(f.Args))
			for _, a := range f.Args {
				args = append(args, a.Name+": "+a.Type)
			}
			b.WriteString("(" + strings.Join(args, ", ") + ")")
		}
		b.WriteString(": " + f.Type)
		if f.Description != "" {
			b.WriteString("  # " + f.Description)
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return b.String()
}
