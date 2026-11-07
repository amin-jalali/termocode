package views

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/blocks"
)

// now is 2026-11-07 12:00 UTC; tests run in UTC.
var testNow = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC).UnixMilli()

func init() { Loc = time.UTC }

func prDef() Def {
	return Def{
		ID: "pr.list", Feature: "ci", Title: "Pull requests",
		Source: Source{Op: "pr.list"},
		Params: []Param{
			{ID: "state", Label: "State", Kind: ParamEnum, Enum: []string{"open", "closed", "all"}, Default: "open"},
			{ID: "base", Label: "Base", Kind: ParamString},
		},
		Fields: []Field{
			{ID: "number", Label: "#", Kind: FieldLink, Sortable: true, Filterable: true, Default: true},
			{ID: "title", Label: "Title", Kind: FieldText, Sortable: true, Filterable: true, Default: true},
			{ID: "checks", Label: "CI", Kind: FieldState, Sortable: true, Filterable: true, Default: true},
			{ID: "updated", Label: "Age", Kind: FieldTime, Sortable: true, Filterable: true, Default: true},
			{ID: "author", Label: "Author", Kind: FieldText, Sortable: true, Filterable: true},
			{ID: "adds", Label: "Adds", Kind: FieldNum, Sortable: true, Filterable: true},
			{ID: "cost", Label: "Cost", Kind: FieldCost, Sortable: true, Filterable: true},
			{ID: "draft", Label: "Draft", Kind: FieldBool, Filterable: true},
			{ID: "body", Label: "Body", Kind: FieldMD},
		},
		Layouts: []string{blocks.KindTable, blocks.KindList, blocks.KindCard},
		Default: blocks.ViewSpec{Sort: []blocks.ViewSort{{Field: "updated", Desc: true}}},
		Actions: []blocks.BlockAction{{ID: "talk", Label: "Talk about it", Kind: blocks.ActAttach}},
		Role:    "viewer",
		Refresh: 60,
	}
}

func row(n, title, checks, author string, hoursAgo int, adds float64, draft bool) Row {
	return Row{
		ID: "pr" + n,
		Cells: map[string]Cell{
			"number":  {S: "#" + n},
			"title":   {S: title},
			"checks":  {S: checks},
			"updated": {T: testNow - int64(hoursAgo)*3600_000},
			"author":  {S: author},
			"adds":    {N: adds},
			"cost":    {N: adds / 100},
			"draft":   {B: draft},
		},
		Ref: &blocks.BlockRef{Kind: "pr", ID: n},
	}
}

func prRows() []Row {
	return []Row{
		row("418", "fix toast", "fail", "amin", 2, 12, false),
		row("412", "login rate limit", "running", "sara", 26, 140, false),
		row("409", "Docs: views", "pass", "amin", 50, 3, true),
		row("400", "old cleanup", "pass", "bob", 24*10, 900, false),
		{ID: "pr399", Cells: map[string]Cell{"number": {S: "#399"}, "title": {S: "no checks yet"}, "checks": {Null: true}, "updated": {Null: true}}},
	}
}

func spec(f func(*blocks.ViewSpec)) blocks.ViewSpec {
	s := blocks.ViewSpec{View: "pr.list"}
	if f != nil {
		f(&s)
	}
	return s
}

func mustResolve(t *testing.T, s blocks.ViewSpec) (Def, blocks.ViewSpec) {
	t.Helper()
	d := prDef()
	out, err := Resolve(&d, s)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return d, out
}

func render(t *testing.T, s blocks.ViewSpec) blocks.Block {
	t.Helper()
	d, rs := mustResolve(t, s)
	b, err := Render("b1", &d, rs, prRows(), false, testNow)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return b
}

func firstCol(b blocks.Block) []string {
	var out []string
	for _, r := range b.Rows {
		out = append(out, r[0])
	}
	return out
}

