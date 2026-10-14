package setup

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// fakeDconf is an in-memory gsettings: "schema key" → value. It records
// every call.
type fakeDconf struct {
	vals  map[string]string
	calls [][]string
	fail  string // a key whose "set" fails
	drop  bool   // "set" succeeds but keeps nothing
}

func (f *fakeDconf) run(_ context.Context, args ...string) (string, error) {
	f.calls = append(f.calls, append([]string(nil), args...))
	if len(args) < 3 {
		return "", errors.New("bad args")
	}
	k := args[1] + " " + args[2]
	switch args[0] {
	case "get":
		v, ok := f.vals[k]
		if !ok {
			return "", errors.New("no key")
		}
		return v + "\n", nil
	case "set":
		if args[2] == f.fail {
			return "", errors.New("denied")
		}
		if !f.drop {
			f.vals[k] = args[3]
		}
		return "", nil
	}
	return "", errors.New("bad verb")
}

func (f *fakeDconf) sets() [][]string {
	var out [][]string
	for _, c := range f.calls {
		if c[0] == "set" {
			out = append(out, c)
		}
	}
	return out
}

const defaultProfile = "b1dcc9dd-5262-4d8d-a863-c897e6d979b9"

func newFakeDconf() *fakeDconf {
	return &fakeDconf{vals: map[string]string{
		gnomeListSchema + " list":                                 "['" + defaultProfile + "']",
		gnomeListSchema + " default":                              "'" + defaultProfile + "'",
		gnomeProfileSchema + defaultProfile + "/ visible-name":    "'Unnamed'",
		gnomeProfileSchema + defaultProfile + "/ use-system-font": "true",
		"org.gnome.desktop.interface monospace-font-name":         "'Ubuntu Mono 13'",
	}}
}

func TestPlanGnomeProfileReadsOnly(t *testing.T) {
	f := newFakeDconf()
	p, err := PlanGnomeProfile(context.Background(), f.run, "JetBrainsMono Nerd Font", 13, func() string { return "new-id" })
	if err != nil {
		t.Fatal(err)
	}
	if len(f.sets()) != 0 {
		t.Fatalf("planning changed settings: %v", f.sets())
	}
	if p.Exists || p.UUID != "new-id" || p.Font != "JetBrainsMono Nerd Font 13" {
		t.Fatalf("plan = %+v", p)
	}
	if want := []string{defaultProfile, "new-id"}; !reflect.DeepEqual(p.List, want) {
		t.Fatalf("list = %v, want %v", p.List, want)
	}
	// Every command targets the new profile or the list — never the
	// default profile and never the "default" key.
	for _, c := range p.Commands() {
		joined := strings.Join(c, " ")
		if strings.Contains(joined, defaultProfile+"/") || c[2] == "default" {
			t.Fatalf("command touches the default profile: %v", c)
		}
	}
}

func TestApplyGnomeProfile(t *testing.T) {
	f := newFakeDconf()
	ctx := context.Background()
	p, err := PlanGnomeProfile(ctx, f.run, "JetBrainsMono Nerd Font", 0, func() string { return "new-id" })
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyGnomeProfile(ctx, f.run, p); err != nil {
		t.Fatal(err)
	}
	if got := f.vals[gnomeProfileSchema+"new-id/ font"]; got != "'JetBrainsMono Nerd Font 12'" {
		t.Fatalf("font = %q", got)
	}
	if got := f.vals[gnomeListSchema+" default"]; got != "'"+defaultProfile+"'" {
		t.Fatalf("default profile changed to %q", got)
	}
	if got := f.vals[gnomeProfileSchema+defaultProfile+"/ use-system-font"]; got != "true" {
		t.Fatalf("default profile font setting changed: %q", got)
	}
	// A second plan finds it: nothing more to do.
	p2, err := PlanGnomeProfile(ctx, f.run, "JetBrainsMono Nerd Font", 13, nil)
	if err != nil || !p2.Exists || p2.UUID != "new-id" {
		t.Fatalf("second plan = %+v, %v", p2, err)
	}
	n := len(f.sets())
	if err := ApplyGnomeProfile(ctx, f.run, p2); err != nil || len(f.sets()) != n {
		t.Fatalf("applying an existing profile wrote again: %v", err)
	}
}

func TestApplyGnomeProfileReportsFailure(t *testing.T) {
	ctx := context.Background()
	f := newFakeDconf()
	f.fail = "font"
	p, _ := PlanGnomeProfile(ctx, f.run, "X Nerd Font", 12, func() string { return "id1" })
	if err := ApplyGnomeProfile(ctx, f.run, p); err == nil {
		t.Fatal("a failed set was not reported")
	}
	f = newFakeDconf()
	f.drop = true
	p, _ = PlanGnomeProfile(ctx, f.run, "X Nerd Font", 12, func() string { return "id1" })
	if err := ApplyGnomeProfile(ctx, f.run, p); err == nil {
		t.Fatal("a change that did not stick was not reported")
	}
}

func TestGVariantHelpers(t *testing.T) {
	if got := parseGVList("@as []"); len(got) != 0 {
		t.Fatalf("empty list = %v", got)
	}
	if got := parseGVList("['a', 'b']\n"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("list = %v", got)
	}
	if got := quoteGV("it's"); got != `'it\'s'` {
		t.Fatalf("quote = %s", got)
	}
	if unquoteGV(quoteGV(`a\b'c`)) != `a\b'c` {
		t.Fatal("quote round trip")
	}
	for in, want := range map[string]int{"Ubuntu Mono 13": 13, "Monospace": 0, "X 999": 0, "": 0} {
		if got := FontSize(in); got != want {
			t.Errorf("FontSize(%q) = %d, want %d", in, got, want)
		}
	}
}
