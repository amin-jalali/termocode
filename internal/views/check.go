package views

import (
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/blocks"
)

// LooksSecret reports whether an arg value looks like a key or a token.
// Such a value is refused on the phone ("type it on the laptop"). The
// hub may swap in the AI's fuller detector at start; the default catches
// the common prefixes and long opaque runs.
var LooksSecret = func(v string) bool { return secretRe.MatchString(v) || opaqueRun(v) }

// opaqueRun reports a run of 40 or more key-like characters that mixes
// digits, upper and lower case: how generated keys look, unlike names,
// branches and paths.
func opaqueRun(v string) bool {
	run, digit, upper, lower := 0, false, false, false
	for i := 0; i <= len(v); i++ {
		c := byte(0)
		if i < len(v) {
			c = v[i]
		}
		switch {
		case c >= '0' && c <= '9':
			digit = true
		case c >= 'A' && c <= 'Z':
			upper = true
		case c >= 'a' && c <= 'z':
			lower = true
		case c == '+' || c == '=' || c == '_':
		default:
			if run >= 40 && digit && upper && lower {
				return true
			}
			run, digit, upper, lower = 0, false, false, false
			continue
		}
		run++
	}
	return false
}

var (
	secretRe = regexp.MustCompile(`(?i)(^|[^a-z0-9])((ghp_|gho_|ghs_|ghu_|github_pat_|glpat-|sk-|sk_live_|rk_live_|xox[abpr]-)[a-z0-9_\-]{16,}|akia[0-9a-z]{16}|aiza[0-9a-z_\-]{30,})|-----begin|\$\{secret:`)
	nameRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.\-]{0,127}$`)
	intRe    = regexp.MustCompile(`^-?[0-9]{1,18}$`)
	uriRe    = regexp.MustCompile(`^[a-z][a-z0-9+.\-]*:[^\s]{1,400}$`)
)

// condKinds says on which field kinds each filter op works.
var condKinds = map[string]map[string]bool{
	"eq":        nil, // nil: every kind
	"ne":        nil,
	"in":        nil,
	"empty":     nil,
	"not_empty": nil,
	"gt":        setOf(FieldNum, FieldTime, FieldCost),
	"lt":        setOf(FieldNum, FieldTime, FieldCost),
	"ge":        setOf(FieldNum, FieldTime, FieldCost),
	"le":        setOf(FieldNum, FieldTime, FieldCost),
	"contains":  setOf(FieldText, FieldPath, FieldLink, FieldMD, FieldState),
	"starts":    setOf(FieldText, FieldPath, FieldLink, FieldMD, FieldState),
}

func fieldIDs(d *Def, keep func(Field) bool) string {
	var ids []string
	for _, f := range d.Fields {
		if keep == nil || keep(f) {
			ids = append(ids, f.ID)
		}
	}
	if len(ids) == 0 {
		return "none"
	}
	return strings.Join(ids, ", ")
}

func paramIDs(d *Def) string {
	if len(d.Params) == 0 {
		return "none"
	}
	ids := make([]string, len(d.Params))
	for i, p := range d.Params {
		ids[i] = p.ID
	}
	return strings.Join(ids, ", ")
}

// CheckSpec checks a spec against its view: the shape (blocks), then
// every arg, field, filter, sort key, group, layout and refresh against
// what the def allows. An unknown id is refused with the allowed ids.
func CheckSpec(d *Def, s *blocks.ViewSpec) error {
	if err := s.Validate(); err != nil {
		return err
	}
	if s.View != d.ID {
		return invalid("spec is for view %q, not %q", s.View, d.ID)
	}
	for k, v := range s.Args {
		p, ok := d.Param(k)
		if !ok {
			return invalid("view %s has no param %q (params: %s)", d.ID, k, paramIDs(d))
		}
		if err := checkArg(p, v); err != nil {
			return err
		}
	}
	for _, p := range d.Params {
		if p.Required && s.Args[p.ID] == "" && d.Default.Args[p.ID] == "" && p.Default == "" {
			return invalid("view %s needs param %q", d.ID, p.ID)
		}
	}
	seen := map[string]bool{}
	for _, id := range s.Fields {
		if _, ok := d.Field(id); !ok {
			return invalid("view %s has no field %q (fields: %s)", d.ID, id, fieldIDs(d, nil))
		}
		if seen[id] {
			return invalid("field %q named twice", id)
		}
		seen[id] = true
	}
	layout := s.Layout
	if layout == "" {
		layout = d.Layouts[0]
	}
	if !slices.Contains(d.Layouts, layout) {
		return invalid("view %s has no layout %q (layouts: %s)", d.ID, layout, strings.Join(d.Layouts, ", "))
	}
	if layout == blocks.KindTable && len(s.Fields) > MaxTableFields {
		return invalid("a table shows at most %d fields, the spec names %d", MaxTableFields, len(s.Fields))
	}
	for _, c := range s.Filter {
		f, ok := d.Field(c.Field)
		if !ok || !f.Filterable {
			return invalid("view %s cannot filter by %q (filterable: %s)", d.ID, c.Field, fieldIDs(d, func(f Field) bool { return f.Filterable }))
		}
		if kinds := condKinds[c.Op]; kinds != nil && !kinds[f.Kind] {
			return invalid("filter %s: %s does not work on a %s field", c.Field, c.Op, f.Kind)
		}
		vals := c.In
		if c.Op != "in" && c.Op != "empty" && c.Op != "not_empty" {
			vals = []string{c.Value}
		}
		if c.Op == "empty" || c.Op == "not_empty" {
			if c.Value != "" {
				return invalid("filter %s: %s takes no value", c.Field, c.Op)
			}
		}
		for _, v := range vals {
			if _, err := parseValue(f.Kind, v, 0); err != nil {
				return invalid("filter %s: %v", c.Field, err)
			}
		}
	}
	seen = map[string]bool{}
	for _, o := range s.Sort {
		f, ok := d.Field(o.Field)
		if !ok || !f.Sortable {
			return invalid("view %s cannot sort by %q (sortable: %s)", d.ID, o.Field, fieldIDs(d, func(f Field) bool { return f.Sortable }))
		}
		if seen[o.Field] {
			return invalid("sort key %q named twice", o.Field)
		}
		seen[o.Field] = true
	}
	if s.Group != "" {
		f, ok := d.Field(s.Group)
		if !ok {
			return invalid("view %s cannot group by %q (fields: %s)", d.ID, s.Group, fieldIDs(d, nil))
		}
		if f.Kind == FieldMD || f.Kind == FieldNum || f.Kind == FieldCost {
			return invalid("view %s cannot group by a %s field (%s)", d.ID, f.Kind, f.ID)
		}
		if layout != blocks.KindTable && layout != blocks.KindList {
			return invalid("only a table or a list is grouped, not a %s", layout)
		}
	}
	if s.Refresh != 0 && d.Live {
		return invalid("view %s is live; refresh is for op-backed views", d.ID)
	}
	return nil
}

// checkArg checks one arg value against its param kind. No arg is ever a
// secret, an absolute path, a URL or more than one line.
func checkArg(p Param, v string) error {
	if strings.ContainsAny(v, "\n\r\x00") {
		return invalid("param %s: one line only", p.ID)
	}
	if v != "" && LooksSecret(v) {
		return invalid("param %s looks like a secret; type it on the laptop", p.ID)
	}
	if v == "" {
		return nil
	}
	switch p.Kind {
	case ParamInt:
		if !intRe.MatchString(v) {
			return invalid("param %s: %q is not a whole number", p.ID, v)
		}
	case ParamNumberPR:
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return invalid("param %s: %q is not a pull request number", p.ID, v)
		}
	case ParamEnum:
		if !slices.Contains(p.Enum, v) {
			return invalid("param %s: %q is not one of %s", p.ID, v, strings.Join(p.Enum, ", "))
		}
	case ParamWS, ParamTable, ParamConn:
		if !nameRe.MatchString(v) {
			return invalid("param %s: %q is not a name", p.ID, v)
		}
	case ParamPath:
		if strings.HasPrefix(v, "/") || strings.HasPrefix(v, `\`) || strings.Contains(v, `\`) || strings.Contains(v, "://") || (len(v) > 1 && v[1] == ':') {
			return invalid("param %s: a relative path only", p.ID)
		}
		c := path.Clean(strings.TrimPrefix(v, "~/"))
		if c == ".." || strings.HasPrefix(c, "../") {
			return invalid("param %s: the path leaves the folder", p.ID)
		}
	case ParamURI:
		// Only a uri the server itself listed is fetched (the source
		// checks it against the cached list); here: one plain token.
		if !uriRe.MatchString(v) || strings.HasPrefix(v, "file:") {
			return invalid("param %s: %q is not a resource uri", p.ID, v)
		}
	}
	return nil
}

// Resolve checks s against d and fills what it leaves out from the def:
// args from param defaults and the def's default args, fields, layout,
// limit and density. The result is the spec the block carries, so the
// customizer always shows the real choices.
func Resolve(d *Def, s blocks.ViewSpec) (blocks.ViewSpec, error) {
	if err := CheckSpec(d, &s); err != nil {
		return blocks.ViewSpec{}, err
	}
	out := s
	out.Args = map[string]string{}
	for _, p := range d.Params {
		if v := s.Args[p.ID]; v != "" {
			out.Args[p.ID] = v
		} else if v := d.Default.Args[p.ID]; v != "" {
			out.Args[p.ID] = v
		} else if p.Default != "" {
			out.Args[p.ID] = p.Default
		}
	}
	if len(out.Args) == 0 {
		out.Args = nil
	}
	if out.Layout == "" {
		out.Layout = d.Default.Layout
	}
	if out.Layout == "" {
		out.Layout = d.Layouts[0]
	}
	if len(out.Fields) == 0 {
		out.Fields = slices.Clone(d.Default.Fields)
	}
	if len(out.Fields) == 0 {
		for _, f := range d.Fields {
			if f.Default {
				out.Fields = append(out.Fields, f.ID)
			}
		}
	}
	if len(out.Fields) == 0 {
		for _, f := range d.Fields {
			out.Fields = append(out.Fields, f.ID)
		}
	}
	if out.Layout == blocks.KindTable && len(out.Fields) > MaxTableFields {
		out.Fields = out.Fields[:MaxTableFields]
	}
	if s.Filter == nil && s.Sort == nil && s.Group == "" {
		// A bare spec ("open the view") gets the def's filter, sort and
		// group; a spec that sets any of them is taken as it is.
		out.Filter = slices.Clone(d.Default.Filter)
		out.Sort = slices.Clone(d.Default.Sort)
		out.Group = d.Default.Group
		if out.Group != "" && out.Layout != blocks.KindTable && out.Layout != blocks.KindList {
			out.Group = ""
		}
	}
	if out.Limit == 0 {
		out.Limit = d.Default.Limit
	}
	if out.Limit == 0 {
		out.Limit = DefaultLimit
	}
	if out.Density == "" {
		out.Density = d.Default.Density
	}
	if out.Density == "" {
		out.Density = DensityCompact
	}
	if out.Refresh == 0 && !d.Live {
		out.Refresh = d.Refresh
	}
	if err := CheckSpec(d, &out); err != nil {
		return blocks.ViewSpec{}, err
	}
	return out, nil
}