func TestDefCheckAcceptsTheSample(t *testing.T) {
	d := prDef()
	if err := d.Check(); err != nil {
		t.Fatal(err)
	}
}

func TestDefCheckRefusesBrokenDefs(t *testing.T) {
	cases := map[string]func(*Def){
		"bad id":           func(d *Def) { d.ID = "PR List" },
		"no source":        func(d *Def) { d.Source = Source{} },
		"two sources":      func(d *Def) { d.Source.Topic = "git" },
		"mcp no server":    func(d *Def) { d.Source = Source{MCPTool: "list"} },
		"repeated field":   func(d *Def) { d.Fields = append(d.Fields, d.Fields[0]) },
		"unknown kind":     func(d *Def) { d.Fields[0].Kind = "html" },
		"enum without set": func(d *Def) { d.Params[0].Enum = nil },
		"bad default arg":  func(d *Def) { d.Params[0].Default = "merged" },
		"no layouts":       func(d *Def) { d.Layouts = nil },
		"bad layout":       func(d *Def) { d.Layouts = []string{"html"} },
		"live op":          func(d *Def) { d.Live = true; d.Refresh = 0 },
		"short refresh":    func(d *Def) { d.Refresh = 3 },
		"bad default spec": func(d *Def) { d.Default.Sort = []blocks.ViewSort{{Field: "body"}} },
	}
	for name, mutate := range cases {
		d := prDef()
		mutate(&d)
		if err := d.Check(); !errors.Is(err, ErrInvalid) && !errors.Is(err, blocks.ErrInvalid) {
			t.Errorf("%s: Check() = %v, want invalid", name, err)
		}
	}
}

func TestCheckSpecNamesTheAllowedIDs(t *testing.T) {
	cases := []struct {
		name string
		s    blocks.ViewSpec
		want string
	}{
		{"unknown field", spec(func(s *blocks.ViewSpec) { s.Fields = []string{"number", "status"} }), "fields: number, title, checks, updated, author, adds, cost, draft, body"},
		{"unknown param", spec(func(s *blocks.ViewSpec) { s.Args = map[string]string{"repo": "x"} }), "params: state, base"},
		{"unknown layout", spec(func(s *blocks.ViewSpec) { s.Layout = blocks.KindBoard }), "layouts: table, list, card"},
		{"not filterable", spec(func(s *blocks.ViewSpec) {
			s.Filter = []blocks.ViewCond{{Field: "body", Op: "contains", Value: "x"}}
		}), "filterable: number, title, checks, updated, author, adds, cost, draft"},
		{"not sortable", spec(func(s *blocks.ViewSpec) { s.Sort = []blocks.ViewSort{{Field: "draft"}} }), "sortable: number, title, checks, updated, author, adds, cost"},
		{"gt on text", spec(func(s *blocks.ViewSpec) {
			s.Filter = []blocks.ViewCond{{Field: "title", Op: "gt", Value: "a"}}
		}), "gt does not work on a text field"},
		{"bad number", spec(func(s *blocks.ViewSpec) {
			s.Filter = []blocks.ViewCond{{Field: "adds", Op: "gt", Value: "many"}}
		}), "not a number"},
		{"bad time", spec(func(s *blocks.ViewSpec) {
			s.Filter = []blocks.ViewCond{{Field: "updated", Op: "gt", Value: "last tuesday"}}
		}), "not a time"},
		{"enum arg", spec(func(s *blocks.ViewSpec) { s.Args = map[string]string{"state": "merged"} }), "not one of open, closed, all"},
		{"group by number", spec(func(s *blocks.ViewSpec) { s.Group = "adds" }), "cannot group by a num field"},
		{"group a card", spec(func(s *blocks.ViewSpec) { s.Layout = blocks.KindCard; s.Group = "author" }), "only a table or a list is grouped"},
		{"other view", blocks.ViewSpec{View: "git.status"}, `not "pr.list"`},
		{"field twice", spec(func(s *blocks.ViewSpec) { s.Fields = []string{"title", "title"} }), "named twice"},
		{"empty with value", spec(func(s *blocks.ViewSpec) {
			s.Filter = []blocks.ViewCond{{Field: "author", Op: "empty", Value: "x"}}
		}), "takes no value"},
	}
	for _, c := range cases {
		d := prDef()
		err := CheckSpec(&d, &c.s)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: CheckSpec() = %v, want it to say %q", c.name, err, c.want)
		}
	}
}

