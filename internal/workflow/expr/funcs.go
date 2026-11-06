package expr

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
)

func (e *evaluator) call(c callNode) (any, error) {
	if c.fn == "has" {
		_, found := e.resolve(c.args[0].(pathNode))
		return found, nil
	}
	x, err := e.eval(c.args[0])
	if err != nil {
		return nil, err
	}
	switch c.fn {
	case "len":
		switch t := x.(type) {
		case nil:
			return float64(0), nil
		case string:
			return float64(utf8.RuneCountInString(t)), nil
		case []any:
			return float64(len(t)), nil
		case map[string]any:
			return float64(len(t)), nil
		}
		return nil, errAt(c.pos, "len needs a string, list or map (got %s)", kindOf(x))
	case "lower", "upper", "trim":
		if x == nil {
			return "", nil
		}
		s, ok := x.(string)
		if !ok {
			return nil, errAt(c.pos, "%s needs a string (got %s)", c.fn, kindOf(x))
		}
		switch c.fn {
		case "lower":
			s = strings.ToLower(s)
		case "upper":
			s = strings.ToUpper(s)
		default:
			s = strings.TrimSpace(s)
		}
		if len(s) > MaxStringLen {
			return nil, errAt(c.pos, "string is too long (limit %d bytes)", MaxStringLen)
		}
		return s, nil
	case "seen":
		if e.env == nil || e.env.Seen == nil {
			return nil, errAt(c.pos, "seen() is not available here")
		}
		var key string
		switch t := x.(type) {
		case string:
			key = t
		case float64:
			key = formatNumber(t)
		default:
			return nil, errAt(c.pos, "seen needs a string or number key (got %s)", kindOf(x))
		}
		ok, err := e.env.Seen(key)
		if err != nil {
			return nil, errAt(c.pos, "seen: %v", err)
		}
		return ok, nil
	case "days_since":
		if x == nil {
			return nil, nil
		}
		s, ok := x.(string)
		if !ok {
			return nil, errAt(c.pos, "days_since needs a date string like 2026-11-05 or an RFC 3339 time (got %s)", kindOf(x))
		}
		t, err := parseTime(s)
		if err != nil {
			return nil, errAt(c.pos, "days_since needs a date like 2026-11-05 or an RFC 3339 time")
		}
		now := time.Now
		if e.env != nil && e.env.Now != nil {
			now = e.env.Now
		}
		return now().Sub(t).Hours() / 24, nil
	case "json":
		b, err := json.Marshal(x)
		if err != nil {
			return nil, errAt(c.pos, "json: the value has no JSON form")
		}
		if len(b) > MaxStringLen {
			return nil, errAt(c.pos, "json: the text is too long (limit %d bytes)", MaxStringLen)
		}
		return string(b), nil
	}
	return nil, errAt(c.pos, "internal error: unknown function %q", c.fn)
}

func parseTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse("2006-01-02", s)
}
