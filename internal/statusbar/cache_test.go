package statusbar

import (
	"reflect"
	"testing"
)

// TestSameStateComparesEveryField: changing any State field makes
// sameState false, so the View cache can never serve a stale bar when a
// field is added to State but not to sameState.
func TestSameStateComparesEveryField(t *testing.T) {
	base := State{LSP: []string{"gopls"}, Ext: []string{"x"}}
	ty := reflect.TypeOf(base)
	for i := 0; i < ty.NumField(); i++ {
		changed := base
		changed.LSP = append([]string(nil), base.LSP...)
		changed.Ext = append([]string(nil), base.Ext...)
		f := reflect.ValueOf(&changed).Elem().Field(i)
		switch f.Kind() {
		case reflect.String:
			f.SetString(f.String() + "!")
		case reflect.Int:
			f.SetInt(f.Int() + 1)
		case reflect.Bool:
			f.SetBool(!f.Bool())
		case reflect.Slice:
			f.Set(reflect.Append(f, reflect.ValueOf("more")))
		default:
			t.Fatalf("State.%s: kind %s not handled by this test", ty.Field(i).Name, f.Kind())
		}
		if sameState(&base, &changed) {
			t.Errorf("sameState ignores State.%s", ty.Field(i).Name)
		}
	}
	if !sameState(&base, &State{LSP: []string{"gopls"}, Ext: []string{"x"}}) {
		t.Error("equal states differ")
	}
}

// TestViewCacheFollowsState: the cached View equals a fresh draw after
// each change.
func TestViewCacheFollowsState(t *testing.T) {
	m := Model{}
	m.SetWidth(120)
	states := []State{
		{Path: "a.go", Line: 1, Col: 1},
		{Path: "a.go", Line: 2, Col: 1},
		{Path: "a.go", Line: 2, Col: 1, Dirty: true},
		{Path: "a.go", Line: 2, Col: 1, ShowLSP: true, LSP: []string{"gopls"}},
		{Path: "a.go", Line: 2, Col: 1, ShowLSP: true, LSP: []string{"gopls", "x"}},
	}
	for i, s := range states {
		for w := range []int{120, 80} {
			m.SetWidth([]int{120, 80}[w])
			if got, want := m.View(s), m.draw(s); got != want {
				t.Fatalf("state %d width %d: cached View differs from draw", i, m.w)
			}
		}
	}
}
