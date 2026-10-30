package webproto

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// opConstants reads every `Op... = "..."` string constant of the package
// sources, so an op name added in code without a policy row fails.
func opConstants(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		n := e.Name()
		if !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, n, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.CONST {
				continue
			}
			for _, s := range g.Specs {
				vs := s.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if !strings.HasPrefix(name.Name, "Op") || i >= len(vs.Values) {
						continue
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						continue
					}
					v, _ := strconv.Unquote(lit.Value)
					out[name.Name] = v
				}
			}
		}
	}
	return out
}

var opNameRE = regexp.MustCompile(`^[a-z]+(\.[a-z_]+)+$`)

func TestEveryOpHasPolicyRow(t *testing.T) {
	consts := opConstants(t)
	if len(consts) == 0 {
		t.Fatal("no op constants found")
	}
	inConsts := map[string]bool{}
	for name, op := range consts {
		inConsts[op] = true
		if _, ok := Lookup(op); !ok {
			t.Errorf("%s = %q has no policy row", name, op)
		}
	}
	seen := map[string]bool{}
	for _, p := range Ops() {
		if seen[p.Op] {
			t.Errorf("%s: two rows", p.Op)
		}
		seen[p.Op] = true
		if !inConsts[p.Op] {
			t.Errorf("%s: row without an Op constant", p.Op)
		}
		if !opNameRE.MatchString(p.Op) || len(p.Op) > MaxOpName {
			t.Errorf("%s: bad op name", p.Op)
		}
		if p.Role != RoleViewer && p.Role != RoleController && p.Role != RoleCtl {
			t.Errorf("%s: role %q", p.Op, p.Role)
		}
		if p.Rate.N <= 0 || p.Rate.Per <= 0 {
			t.Errorf("%s: no rate", p.Op)
		}
		if p.MaxBytes <= 0 || p.MaxBytes > MaxFrame {
			t.Errorf("%s: max bytes %d", p.Op, p.MaxBytes)
		}
		if p.Phase != PhaseV1 && p.Phase != PhaseV2 {
			t.Errorf("%s: phase %q", p.Op, p.Phase)
		}
		if p.Audit != AuditNo && p.Audit != AuditYes && p.Audit != AuditCounted {
			t.Errorf("%s: audit %q", p.Op, p.Audit)
		}
		if p.New == nil {
			t.Errorf("%s: no args type", p.Op)
			continue
		}
		if v := reflect.ValueOf(p.New()); v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
			t.Errorf("%s: args must be a pointer to a struct, got %T", p.Op, p.New())
		}
		// A read never needs the laptop and is never hidden in a count.
		if p.Role == RoleViewer && (p.LaptopCard || p.Audit == AuditCounted) {
			t.Errorf("%s: a viewer op with a laptop card or a counted audit", p.Op)
		}
		// What waits for the laptop is always written down.
		if p.LaptopCard && p.Audit != AuditYes {
			t.Errorf("%s: laptop card without an audit line", p.Op)
		}
	}
}

func TestRolesAndCaps(t *testing.T) {
	viewer := map[string]bool{}
	for _, op := range CapsFor(RoleViewer) {
		viewer[op] = true
	}
	ctrl := map[string]bool{}
	for _, op := range CapsFor(RoleController) {
		ctrl[op] = true
	}
	for _, p := range Ops() {
		wantV := p.Role == RoleViewer
		wantC := p.Role == RoleViewer || p.Role == RoleController
		if viewer[p.Op] != wantV || Allowed(RoleViewer, p.Op) != wantV {
			t.Errorf("%s: viewer allowed = %v, want %v", p.Op, viewer[p.Op], wantV)
		}
		if ctrl[p.Op] != wantC || Allowed(RoleController, p.Op) != wantC {
			t.Errorf("%s: controller allowed = %v, want %v", p.Op, ctrl[p.Op], wantC)
		}
		if Allowed(Role("admin"), p.Op) || Allowed(RoleCtl, p.Op) && p.Role != RoleCtl {
			t.Errorf("%s: allowed for a wrong role", p.Op)
		}
	}
	if Allowed(RoleController, "nope.op") {
		t.Error("unknown op allowed")
	}
	for _, op := range []string{OpWebOpen, OpWebStop} {
		if ctrl[op] || viewer[op] {
			t.Errorf("%s offered to a browser", op)
		}
	}
	if len(CapsFor(Role("admin"))) != 0 {
		t.Error("unknown role has caps")
	}
}

func TestLaptopCardOps(t *testing.T) {
	want := map[string]bool{OpTermClose: true, OpGitDiscard: true, OpWebOpen: true, OpWebPairAsk: true, OpWebGrantAsk: true, OpTreeDelete: true,
		OpGitPush: true, OpGitPull: true, OpGitCheckout: true, OpGitStash: true, OpGitStashPop: true}
	for _, p := range Ops() {
		if p.LaptopCard != want[p.Op] {
			t.Errorf("%s: laptop card = %v", p.Op, p.LaptopCard)
		}
	}
}