func TestCheckSpecRefusesSecretsPathsAndLines(t *testing.T) {
	d := prDef()
	d.Params = append(d.Params,
		Param{ID: "dir", Label: "Folder", Kind: ParamPath},
		Param{ID: "table", Label: "Table", Kind: ParamTable},
		Param{ID: "n", Label: "PR", Kind: ParamNumberPR},
	)
	bad := map[string]map[string]string{
		"github token":  {"base": "ghp_abcdefghijklmnopqrstuvwxyz0123456789"},
		"opaque key":    {"base": "AbCdEf0123456789AbCdEf0123456789AbCdEf0123"},
		"secret ref":    {"base": "${secret:db}"},
		"two lines":     {"base": "a\nb"},
		"leaves folder": {"dir": "src/../../etc"},
		"windows path":  {"dir": `src\x`},
		"table sql":     {"table": "orders; drop table x"},
		"pr zero":       {"n": "0"},
	}
	for name, args := range bad {
		s := blocks.ViewSpec{View: "pr.list", Args: args}
		if err := CheckSpec(&d, &s); err == nil {
			t.Errorf("%s: CheckSpec accepted %v", name, args)
		}
	}
	good := map[string]string{"base": "feature/login-rate-limit-for-the-api-gateway", "dir": "internal/webgw", "table": "orders", "n": "418"}
	s := blocks.ViewSpec{View: "pr.list", Args: good}
	if err := CheckSpec(&d, &s); err != nil {
		t.Errorf("good args refused: %v", err)
	}
}

func TestResolveFillsDefaults(t *testing.T) {
	_, s := mustResolve(t, spec(nil))
	if got := strings.Join(s.Fields, ","); got != "number,title,checks,updated" {
		t.Errorf("fields = %s", got)
	}
	if s.Layout != blocks.KindTable || s.Limit != DefaultLimit || s.Density != DensityCompact || s.Refresh != 60 {
		t.Errorf("defaults = layout %s limit %d density %s refresh %d", s.Layout, s.Limit, s.Density, s.Refresh)
	}
	if s.Args["state"] != "open" {
		t.Errorf("args = %v, want the param default", s.Args)
	}
	if len(s.Sort) != 1 || s.Sort[0].Field != "updated" || !s.Sort[0].Desc {
		t.Errorf("sort = %v, want the def's", s.Sort)
	}
	// A spec that sets its own sort keeps it (no def sort mixed in).
	_, s = mustResolve(t, spec(func(s *blocks.ViewSpec) { s.Sort = []blocks.ViewSort{{Field: "title"}} }))
	if len(s.Sort) != 1 || s.Sort[0].Field != "title" {
		t.Errorf("own sort = %v", s.Sort)
	}
}

func TestRenderTableDefaultSortNewestFirst(t *testing.T) {
	b := render(t, spec(nil))
	if b.Kind != blocks.KindTable || b.Total != 5 || b.Truncated {
		t.Fatalf("kind %s total %d truncated %v", b.Kind, b.Total, b.Truncated)
	}
	if got := strings.Join(firstCol(b), " "); got != "#418 #412 #409 #400 #399" {
		t.Errorf("order = %s (no time sorts last)", got)
	}
	if len(b.Columns) != 4 || b.Columns[2].Kind != blocks.ColTone || b.Columns[3].Kind != blocks.ColTime {
		t.Errorf("columns = %+v", b.Columns)
	}
	if b.Spec == nil || b.Spec.View != "pr.list" || b.Source != "pr.list" {
		t.Errorf("block does not carry its spec: %+v", b.Spec)
	}
}

