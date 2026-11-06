package expr

import (
	"fmt"
	"strings"
)

// MaxTemplateLen caps one template string.
const MaxTemplateLen = 64 << 10

// part is one piece of a template: text, or an expression.
type part struct {
	text   string
	expr   string
	isExpr bool
	pos    int // byte offset of "{{"
}

// HasTemplate reports whether s holds a {{ … }} template.
func HasTemplate(s string) bool { return strings.Contains(s, "{{") }

// Check parses an expression without evaluating it.
func Check(src string) error {
	_, err := parse(src)
	return err
}

// CheckTemplate parses every {{ … }} in a template without evaluating.
func CheckTemplate(tmpl string) error {
	parts, err := split(tmpl)
	if err != nil {
		return err
	}
	for _, p := range parts {
		if p.isExpr {
			if err := Check(p.expr); err != nil {
				return tmplErr(p, err)
			}
		}
	}
	return nil
}

// Render fills every {{ … }} in tmpl and returns text. Values become
// text: strings as they are, null as "", lists and maps as JSON.
func Render(tmpl string, scope map[string]any, env *Env) (string, error) {
	parts, err := split(tmpl)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, p := range parts {
		if !p.isExpr {
			b.WriteString(p.text)
		} else {
			v, err := EvalEnv(p.expr, scope, env)
			if err != nil {
				return "", tmplErr(p, err)
			}
			b.WriteString(v.Text())
		}
		if b.Len() > MaxStringLen {
			return "", fmt.Errorf("template: the text is too long (limit %d bytes)", MaxStringLen)
		}
	}
	return b.String(), nil
}

// RenderValue is Render, but when the whole string is one {{ … }} it
// returns the value with its JSON type (number, bool, list, map, null).
func RenderValue(tmpl string, scope map[string]any, env *Env) (any, error) {
	parts, err := split(tmpl)
	if err != nil {
		return nil, err
	}
	if len(parts) == 1 && parts[0].isExpr {
		v, err := EvalEnv(parts[0].expr, scope, env)
		if err != nil {
			return nil, tmplErr(parts[0], err)
		}
		return v.Any(), nil
	}
	return Render(tmpl, scope, env)
}

// RenderJSON returns a copy of v (a JSON value such as step args) with
// every string rendered by RenderValue. Map keys are never templates.
func RenderJSON(v any, scope map[string]any, env *Env) (any, error) {
	return renderJSON(v, scope, env, 0)
}

func renderJSON(v any, scope map[string]any, env *Env, depth int) (any, error) {
	if depth > 1000 {
		return nil, fmt.Errorf("template: the value is nested too deep")
	}
	switch t := v.(type) {
	case string:
		return RenderValue(t, scope, env)
	case []any:
		out := make([]any, len(t))
		for i, el := range t {
			r, err := renderJSON(el, scope, env, depth+1)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, el := range t {
			r, err := renderJSON(el, scope, env, depth+1)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", clip(k), err)
			}
			out[k] = r
		}
		return out, nil
	}
	return v, nil
}

func clip(s string) string {
	if len(s) > 40 {
		return s[:40] + "…"
	}
	return s
}

func tmplErr(p part, err error) error {
	return fmt.Errorf("template {{ at col %d: %w", p.pos+1, err)
}

// split cuts tmpl into text and {{ … }} parts. Inside {{ … }} a "}}" in
// a quoted string does not close the template.
func split(tmpl string) ([]part, error) {
	if len(tmpl) > MaxTemplateLen {
		return nil, fmt.Errorf("template is too long (%d bytes, the limit is %d)", len(tmpl), MaxTemplateLen)
	}
	var parts []part
	i := 0
	for {
		open := strings.Index(tmpl[i:], "{{")
		if open < 0 {
			if i < len(tmpl) || len(parts) == 0 {
				parts = append(parts, part{text: tmpl[i:], pos: i})
			}
			return parts, nil
		}
		open += i
		if open > i {
			parts = append(parts, part{text: tmpl[i:open], pos: i})
		}
		end, err := closeAt(tmpl, open+2)
		if err != nil {
			return nil, fmt.Errorf("template {{ at col %d: %w", open+1, err)
		}
		parts = append(parts, part{expr: tmpl[open+2 : end], isExpr: true, pos: open})
		i = end + 2
	}
}

// closeAt finds the "}}" that ends the expression starting at i.
func closeAt(s string, i int) (int, error) {
	var quote byte
	for ; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '}' && i+1 < len(s) && s[i+1] == '}':
			return i, nil
		}
	}
	return 0, fmt.Errorf("{{ is not closed with }}")
}
