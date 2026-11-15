package assets

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// phoneRoutes are the views a phone or tablet can show: the tabs of
// state/route.ts and every sub view MoreView draws (views/views.tsx).
func phoneRoutes(t *testing.T) (tabs, subs []string) {
	t.Helper()
	route, err := os.ReadFile("src/state/route.ts")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`TABS = \[([^\]]*)\]`).FindSubmatch(route)
	if m == nil {
		t.Fatal("no TABS in src/state/route.ts")
	}
	for _, q := range regexp.MustCompile(`"([a-z]+)"`).FindAllSubmatch(m[1], -1) {
		tabs = append(tabs, string(q[1]))
	}
	views, err := os.ReadFile("src/views/views.tsx")
	if err != nil {
		t.Fatal(err)
	}
	body := string(views)
	if i := strings.Index(body, "export function MoreView"); i >= 0 {
		body = body[i:]
	}
	seen := map[string]bool{}
	for _, q := range regexp.MustCompile(`sub === "([a-z-]+)"|sub\.startsWith\("([a-z-]+)/"\)`).FindAllStringSubmatch(body, -1) {
		s := q[1] + q[2]
		if !seen[s] {
			seen[s] = true
			subs = append(subs, s)
		}
	}
	sort.Strings(subs)
	if len(tabs) < 5 || len(subs) < 15 {
		t.Fatalf("read %d tabs and %d sub views: the parser lost them", len(tabs), len(subs))
	}
	return tabs, subs
}

// Every view a phone or tablet has is reachable: each sub view is a row
// of the More list, and every view (tabs included) is a row of the More
// list or an action of the radial menu's rings. A new view that is in
// neither fails here.
func TestEveryPhoneViewIsReachable(t *testing.T) {
	tabs, subs := phoneRoutes(t)
	list, err := os.ReadFile("src/radial/morelist.ts")
	if err != nil {
		t.Fatalf("the More list: %v", err)
	}
	rows := map[string]bool{}
	for _, q := range regexp.MustCompile(`\["([a-z-]+)", "[a-z_.]+", "[a-zA-Z]+"\]`).FindAllSubmatch(list, -1) {
		rows[string(q[1])] = true
	}
	actions, err := os.ReadFile("src/radial/actions.ts")
	if err != nil {
		t.Fatal(err)
	}
	radial := map[string]bool{}
	for _, q := range regexp.MustCompile(`more\("([a-z-]+)"\)|tab\("([a-z]+)"\)`).FindAllSubmatch(actions, -1) {
		radial[string(q[1])+string(q[2])] = true
	}
	for _, s := range subs {
		if !rows[s] {
			t.Errorf("#/more/%s is not a row of the More list (src/radial/morelist.ts)", s)
		}
	}
	for _, tab := range tabs {
		if tab == "more" {
			continue // the list itself
		}
		if !rows[tab] && !radial[tab] {
			t.Errorf("#/%s is in neither the More list nor the radial menu", tab)
		}
	}
	for _, s := range subs {
		if !rows[s] && !radial[s] {
			t.Errorf("#/more/%s is in neither the More list nor the radial menu", s)
		}
	}
	for r := range rows {
		if !hasName(subs, r) && !hasName(tabs, r) {
			t.Errorf("the More list has a row %q that no view draws", r)
		}
	}
}

func hasName(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