func TestRenderFilters(t *testing.T) {
	cases := []struct {
		name string
		f    []blocks.ViewCond
		want string
	}{
		{"failing CI", []blocks.ViewCond{{Field: "checks", Op: "eq", Value: "FAIL"}}, "#418"},
		{"not passing", []blocks.ViewCond{{Field: "checks", Op: "ne", Value: "pass"}}, "#418 #412 #399"},
		{"in", []blocks.ViewCond{{Field: "author", Op: "in", In: []string{"sara", "bob"}}}, "#412 #400"},
		{"contains any case", []blocks.ViewCond{{Field: "title", Op: "contains", Value: "LOGIN"}}, "#412"},
		{"starts", []blocks.ViewCond{{Field: "title", Op: "starts", Value: "docs"}}, "#409"},
		{"over 100", []blocks.ViewCond{{Field: "adds", Op: "gt", Value: "100"}}, "#412 #400"},
		{"cost with $", []blocks.ViewCond{{Field: "cost", Op: "ge", Value: "$1.40"}}, "#412 #400"},
		{"today", []blocks.ViewCond{{Field: "updated", Op: "eq", Value: "today"}}, "#418"},
		{"last day", []blocks.ViewCond{{Field: "updated", Op: "gt", Value: "-24h"}}, "#418"},
		{"older than a week", []blocks.ViewCond{{Field: "updated", Op: "lt", Value: "7d"}}, "#400"},
		{"since a date", []blocks.ViewCond{{Field: "updated", Op: "ge", Value: "2026-11-05"}}, "#418 #412 #409"},
		{"no checks", []blocks.ViewCond{{Field: "checks", Op: "empty"}}, "#399"},
		{"drafts", []blocks.ViewCond{{Field: "draft", Op: "eq", Value: "true"}}, "#409"},
		{"and", []blocks.ViewCond{{Field: "author", Op: "eq", Value: "amin"}, {Field: "checks", Op: "eq", Value: "pass"}}, "#409"},
	}
	for _, c := range cases {
		b := render(t, spec(func(s *blocks.ViewSpec) { s.Filter = c.f }))
		got := strings.Join(firstCol(b), " ")
		if got != c.want {
			t.Errorf("%s: rows = %q, want %q", c.name, got, c.want)
		}
		if b.Total != len(firstCol(b)) {
			t.Errorf("%s: total %d is not the filtered count", c.name, b.Total)
		}
	}
}

func TestRenderSortKeepsEmptyLastBothWays(t *testing.T) {
	for _, desc := range []bool{false, true} {
		b := render(t, spec(func(s *blocks.ViewSpec) { s.Sort = []blocks.ViewSort{{Field: "checks", Desc: desc}} }))
		col := firstCol(b)
		if col[len(col)-1] != "#399" {
			t.Errorf("desc=%v: %v, want the row with no checks last", desc, col)
		}
	}
	b := render(t, spec(func(s *blocks.ViewSpec) {
		s.Sort = []blocks.ViewSort{{Field: "author"}, {Field: "adds", Desc: true}}
		s.Filter = []blocks.ViewCond{{Field: "author", Op: "not_empty"}}
	}))
	if got := strings.Join(firstCol(b), " "); got != "#418 #409 #400 #412" {
		t.Errorf("two keys = %s", got)
	}
}

func TestRenderLimitMarksTruncated(t *testing.T) {
	b := render(t, spec(func(s *blocks.ViewSpec) { s.Limit = 2 }))
	if len(b.Rows) != 2 || b.Total != 5 || !b.Truncated {
		t.Errorf("rows %d total %d truncated %v", len(b.Rows), b.Total, b.Truncated)
	}
	d, rs := mustResolve(t, spec(nil))
	b, err := Render("b1", &d, rs, prRows(), true, testNow)
	if err != nil || !b.Truncated {
		t.Errorf("a fetch that stopped early must mark the block truncated: %v %v", b.Truncated, err)
	}
}

