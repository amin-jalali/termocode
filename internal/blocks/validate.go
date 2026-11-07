package blocks

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrInvalid is returned by Validate for a block that breaks a cap or a
// rule. The message names the first rule broken.
var ErrInvalid = errors.New("blocks: invalid block")

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

var (
	kindSet    = setOf(Kinds()...)
	toneSet    = setOf("", ToneOK, ToneWarn, ToneBad, ToneBusy, ToneIdle, ToneInfo)
	colKindSet = setOf("", ColText, ColNum, ColTime, ColCost, ColTone, ColMono)
	formSet    = setOf(FormText, FormNum, FormBool, FormChoice, FormLong)
	actSet     = setOf(ActOp, ActAttach, ActView, ActLaptop)
	diffSet    = setOf("add", "del", "ctx")
	condSet    = setOf("eq", "ne", "gt", "lt", "ge", "le", "contains", "starts", "in", "empty", "not_empty")
	densitySet = setOf("", "compact", "detailed")
	idRe       = regexp.MustCompile(`^[A-Za-z0-9_.:\-]+$`)
	opRe       = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
	// secretish names a form field must never have: a form never asks for
	// a secret (they stay on the laptop).
	secretish = regexp.MustCompile(`(?i)password|passphrase|(^|[^a-z])(pass|pin|otp)([^a-z]|$)|secret|token([^a-z]|$)|api[_-]?key|private[_-]?key|credential`)
)

func setOf(vs ...string) map[string]bool {
	m := make(map[string]bool, len(vs))
	for _, v := range vs {
		m[v] = true
	}
	return m
}

// Validate checks every cap and rule of the block: a known kind with its
// payload, ids, text lengths, list sizes, relative paths, op names, no
// secret form field, and the whole block at most MaxBlockBytes as JSON.
func (b *Block) Validate() error {
	if b == nil {
		return invalid("no block")
	}
	if err := checkID("block id", b.ID); err != nil {
		return err
	}
	if !kindSet[b.Kind] {
		return invalid("unknown kind %q (one of %s)", b.Kind, strings.Join(Kinds(), ", "))
	}
	if err := checkText("title", b.Title, MaxTitle, true); err != nil {
		return err
	}
	if err := checkText("source", b.Source, MaxTitle, false); err != nil {
		return err
	}
	if err := checkText("ws", b.WS, MaxID, false); err != nil {
		return err
	}
	if err := checkText("for_you", b.ForYou, MaxForYou, false); err != nil {
		return err
	}
	if b.Total < 0 {
		return invalid("negative total")
	}
	if b.Spec != nil {
		if err := b.Spec.Validate(); err != nil {
			return err
		}
	}
	if err := checkRef(b.Ref); err != nil {
		return err
	}
	if err := checkActions(b.Actions); err != nil {
		return err
	}
	if !toneSet[b.State] {
		return invalid("unknown state %q", b.State)
	}
	if err := checkItems("items", b.Items); err != nil {
		return err
	}
	if len(b.Columns) > MaxColumns {
		return invalid("%d columns, max %d", len(b.Columns), MaxColumns)
	}
	for _, c := range b.Columns {
		if err := checkID("column id", c.ID); err != nil {
			return err
		}
		if err := checkText("column label", c.Label, MaxTitle, false); err != nil {
			return err
		}
		if !colKindSet[c.Kind] {
			return invalid("column %q: unknown kind %q", c.ID, c.Kind)
		}
	}
	if err := checkRows(b.Rows, len(b.Columns)); err != nil {
		return err
	}
	if len(b.Groups) > MaxGroups {
		return invalid("%d groups, max %d", len(b.Groups), MaxGroups)
	}
	for _, g := range b.Groups {
		if err := checkText("group label", g.Label, MaxTitle, true); err != nil {
			return err
		}
		if g.Count < 0 {
			return invalid("group %q: negative count", g.Label)
		}
		if err := checkItems("group items", g.Items); err != nil {
			return err
		}
		if err := checkRows(g.Rows, len(b.Columns)); err != nil {
			return err
		}
	}
	if err := checkDiff(b.Diff); err != nil {
		return err
	}
	if len(b.Fields) > MaxFields {
		return invalid("%d fields, max %d", len(b.Fields), MaxFields)
	}
	for _, f := range b.Fields {
		if err := checkText("field label", f.Label, MaxTitle, true); err != nil {
			return err
		}
		if err := checkText("field value", f.Value, MaxSub, false); err != nil {
			return err
		}
		if !colKindSet[f.Kind] || !toneSet[f.Tone] {
			return invalid("field %q: unknown kind or tone", f.Label)
		}
	}
	if len(b.Bars) > MaxBars {
		return invalid("%d bars, max %d", len(b.Bars), MaxBars)
	}
	for _, r := range b.Bars {
		if err := checkText("bar label", r.Label, MaxTitle, true); err != nil {
			return err
		}
		if err := checkText("bar text", r.Text, MaxTitle, false); err != nil {
			return err
		}
		if math.IsNaN(r.Value) || r.Value < 0 || r.Value > 1e15 { // NaN, negative or absurd
			return invalid("bar %q: value out of range", r.Label)
		}
		if !toneSet[r.Tone] {
			return invalid("bar %q: unknown tone %q", r.Label, r.Tone)
		}
	}
	if err := checkForm(b.Form); err != nil {
		return err
	}
	if len(b.JSON) > MaxJSON {
		return invalid("json is %d bytes, max %d", len(b.JSON), MaxJSON)
	}
	if b.JSON != "" && !json.Valid([]byte(b.JSON)) {
		return invalid("json is not valid JSON")
	}
	if err := checkBoard(b.Board); err != nil {
		return err
	}
	if err := checkPayload(b); err != nil {
		return err
	}
	raw, err := json.Marshal(b)
	if err != nil {
		return invalid("does not encode: %v", err)
	}
	if len(raw) > MaxBlockBytes {
		return invalid("block is %d bytes, max %d", len(raw), MaxBlockBytes)
	}
	return nil
}

