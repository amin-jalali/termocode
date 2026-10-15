package testkit

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/nerdterm"
)

// Behaviour tests for the first-run setup card on the Welcome page. The
// terminal detection, the font install and gsettings are fakes: nothing
// on the machine running the tests is read or changed.

// freshConfig is a config.json from before the setup card was answered.
const freshConfig = `{"animations": "off", "images": "blocks"}`

var (
	fakeUbuntuMono = nerdterm.Info{Terminal: nerdterm.GNOME, Font: "Ubuntu Mono 13", System: true, Source: "system monospace font"}
	fakeNerdFont   = nerdterm.Info{Terminal: nerdterm.GNOME, Font: "JetBrainsMono Nerd Font 13", Nerd: true, Source: "default profile, custom font"}
)

// fakeProbe is a switchable detection that records the fresh flags.
type fakeProbe struct {
	mu     sync.Mutex
	term   nerdterm.Info
	fresh  []bool
	nerdOK bool // a Nerd Font is installed
}

func (f *fakeProbe) set(term nerdterm.Info) { f.mu.Lock(); f.term = term; f.mu.Unlock() }

func (f *fakeProbe) calls() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]bool(nil), f.fresh...)
}

func (f *fakeProbe) probe(fresh bool) app.SetupProbe {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fresh = append(f.fresh, fresh)
	p := app.SetupProbe{Terminal: f.term}
	if f.nerdOK {
		p.NerdInstalled, p.NerdFamily = true, "JetBrainsMono Nerd Font"
	}
	return p
}

// setupDriver starts the app with a fresh config and term as the
// terminal's font.
func setupDriver(t *testing.T, term nerdterm.Info) (*Driver, *fakeProbe) {
	t.Helper()
	fp := &fakeProbe{term: term, nerdOK: true}
	t.Cleanup(app.SetSetupProbe(fp.probe))
	d := New(t, Options{W: 150, H: 50, Config: map[string]string{"config.json": freshConfig}})
	if !d.WaitForText("Ubuntu Mono", 2*time.Second) && !d.WaitForText("Nerd", time.Second) {
		t.Fatalf("setup card did not show the detection:\n%s", screen(d))
	}
	return d, fp
}

func cardShown(d *Driver) bool { return strings.Contains(screen(d), "SET UP TERMOCODE") }

func setupButton(d *Driver, id string) (app.SetupButtonInfo, bool) {
	for _, b := range d.Model().SetupCardButtons() {
		if b.ID == id {
			return b, true
		}
	}
	return app.SetupButtonInfo{}, false
}

func readConfig(t testing.TB) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSetupCardShowsOnFreshConfig(t *testing.T) {
	d, fp := setupDriver(t, fakeUbuntuMono)
	if !cardShown(d) {
		t.Fatalf("no setup card:\n%s", screen(d))
	}
	for _, want := range []string{"Icons", "Ubuntu Mono", "not a Nerd Font", "AI", "Not set up", "Theme", "0 of 2 done"} {
		if !strings.Contains(screen(d), want) {
			t.Errorf("card does not show %q:\n%s", want, screen(d))
		}
	}
	if got := fp.calls(); len(got) == 0 || got[0] {
		t.Errorf("startup detection should use the cache (fresh=false), calls %v", got)
	}
	checkHits(t, d, "setup card")
}

func TestSetupCardHiddenByDefaultInTests(t *testing.T) {
	d := New(t, Options{W: 150, H: 50})
	if cardShown(d) {
		t.Fatalf("setup_done config still shows the card:\n%s", screen(d))
	}
}

func TestSetupCardHideStaysAfterRestart(t *testing.T) {
	d, _ := setupDriver(t, fakeUbuntuMono)
	clickText(d, "Hide")
	if cardShown(d) {
		t.Fatalf("Hide did not hide the card:\n%s", screen(d))
	}
	if !strings.Contains(screen(d), "Setup: Show Setup Card") {
		t.Errorf("no hint how to bring it back:\n%s", screen(d))
	}
	if v := readConfig(t)["setup_dismissed"]; v != true {
		t.Fatalf("setup_dismissed = %v", v)
	}
	d.Restart()
	if cardShown(d) {
		t.Fatalf("card back after restart:\n%s", screen(d))
	}
	d.Palette("setup-show-card")
	if !d.WaitForText("SET UP TERMOCODE", 2*time.Second) {
		t.Fatalf("Setup: Show Setup Card did not bring it back:\n%s", screen(d))
	}
	if v := readConfig(t)["setup_dismissed"]; v != false {
		t.Fatalf("setup_dismissed after Show = %v", v)
	}
}