func TestRenderGroups(t *testing.T) {
	b := render(t, spec(func(s *blocks.ViewSpec) {
		s.Group = "author"
		s.Sort = []blocks.ViewSort{{Field: "number"}}
	}))
	if len(b.Rows) != 0 || len(b.Groups) != 4 {
		t.Fatalf("groups = %+v", b.Groups)
	}
	var labels []string
	for _, g := range b.Groups {
		labels = append(labels, g.Label)
	}
	if got := strings.Join(labels, ","); got != "(none),bob,amin,sara" {
		t.Errorf("labels = %s (first-seen order after the sort)", got)
	}
	if b.Groups[2].Count != 2 || len(b.Groups[2].Rows) != 2 {
		t.Errorf("amin group = %+v", b.Groups[2])
	}
	// The limit counts rows over all groups; counts stay the full counts.
	b = render(t, spec(func(s *blocks.ViewSpec) { s.Group = "author"; s.Limit = 2; s.Sort = []blocks.ViewSort{{Field: "number"}} }))
	if !b.Truncated || len(b.Groups) != 2 || len(b.Groups[1].Rows) != 1 || b.Groups[1].Count != 1 {
		t.Errorf("limited groups = %+v truncated %v", b.Groups, b.Truncated)
	}
	b = render(t, spec(func(s *blocks.ViewSpec) { s.Group = "updated"; s.Layout = blocks.KindList }))
	if b.Groups[0].Label != "2026-11-07" || len(b.Groups[0].Items) != 1 {
		t.Errorf("time groups by day: %+v", b.Groups[0])
	}
}

func TestRenderListDensity(t *testing.T) {
	compact := render(t, spec(func(s *blocks.ViewSpec) {
		s.Layout = blocks.KindList
		s.Fields = []string{"title", "number", "checks", "updated", "author", "adds"}
	}))
	it := compact.Items[0]
	if it.Title != "fix toast" || it.Badge != "fail" || it.Tone != blocks.ToneBad || it.Time == 0 {
		t.Errorf("item = %+v", it)
	}
	if it.Sub != "#418 · amin" {
		t.Errorf("compact sub = %q, want two values", it.Sub)
	}
	if it.Ref == nil || it.Ref.ID != "418" || len(it.Actions) != 1 || it.Actions[0].Ref == nil {
		t.Errorf("item keeps its ref and gets the row actions: %+v", it)
	}
	detailed := render(t, spec(func(s *blocks.ViewSpec) {
		s.Layout = blocks.KindList
		s.Density = DensityDetailed
		s.Fields = []string{"title", "number", "checks", "updated", "author", "adds"}
	}))
	if got := detailed.Items[0].Sub; got != "#: #418 · Author: amin · Adds: 12" {
		t.Errorf("detailed sub = %q", got)
	}
}

func TestRenderCard(t *testing.T) {
	b := render(t, spec(func(s *blocks.ViewSpec) {
		s.Layout = blocks.KindCard
		s.Fields = []string{"number", "checks", "cost", "draft"}
		s.Filter = []blocks.ViewCond{{Field: "number", Op: "eq", Value: "#412"}}
	}))
	if len(b.Fields) != 4 || b.Fields[1].Tone != blocks.ToneBusy || b.Fields[2].Value != "$1.40" || b.Fields[3].Value != "no" {
		t.Errorf("card fields = %+v", b.Fields)
	}
	if b.Ref == nil || b.Ref.ID != "412" {
		t.Errorf("card ref = %+v", b.Ref)
	}
}