// checkPayload wants the payload of the block's kind.
func checkPayload(b *Block) error {
	ok := true
	switch b.Kind {
	case KindTable:
		ok = len(b.Columns) > 0
	case KindDiff:
		ok = b.Diff != nil
	case KindStatus:
		ok = b.State != ""
	case KindForm:
		ok = len(b.Form) > 0
	case KindJSON:
		ok = b.JSON != ""
	case KindText:
		ok = b.MD != nil || b.ForYou != ""
	case KindBoard:
		ok = b.Board != nil
	}
	if !ok {
		return invalid("a %s block without its data", b.Kind)
	}
	return nil
}

func checkID(what, id string) error {
	if id == "" || len(id) > MaxID || !idRe.MatchString(id) {
		return invalid("bad %s %q", what, cut(id))
	}
	return nil
}

// checkText wants valid UTF-8 without control characters, at most max
// runes, and not empty when need is set.
func checkText(what, s string, max int, need bool) error {
	if need && strings.TrimSpace(s) == "" {
		return invalid("empty %s", what)
	}
	if !utf8.ValidString(s) {
		return invalid("%s is not UTF-8", what)
	}
	if n := utf8.RuneCountInString(s); n > max {
		return invalid("%s has %d characters, max %d", what, n, max)
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\t' {
			return invalid("%s has a control character", what)
		}
	}
	return nil
}

func checkItems(what string, items []BlockItem) error {
	if len(items) > MaxItems {
		return invalid("%d %s, max %d", len(items), what, MaxItems)
	}
	for _, it := range items {
		if err := checkID("item id", it.ID); err != nil {
			return err
		}
		if err := checkText("item title", it.Title, MaxTitle, true); err != nil {
			return err
		}
		if err := checkText("item sub", it.Sub, MaxSub, false); err != nil {
			return err
		}
		if err := checkText("item badge", it.Badge, MaxID, false); err != nil {
			return err
		}
		if !toneSet[it.Tone] {
			return invalid("item %q: unknown tone %q", it.ID, it.Tone)
		}
		if it.Time < 0 {
			return invalid("item %q: negative time", it.ID)
		}
		if err := checkRef(it.Ref); err != nil {
			return err
		}
		if err := checkActions(it.Actions); err != nil {
			return err
		}
	}
	return nil
}

