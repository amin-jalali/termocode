package theme

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// overrideFields is every palette field user_theme.json may set: the
// editor list (PaletteKeys) plus the two fields only the file format knows.
func overrideFields() []string {
	return append(append([]string{}, PaletteKeys...), "BgKeycap", "TextQuaternary")
}

// writeUserTheme points XDG_CONFIG_HOME at a temp dir and writes
// user_theme.json there with the given entries.
func writeUserTheme(t *testing.T, entries map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	path := filepath.Join(dir, "termocode", "user_theme.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// restorePalette puts the default theme back after a test applied another.
func restorePalette(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { ApplyTheme("vscode-dark-plus") })
}

// fieldHex is the hex the palette draws field with.
func fieldHex(t *testing.T, p Palette256, field string) string {
	t.Helper()
	v := reflect.ValueOf(p).FieldByName(field)
	if !v.IsValid() {
		t.Fatalf("Palette256 has no field %q", field)
	}
	return p.IndexHex[Color256(v.Int())]
}

func TestCustomThemeUsesEveryColourFromTheFile(t *testing.T) {
	entries := map[string]string{}
	want := map[string]string{}
	for i, f := range overrideFields() {
		h := "#" + strings.Repeat("0", 4) + string("0123456789abcdef"[i/16]) + string("0123456789abcdef"[i%16])
		entries[f] = h
		want[f] = h
	}
	writeUserTheme(t, entries)

	got, ok := LookupTheme("Custom")
	if !ok || got.ID != UserThemeID || got.Name != UserThemeName {
		t.Fatalf("LookupTheme(Custom) = %s/%s, %v; want the user theme", got.ID, got.Name, ok)
	}
	for f, h := range want {
		if g := fieldHex(t, got.Palette, f); g != h {
			t.Errorf("%s draws %q, want %q from user_theme.json", f, g, h)
		}
	}
	// Each field got its own colour slot, so no two fields share a colour.
	seen := map[Color256]string{}
	for _, f := range overrideFields() {
		idx := Color256(reflect.ValueOf(got.Palette).FieldByName(f).Int())
		if other, dup := seen[idx]; dup {
			t.Errorf("%s and %s share colour slot %d", f, other, idx)
		}
		seen[idx] = f
	}
}

func TestCustomThemeKeepsDefaultsForBadEntries(t *testing.T) {
	def := vscodeDarkPlusPalette()
	writeUserTheme(t, map[string]string{
		"BgEditor":     "#123456",
		"TextPrimary":  "red",     // not a hex colour
		"NoSuchField":  "#abcdef", // a key from a newer version
		"AccentBlue":   "#ABC",    // short form, upper case
		"  ":           "#111111", // blank key
		"SyntaxString": "   ",     // blank value
	})
	got, ok := LookupTheme("custom")
	if !ok {
		t.Fatal("custom theme not found")
	}
	p := got.Palette
	if h := fieldHex(t, p, "BgEditor"); h != "#123456" {
		t.Errorf("BgEditor = %q, want #123456", h)
	}
	if h := fieldHex(t, p, "AccentBlue"); h != "#abc" {
		t.Errorf("AccentBlue = %q, want #abc", h)
	}
	if p.TextPrimary != def.TextPrimary || p.SyntaxString != def.SyntaxString {
		t.Errorf("bad entries changed a colour: TextPrimary %d SyntaxString %d", p.TextPrimary, p.SyntaxString)
	}
	// Only the two good entries add colour slots; the unknown key leaves none behind.
	if n, base := len(p.IndexHex), len(defaultIndexHex()); n != base+2 {
		t.Errorf("IndexHex has %d entries, want %d (defaults + 2)", n, base+2)
	}
}

func TestCustomThemeShowsInThePickerOnlyWithAFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	list := AvailableThemes()
	if list[0].ID != "vscode-dark-plus" {
		t.Errorf("first theme = %s, want the default", list[0].ID)
	}
	if list[len(list)-1].ID == UserThemeID {
		t.Error("Custom listed without a user_theme.json")
	}
	if got, ok := LookupTheme(UserThemeID); ok || got.ID != "vscode-dark-plus" {
		t.Errorf("LookupTheme(custom) with no file = %s, %v; want default, false", got.ID, ok)
	}

	writeUserTheme(t, map[string]string{"BgEditor": "#202020"})
	list = AvailableThemes()
	if last := list[len(list)-1]; last.ID != UserThemeID {
		t.Errorf("last theme = %s, want Custom at the end", last.ID)
	}
	for i := 2; i < len(list)-1; i++ {
		if list[i-1].Name > list[i].Name {
			t.Errorf("themes not sorted: %q before %q", list[i-1].Name, list[i].Name)
		}
	}
}