func TestRenderCutsCellsAndFitsTheBlock(t *testing.T) {
	long := strings.Repeat("é", 500)
	var rows []Row
	for i := 0; i < 1000; i++ {
		rows = append(rows, Row{ID: "x/" + long, Cells: map[string]Cell{"number": {S: "#1"}, "title": {S: long + "\nsecond"}, "checks": {S: "ok"}, "updated": {T: testNow}}, Ref: &blocks.BlockRef{Kind: "file", Path: "/etc/passwd"}})
	}
	d, s := mustResolve(t, spec(func(s *blocks.ViewSpec) { s.Limit = 200; s.Layout = blocks.KindList }))
	b, err := Render("b1", &d, s, rows, false, testNow)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(b)
	if len(raw) > blocks.MaxBlockBytes || !b.Truncated || len(b.Items) == 200 {
		t.Errorf("block %d bytes, %d items, truncated %v", len(raw), len(b.Items), b.Truncated)
	}
	it := b.Items[0]
	if strings.Contains(it.Title, "\n") || len([]rune(it.Title)) > blocks.MaxTitle || it.ID != "r0" || it.Ref != nil {
		t.Errorf("item not cleaned: id %q ref %+v", it.ID, it.Ref)
	}
}

func TestRegistryRunFetchesOnTheLaptop(t *testing.T) {
	r := NewRegistry()
	r.now = func() time.Time { return time.UnixMilli(testNow) }
	var gotArgs map[string]string
	if err := r.Add(prDef(), func(_ context.Context, s blocks.ViewSpec) ([]Row, error) {
		gotArgs = s.Args
		return prRows(), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(prDef(), func(context.Context, blocks.ViewSpec) ([]Row, error) { return nil, nil }); err == nil {
		t.Error("a repeated id was registered")
	}
	b, err := r.Run(context.Background(), "v1", spec(func(s *blocks.ViewSpec) {
		s.Filter = []blocks.ViewCond{{Field: "checks", Op: "eq", Value: "fail"}}
	}))
	if err != nil {
		t.Fatal(err)
	}
	if gotArgs["state"] != "open" || len(b.Rows) != 1 || b.ID != "v1" {
		t.Errorf("args %v rows %v", gotArgs, b.Rows)
	}
	_, err = r.Run(context.Background(), "v2", blocks.ViewSpec{View: "pr.lst"})
	if err == nil || !strings.Contains(err.Error(), "views: pr.list") {
		t.Errorf("unknown view error = %v, want the near ids", err)
	}
	boom := errors.New("gh is not signed in")
	_ = r.Add(Def{ID: "pr.view", Feature: "ci", Title: "PR", Source: Source{Op: "pr.view"}, Fields: []Field{{ID: "title", Label: "Title", Kind: FieldText}}, Layouts: []string{blocks.KindDetail}},
		func(context.Context, blocks.ViewSpec) ([]Row, error) { return nil, boom })
	if _, err := r.Run(context.Background(), "v3", blocks.ViewSpec{View: "pr.view"}); !errors.Is(err, boom) {
		t.Errorf("fetch error lost: %v", err)
	}
}

func FuzzSpec(f *testing.F) {
	f.Add(`{"view":"pr.list","filter":[{"field":"updated","op":"gt","value":"-24h"}],"group":"author","limit":3}`)
	f.Add(`{"view":"pr.list","layout":"list","density":"detailed","fields":["title","checks"]}`)
	f.Add(`{"view":"pr.list","args":{"base":"x"},"sort":[{"field":"cost","desc":true}]}`)
	f.Fuzz(func(t *testing.T, raw string) {
		var s blocks.ViewSpec
		if json.Unmarshal([]byte(raw), &s) != nil {
			return
		}
		d := prDef()
		rs, err := Resolve(&d, s)
		if err != nil {
			return
		}
		b, err := Render("f", &d, rs, prRows(), false, testNow)
		if err != nil {
			t.Fatalf("a resolved spec failed to render: %v (%s)", err, raw)
		}
		if err := b.Validate(); err != nil {
			t.Fatalf("rendered block is invalid: %v", err)
		}
	})
}