func checkRows(rows [][]string, cols int) error {
	if len(rows) > MaxRows {
		return invalid("%d rows, max %d", len(rows), MaxRows)
	}
	for i, r := range rows {
		if len(r) != cols {
			return invalid("row %d has %d cells for %d columns", i, len(r), cols)
		}
		for _, c := range r {
			if err := checkText("cell", c, MaxSub, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkDiff(d *BlockDiff) error {
	if d == nil {
		return nil
	}
	if err := checkPath(d.Path, true); err != nil {
		return err
	}
	if err := checkPath(d.Old, false); err != nil {
		return err
	}
	if d.Adds < 0 || d.Dels < 0 {
		return invalid("negative diff counts")
	}
	if len(d.Hunks) > MaxDiffHunks {
		return invalid("%d hunks, max %d", len(d.Hunks), MaxDiffHunks)
	}
	n := 0
	for _, h := range d.Hunks {
		if err := checkText("hunk header", h.Header, MaxSub, false); err != nil {
			return err
		}
		n += len(h.Lines)
		if n > MaxDiffLines {
			return invalid("diff has more than %d lines", MaxDiffLines)
		}
		for _, l := range h.Lines {
			if !diffSet[l.K] || l.A < 0 || l.B < 0 {
				return invalid("bad diff line")
			}
			if err := checkText("diff line", l.Text, 4*MaxSub, false); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkPath wants a workspace-relative or ~-relative path: never absolute,
// never a drive letter, never a URL, never "..".
func checkPath(p string, need bool) error {
	if p == "" {
		if need {
			return invalid("empty path")
		}
		return nil
	}
	if len(p) > MaxPath || !utf8.ValidString(p) || strings.ContainsRune(p, 0) {
		return invalid("bad path")
	}
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) || (len(p) > 1 && p[1] == ':') || strings.Contains(p, "://") {
		return invalid("path %q is not relative", cut(p))
	}
	for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return invalid("path %q leaves its folder", cut(p))
		}
	}
	for _, r := range p {
		if unicode.IsControl(r) {
			return invalid("path has a control character")
		}
	}
	return nil
}

func checkRef(r *BlockRef) error {
	if r == nil {
		return nil
	}
	if err := checkID("ref kind", r.Kind); err != nil {
		return err
	}
	if r.ID != "" {
		if err := checkText("ref id", r.ID, MaxTitle, false); err != nil {
			return err
		}
	}
	if r.Line < 0 {
		return invalid("negative ref line")
	}
	return checkPath(r.Path, false)
}

func checkActions(as []BlockAction) error {
	if len(as) > MaxActions {
		return invalid("%d actions, max %d", len(as), MaxActions)
	}
	for _, a := range as {
		if err := checkID("action id", a.ID); err != nil {
			return err
		}
		if err := checkText("action label", a.Label, MaxTitle, true); err != nil {
			return err
		}
		if !actSet[a.Kind] {
			return invalid("action %q: unknown kind %q", a.ID, a.Kind)
		}
		switch a.Kind {
		case ActOp:
			if len(a.Op) > MaxID || !opRe.MatchString(a.Op) {
				return invalid("action %q: bad op %q", a.ID, cut(a.Op))
			}
		case ActView:
			if a.Spec == nil {
				return invalid("action %q: a view action without a spec", a.ID)
			}
		}
		if a.Kind != ActOp && a.Op != "" {
			return invalid("action %q: only an op action names an op", a.ID)
		}
		if err := checkRef(a.Ref); err != nil {
			return err
		}
		if a.Spec != nil {
			if err := a.Spec.Validate(); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkForm(fs []BlockFormField) error {
	if len(fs) > MaxFormFields {
		return invalid("%d form fields, max %d", len(fs), MaxFormFields)
	}
	for _, f := range fs {
		if err := checkID("form field id", f.ID); err != nil {
			return err
		}
		if err := checkText("form label", f.Label, MaxTitle, true); err != nil {
			return err
		}
		if err := checkText("form hint", f.Hint, MaxSub, false); err != nil {
			return err
		}
		if err := checkText("form value", f.Value, MaxSub, false); err != nil {
			return err
		}
		if !formSet[f.Kind] {
			return invalid("form field %q: unknown kind %q", f.ID, f.Kind)
		}
		if secretish.MatchString(f.ID) || secretish.MatchString(f.Label) {
			return invalid("form field %q asks for a secret; secrets stay on the laptop", f.ID)
		}
		if (f.Kind == FormChoice) != (len(f.Choices) > 0) {
			return invalid("form field %q: choices go with the choice kind only", f.ID)
		}
		if len(f.Choices) > MaxChoices {
			return invalid("form field %q: %d choices, max %d", f.ID, len(f.Choices), MaxChoices)
		}
		for _, c := range f.Choices {
			if err := checkText("form choice", c, MaxTitle, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func checkBoard(b *BlockBoard) error {
	if b == nil {
		return nil
	}
	if len(b.Columns) == 0 || len(b.Columns) > MaxBoardCols {
		return invalid("board has %d columns (1 to %d)", len(b.Columns), MaxBoardCols)
	}
	for _, c := range b.Columns {
		if err := checkID("board column id", c.ID); err != nil {
			return err
		}
		if err := checkText("board column label", c.Label, MaxTitle, true); err != nil {
			return err
		}
		if !toneSet[c.Tone] {
			return invalid("board column %q: unknown tone %q", c.ID, c.Tone)
		}
		if err := checkItems("board items", c.Items); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks the shape of a spec: sizes, ids and enums. It never
// allows SQL, a shell line, an absolute path or a URL in an arg: those
// are not view args (the registry checks names and kinds per view).
func (s *ViewSpec) Validate() error {
	if s == nil {
		return invalid("no spec")
	}
	if err := checkID("view", s.View); err != nil {
		return err
	}
	if s.WS != "" {
		if err := checkID("spec ws", s.WS); err != nil {
			return err
		}
	}
	if len(s.Args) > MaxSpecArgs {
		return invalid("%d args, max %d", len(s.Args), MaxSpecArgs)
	}
	for k, v := range s.Args {
		if err := checkID("arg name", k); err != nil {
			return err
		}
		if err := checkText("arg "+k, v, MaxSub, false); err != nil {
			return err
		}
		if strings.Contains(v, "://") || strings.HasPrefix(v, "/") || strings.HasPrefix(v, `\`) || (len(v) > 1 && v[1] == ':' && unicode.IsLetter(rune(v[0]))) {
			return invalid("arg %s: a spec never carries a path or a URL", k)
		}
	}
	if len(s.Fields) > MaxSpecFields {
		return invalid("%d fields, max %d", len(s.Fields), MaxSpecFields)
	}
	for _, f := range s.Fields {
		if err := checkID("field", f); err != nil {
			return err
		}
	}
	if len(s.Filter) > MaxSpecFilter {
		return invalid("%d filter conditions, max %d", len(s.Filter), MaxSpecFilter)
	}
	for _, c := range s.Filter {
		if err := checkID("filter field", c.Field); err != nil {
			return err
		}
		if !condSet[c.Op] {
			return invalid("filter %s: unknown op %q", c.Field, c.Op)
		}
		if err := checkText("filter value", c.Value, MaxSub, false); err != nil {
			return err
		}
		if (c.Op == "in") != (len(c.In) > 0) || len(c.In) > MaxSpecIn {
			return invalid("filter %s: in takes 1 to %d values, other ops none", c.Field, MaxSpecIn)
		}
		for _, v := range c.In {
			if err := checkText("filter value", v, MaxSub, false); err != nil {
				return err
			}
		}
	}
	if len(s.Sort) > MaxSpecSort {
		return invalid("%d sort keys, max %d", len(s.Sort), MaxSpecSort)
	}
	for _, o := range s.Sort {
		if err := checkID("sort field", o.Field); err != nil {
			return err
		}
	}
	if s.Group != "" {
		if err := checkID("group", s.Group); err != nil {
			return err
		}
	}
	if s.Limit < 0 || s.Limit > MaxSpecLimit {
		return invalid("limit %d (1 to %d)", s.Limit, MaxSpecLimit)
	}
	if s.Layout != "" && !kindSet[s.Layout] {
		return invalid("unknown layout %q", s.Layout)
	}
	if !densitySet[s.Density] {
		return invalid("unknown density %q", s.Density)
	}
	if err := checkText("spec title", s.Title, 60, false); err != nil {
		return err
	}
	if s.Refresh != 0 && s.Refresh < MinSpecRefresh {
		return invalid("refresh %d s, min %d", s.Refresh, MinSpecRefresh)
	}
	return nil
}

func cut(s string) string {
	if len(s) > 40 {
		s = s[:40] + "…"
	}
	return s
}