func TestApplyingCustomThemeShowsTheNewColour(t *testing.T) {
	restorePalette(t)
	writeUserTheme(t, map[string]string{"BgEditor": "#0a0b0c", "DiagError": "#ff0001"})
	if _, ok := ApplyTheme("custom"); !ok {
		t.Fatal("ApplyTheme(custom) failed")
	}
	if got := CurrentPaletteHex("BgEditor"); got != "#0a0b0c" {
		t.Errorf("BgEditor = %q, want #0a0b0c", got)
	}
	if got := CurrentPaletteHex("DiagError"); got != "#ff0001" {
		t.Errorf("DiagError = %q, want #ff0001", got)
	}
	if got := CurrentPaletteHex("NoSuchField"); got != "" {
		t.Errorf("unknown field = %q, want empty", got)
	}
}

func TestEveryEditableKeyHasALiveColour(t *testing.T) {
	restorePalette(t)
	ApplyTheme("vscode-dark-plus")
	for _, k := range PaletteKeys {
		if got := CurrentPaletteHex(k); !IsHexColor(got) {
			t.Errorf("CurrentPaletteHex(%s) = %q, want a hex colour", k, got)
		}
	}
}

func TestNextFreeIndexWhenEverySlotIsTaken(t *testing.T) {
	p := Palette256{IndexHex: map[Color256]string{}}
	for i := 0; i < 256; i++ {
		p.IndexHex[Color256(i)] = "#000000"
	}
	if got := nextFreeIndex(&p); got != 255 {
		t.Errorf("nextFreeIndex with no free slot = %d, want 255", got)
	}
	if got := nextFreeIndex(nil); got < 16 || got > 255 {
		t.Errorf("nextFreeIndex(nil) = %d, want a slot in 16..255", got)
	}
	// A palette without its own map still gets one.
	var empty Palette256
	applyHexOverride(&empty, "BgEditor", "#010203")
	if fieldHex(t, empty, "BgEditor") != "#010203" {
		t.Error("override on a palette with no IndexHex was lost")
	}
}

func TestSaveUserOverridesKeepsOtherKeys(t *testing.T) {
	path := writeUserTheme(t, map[string]string{"BgEditor": "#111111", "TextPrimary": "#eeeeee"})
	if err := SaveUserOverrides(map[string]string{"AccentBlue": " #0000ff ", "TextPrimary": ""}); err != nil {
		t.Fatal(err)
	}
	got := LoadUserOverrides()
	want := map[string]string{"BgEditor": "#111111", "AccentBlue": "#0000ff"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("after save = %v, want %v", got, want)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file gone: %v", err)
	}
}

func TestSaveUserOverridesCreatesTheFolder(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "new", "dir"))
	if err := SaveUserOverrides(map[string]string{"BgEditor": "#222222"}); err != nil {
		t.Fatal(err)
	}
	if !HasUserTheme() {
		t.Error("HasUserTheme false after the first save")
	}
}

func TestSaveUserOverridesOverACorruptFile(t *testing.T) {
	path := writeUserTheme(t, nil)
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := LoadUserOverrides(); got != nil {
		t.Errorf("corrupt file loads as %v, want nil", got)
	}
	if err := SaveUserOverrides(map[string]string{"BgEditor": "#333333"}); err != nil {
		t.Fatal(err)
	}
	if got := LoadUserOverrides(); got["BgEditor"] != "#333333" {
		t.Errorf("after save over a corrupt file = %v", got)
	}
}

func TestUserThemeErrorsWhenTheFolderCannotBeMade(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", blocker) // a file, so termocode/ can't be made under it
	if err := SaveUserOverrides(map[string]string{"BgEditor": "#444444"}); err == nil {
		t.Error("SaveUserOverrides under a file: want an error")
	}
	if got := LoadUserOverrides(); got != nil {
		t.Errorf("LoadUserOverrides = %v, want nil", got)
	}
}

func TestUserThemeFileThatIsAFolderLoadsEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "termocode", "user_theme.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := LoadUserOverrides(); got != nil {
		t.Errorf("LoadUserOverrides on a folder = %v, want nil", got)
	}
	if err := SaveUserOverrides(map[string]string{"BgEditor": "#555555"}); err == nil {
		t.Error("SaveUserOverrides onto a folder: want an error")
	}
}

func TestUserThemePathFallsBackToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", home)
	got, err := UserThemePath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".config", "termocode", "user_theme.json"); got != want {
		t.Errorf("UserThemePath = %q, want %q", got, want)
	}
}

func TestNoHomeMeansNoUserThemeAndNoCrash(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if _, err := UserThemePath(); err == nil {
		t.Skip("this platform finds a home folder without $HOME")
	}
	if got := LoadUserOverrides(); got != nil {
		t.Errorf("LoadUserOverrides = %v, want nil", got)
	}
	if err := SaveUserOverrides(map[string]string{"BgEditor": "#666666"}); err == nil {
		t.Error("SaveUserOverrides without a home: want an error")
	}
}

func TestIsHexColor(t *testing.T) {
	for s, want := range map[string]bool{
		"#abc":     true,
		"#A1B2C3":  true,
		" #0f0f0f": true,
		"#12345":   false,
		"abcdef":   false,
		"#ggg":     false,
		"#":        false,
		"":         false,
		"#1234567": false,
	} {
		if got := IsHexColor(s); got != want {
			t.Errorf("IsHexColor(%q) = %v, want %v", s, got, want)
		}
	}
}
