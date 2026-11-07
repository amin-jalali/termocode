package assets

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func readHomeSrc(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("src/home/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Spec (MOB-9): the Views menu and the Customize sheet are part of the
// phone's Home (home.js), not of app.js: they live under src/home.
// view.run sends a spec ({spec, live}), never the old {view} shape the
// strict decoder refuses.
func TestHomeViewsMenuAndCustomize(t *testing.T) {
	menu := readHomeSrc(t, "ViewsMenu.tsx")
	for _, want := range []string{`"view.catalog"`, "byFeature(", "Params", `type="search"`} {
		if !strings.Contains(menu, want) {
			t.Errorf("ViewsMenu.tsx: no %s", want)
		}
	}
	cust := readHomeSrc(t, "Customize.tsx")
	for _, want := range []string{"cust.layout", "cust.density", "cust.fields", "cust.filter", "cust.sort", "cust.group", "cust.limit", "cust.apply", "cleanSpec("} {
		if !strings.Contains(cust, want) {
			t.Errorf("Customize.tsx: no %s", want)
		}
	}
	// Safe choice first: Cancel comes before Apply.
	if i, j := strings.Index(cust, `hs("cust.cancel")`), strings.Index(cust, `hs("cust.apply")`); i < 0 || j < 0 || i > j {
		t.Error("Customize: Cancel is not before Apply")
	}
	home := readHomeSrc(t, "Home.tsx")
	if strings.Contains(home, `request("view.run", { view:`) {
		t.Error(`Home.tsx sends view.run {view}: the laptop wants {spec}`)
	}
	for _, want := range []string{`request("view.run", { spec, live })`, `"view_update"`, "view.stop", "specFromRoute(", "<Customize", "<ViewsMenu"} {
		if !strings.Contains(home+readHomeSrc(t, "views.ts")+readHomeSrc(t, "Home.tsx"), want) {
			t.Errorf("Home: no %s", want)
		}
	}
	if !strings.Contains(readHomeSrc(t, "QuickBlock.tsx"), "onCustomize") {
		t.Error("QuickBlock has no ⚙ (Customize)")
	}
}

// Spec (MOB-9, MOBUX rules): the phone's new strings are English only.
func TestHomeViewStringsEnglish(t *testing.T) {
	s := readHomeSrc(t, "strings.ts")
	key := regexp.MustCompile(`"((?:views|cust|op|layout|feature)\.[a-z_.]+)": "([^"]*)"`)
	n := 0
	for _, m := range key.FindAllStringSubmatch(s, -1) {
		n++
		for _, r := range m[2] {
			if r >= 0x0600 && r <= 0x06FF {
				t.Errorf("%s is not English: %q", m[1], m[2])
			}
		}
	}
	if n < 30 {
		t.Fatalf("only %d view strings", n)
	}
}