func TestSetupCardDoneStaysAfterRestart(t *testing.T) {
	d, _ := setupDriver(t, fakeUbuntuMono)
	clickText(d, "Done ")
	if cardShown(d) {
		t.Fatalf("Done did not hide the card:\n%s", screen(d))
	}
	d.Restart()
	if cardShown(d) {
		t.Fatalf("card back after restart:\n%s", screen(d))
	}
}

func TestSetupCardUseNerdDisabledWithUbuntuMono(t *testing.T) {
	d, _ := setupDriver(t, fakeUbuntuMono)
	b, ok := setupButton(d, "use-nerd")
	if !ok || !b.Disabled || !strings.Contains(b.Why, "Ubuntu Mono") {
		t.Fatalf("Use Nerd icons: %+v", b)
	}
	clickText(d, "Use Nerd icons")
	if !d.WaitForText("not available", time.Second) {
		t.Errorf("pressing the disabled button said nothing:\n%s", screen(d))
	}
	if v := readConfig(t)["icons"]; v != nil {
		t.Fatalf("a disabled button changed icons to %v", v)
	}
}

func TestSetupCardUseNerdEnabledWithNerdFont(t *testing.T) {
	d, _ := setupDriver(t, fakeNerdFont)
	b, ok := setupButton(d, "use-nerd")
	if !ok || b.Disabled {
		t.Fatalf("Use Nerd icons with a Nerd Font: %+v", b)
	}
	clickText(d, "Use Nerd icons")
	if v := readConfig(t)["icons"]; v != "nerd" {
		t.Fatalf("icons = %v, want nerd", v)
	}
	if !d.WaitForText("1 of 2 done", time.Second) {
		t.Errorf("icons step not done:\n%s", screen(d))
	}
}

// The user switches the terminal font, then presses Check again: the
// detection runs fresh (no termfont.json cache) and the button wakes up.
func TestSetupCardCheckAgainRedetects(t *testing.T) {
	d, fp := setupDriver(t, fakeUbuntuMono)
	fp.set(fakeNerdFont)
	clickText(d, "Check again")
	if !d.WaitFor(func() bool { b, _ := setupButton(d, "use-nerd"); return !b.Disabled }, 2*time.Second) {
		t.Fatalf("Use Nerd icons still disabled after the font change:\n%s", screen(d))
	}
	calls := fp.calls()
	if !calls[len(calls)-1] {
		t.Fatalf("Check again used the cache: calls %v", calls)
	}
}

func TestSetupCardAIStepOpensProviderSetup(t *testing.T) {
	d, _ := setupDriver(t, fakeUbuntuMono)
	clickText(d, "Set up AI…")
	if !d.WaitForText("AI Provider", time.Second) {
		t.Fatalf("provider setup did not open:\n%s", screen(d))
	}
}

func TestSetupCardAISkipByKeyboard(t *testing.T) {
	d, _ := setupDriver(t, fakeUbuntuMono)
	for i := 0; i < 40 && !strings.Contains(screen(d), "▌Skip"); i++ {
		d.Key("tab")
	}
	if !strings.Contains(screen(d), "▌Skip") {
		t.Fatalf("Tab never reached Skip:\n%s", screen(d))
	}
	d.Key("enter")
	if !d.WaitForText("Skipped", time.Second) {
		t.Fatalf("Enter on Skip did nothing:\n%s", screen(d))
	}
	settled, _ := readConfig(t)["setup_settled"].([]any)
	if len(settled) != 1 || settled[0] != "ai" {
		t.Fatalf("setup_settled = %v", settled)
	}
}

func TestSetupCardFontInstallInBackground(t *testing.T) {
	fp := &fakeProbe{term: fakeUbuntuMono}
	t.Cleanup(app.SetSetupProbe(fp.probe))
	release := make(chan struct{})
	t.Cleanup(app.SetNerdFontInstaller(func(_ context.Context, progress func(string)) (string, error) {
		progress("downloading... 50%")
		<-release
		fp.mu.Lock()
		fp.nerdOK = true
		fp.mu.Unlock()
		return "/fake/fonts", nil
	}))
	d := New(t, Options{W: 150, H: 50, Config: map[string]string{"config.json": freshConfig}})
	if !d.WaitForText("Install Nerd Font", 2*time.Second) {
		t.Fatalf("no install button:\n%s", screen(d))
	}
	clickText(d, "Install Nerd Font")
	if runtime.GOOS != "linux" {
		// The automatic install is Linux-only: elsewhere the card says so
		// instead of pretending to install.
		if !d.WaitForText("Automatic install works on Linux only", 2*time.Second) {
			t.Fatalf("no honest 'Linux only' message:\n%s", screen(d))
		}
		return
	}
	if !d.WaitForText("50%", 2*time.Second) {
		t.Fatalf("no progress shown:\n%s", screen(d))
	}
	close(release)
	if !d.WaitForText("installed", 2*time.Second) {
		t.Fatalf("install end not told:\n%s", screen(d))
	}
	// Installing is not using: the steps to switch the terminal show.
	if !d.WaitForText("Custom font", 2*time.Second) {
		t.Fatalf("font steps not shown after install:\n%s", screen(d))
	}
}

// gsettingsStore is an in-memory gsettings that records every call.
type gsettingsStore struct {
	mu    sync.Mutex
	vals  map[string]string
	calls [][]string
}

const (
	gnList    = "org.gnome.Terminal.ProfilesList"
	gnProfile = "org.gnome.Terminal.Legacy.Profile:/org/gnome/terminal/legacy/profiles:/:"
	gnDefault = "b1dcc9dd-5262-4d8d-a863-c897e6d979b9"
)

func newGsettingsStore() *gsettingsStore {
	return &gsettingsStore{vals: map[string]string{
		gnList + " list":                            "['" + gnDefault + "']",
		gnList + " default":                         "'" + gnDefault + "'",
		gnProfile + gnDefault + "/ visible-name":    "'Unnamed'",
		gnProfile + gnDefault + "/ use-system-font": "true",
	}}
}

func (g *gsettingsStore) run(_ context.Context, args ...string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls = append(g.calls, append([]string(nil), args...))
	if len(args) < 3 {
		return "", errors.New("bad args")
	}
	k := args[1] + " " + args[2]
	if args[0] == "set" {
		g.vals[k] = args[3]
		return "", nil
	}
	v, ok := g.vals[k]
	if !ok {
		return "", errors.New("no key")
	}
	return v, nil
}

func (g *gsettingsStore) sets() [][]string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out [][]string
	for _, c := range g.calls {
		if c[0] == "set" {
			out = append(out, c)
		}
	}
	return out
}

func TestSetupCardGnomeProfilePreviewThenConfirm(t *testing.T) {
	gs := newGsettingsStore()
	t.Cleanup(app.SetGnomeGsettings(gs.run))
	d, _ := setupDriver(t, fakeUbuntuMono)

	// The preview shows first; Esc changes nothing.
	clickText(d, "Create termocode profile")
	if !d.WaitForText("NEW GNOME Terminal profile", time.Second) {
		t.Fatalf("no preview:\n%s", screen(d))
	}
	for _, want := range []string{"JetBrainsMono Nerd Font 13", "Not changed", "Ubuntu Mono 13"} {
		if !strings.Contains(screen(d), want) {
			t.Errorf("preview misses %q:\n%s", want, screen(d))
		}
	}
	if s := gs.sets(); len(s) != 0 {
		t.Fatalf("settings changed before confirm: %v", s)
	}
	d.Key("esc")
	if !d.WaitForText("Nothing changed", time.Second) || len(gs.sets()) != 0 {
		t.Fatalf("Esc: sets %v\n%s", gs.sets(), screen(d))
	}

	// Confirm: only the new profile and the list change.
	clickText(d, "Create termocode profile")
	if !d.WaitForText("Create profile", time.Second) {
		t.Fatalf("no preview the second time:\n%s", screen(d))
	}
	clickText(d, "Create profile")
	if !d.WaitForText("is ready", 2*time.Second) {
		t.Fatalf("profile not reported ready:\n%s", screen(d))
	}
	sets := gs.sets()
	if len(sets) == 0 {
		t.Fatal("confirm wrote nothing")
	}
	for _, c := range sets {
		if strings.Contains(c[1], gnDefault) || c[2] == "default" {
			t.Fatalf("the default profile was touched: %v", c)
		}
	}
	if !strings.Contains(screen(d), "--profile=termocode") {
		t.Errorf("how to open the profile not told:\n%s", screen(d))
	}
}
