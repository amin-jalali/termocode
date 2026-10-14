package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/nerdterm"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Step status logic of the first-run setup card, from fake detection
// results. Every message must say what is true, never more.

var (
	ubuntuMono = nerdterm.Info{Terminal: nerdterm.GNOME, Font: "Ubuntu Mono 13", System: true, Source: "system monospace font"}
	gnomeNerd  = nerdterm.Info{Terminal: nerdterm.GNOME, Font: "JetBrainsMono Nerd Font 13", Nerd: true, Source: "default profile, custom font"}
	kittyInfo  = nerdterm.Info{Terminal: nerdterm.Kitty, Nerd: true, Builtin: true}
)

func baseFacts(term nerdterm.Info) setupFacts {
	return setupFacts{
		probed:    true,
		probe:     SetupProbe{Terminal: term, NerdInstalled: true, NerdFamily: "JetBrainsMono Nerd Font"},
		iconMode:  theme.IconModeUnicode,
		goos:      "linux",
		themeID:   "vscode-dark-plus",
		themeName: "VSCode Dark+",
		settled:   map[string]bool{},
	}
}

func stepByID(t *testing.T, steps []setupStep, id string) setupStep {
	t.Helper()
	for _, s := range steps {
		if s.id == id {
			return s
		}
	}
	t.Fatalf("no step %q in %+v", id, steps)
	return setupStep{}
}

func buttonByID(s setupStep, id string) (setupButton, bool) {
	for _, b := range s.buttons {
		if b.id == id {
			return b, true
		}
	}
	return setupButton{}, false
}

func TestSetupIconsStepUbuntuMono(t *testing.T) {
	icons := stepByID(t, buildSetupSteps(baseFacts(ubuntuMono)), "icons")
	if icons.done {
		t.Fatal("icons step done with a non-Nerd font and no answer")
	}
	for _, want := range []string{"Unicode", "Ubuntu Mono", "not a Nerd Font"} {
		if !strings.Contains(icons.status, want) {
			t.Errorf("status %q does not say %q", icons.status, want)
		}
	}
	use, ok := buttonByID(icons, "use-nerd")
	if !ok || !use.disabled || !strings.Contains(use.why, "Ubuntu Mono") {
		t.Fatalf("Use Nerd icons must be shown disabled with the reason: %+v", use)
	}
	if _, ok := buttonByID(icons, "gnome-profile"); !ok {
		t.Error("GNOME: no Create termocode profile button")
	}
	if _, ok := buttonByID(icons, "install-font"); ok {
		t.Error("Install Nerd Font shown while one is installed")
	}
	for _, id := range []string{"font-steps", "check-again", "keep-unicode"} {
		if _, ok := buttonByID(icons, id); !ok {
			t.Errorf("no %s button", id)
		}
	}
}

func TestSetupIconsStepNerdFontFound(t *testing.T) {
	icons := stepByID(t, buildSetupSteps(baseFacts(gnomeNerd)), "icons")
	use, ok := buttonByID(icons, "use-nerd")
	if !ok || use.disabled {
		t.Fatalf("Use Nerd icons must be enabled with a Nerd Font: %+v", use)
	}
	if _, ok := buttonByID(icons, "gnome-profile"); ok {
		t.Error("profile offered although the font is a Nerd Font already")
	}
}

func TestSetupIconsStepDoneWhenNerd(t *testing.T) {
	f := baseFacts(kittyInfo)
	f.iconMode = theme.IconModeNerdFont
	icons := stepByID(t, buildSetupSteps(f), "icons")
	if !icons.done || len(icons.buttons) != 0 {
		t.Fatalf("Nerd icons in kitty: want done with no buttons, got %+v", icons)
	}
}

// Nerd icons forced on while the font is known not to be a Nerd Font:
// say the icons may be boxes, do not call it done.
func TestSetupIconsStepNerdOnWrongFont(t *testing.T) {
	f := baseFacts(ubuntuMono)
	f.iconMode = theme.IconModeNerdFont
	icons := stepByID(t, buildSetupSteps(f), "icons")
	if icons.done || !strings.Contains(icons.status, "boxes") {
		t.Fatalf("got %+v", icons)
	}
	if _, ok := buttonByID(icons, "keep-unicode"); !ok {
		t.Fatal("no way back to Unicode icons")
	}
}

func TestSetupIconsStepKeepUnicode(t *testing.T) {
	f := baseFacts(ubuntuMono)
	f.settled["icons"] = true
	if icons := stepByID(t, buildSetupSteps(f), "icons"); !icons.done {
		t.Fatalf("Keep Unicode did not settle the step: %+v", icons)
	}
}

func TestSetupIconsStepInstallButton(t *testing.T) {
	f := baseFacts(ubuntuMono)
	f.probe.NerdInstalled, f.probe.NerdFamily = false, ""
	icons := stepByID(t, buildSetupSteps(f), "icons")
	inst, ok := buttonByID(icons, "install-font")
	if !ok || inst.disabled {
		t.Fatalf("no enabled Install Nerd Font: %+v", inst)
	}
	if prof, _ := buttonByID(icons, "gnome-profile"); !prof.disabled {
		t.Error("profile button enabled without a Nerd Font installed")
	}
	f.installing, f.installLine = true, "downloading... 40%"
	icons = stepByID(t, buildSetupSteps(f), "icons")
	if inst, _ := buttonByID(icons, "install-font"); !inst.disabled || !strings.Contains(inst.label, "Installing") {
		t.Fatalf("install button while installing: %+v", inst)
	}
	if !strings.Contains(strings.Join(icons.notes, "\n"), "40%") {
		t.Errorf("install progress not shown: %v", icons.notes)
	}
}

func TestSetupIconsStepUnknownAndSSH(t *testing.T) {
	icons := stepByID(t, buildSetupSteps(baseFacts(nerdterm.Info{})), "icons")
	if !strings.Contains(icons.status, "can't read") {
		t.Errorf("unknown terminal: %q", icons.status)
	}
	if _, ok := buttonByID(icons, "gnome-profile"); ok {
		t.Error("profile offered outside GNOME Terminal")
	}
	ssh := nerdterm.Info{Terminal: nerdterm.GNOME, Source: "over SSH"}
	icons = stepByID(t, buildSetupSteps(baseFacts(ssh)), "icons")
	if !strings.Contains(icons.status, "SSH") {
		t.Errorf("ssh: %q", icons.status)
	}
	if _, ok := buttonByID(icons, "gnome-profile"); ok {
		t.Error("profile offered over SSH")
	}
}

func TestSetupIconsStepFontSteps(t *testing.T) {
	f := baseFacts(ubuntuMono)
	f.showSteps = true
	notes := strings.Join(stepByID(t, buildSetupSteps(f), "icons").notes, "\n")
	for _, want := range []string{"Preferences", "Custom font", "JetBrainsMono Nerd Font"} {
		if !strings.Contains(notes, want) {
			t.Errorf("GNOME steps miss %q:\n%s", want, notes)
		}
	}
}

func TestSetupIconsStepProfileReady(t *testing.T) {
	f := baseFacts(ubuntuMono)
	f.probe.ProfileExists = true
	icons := stepByID(t, buildSetupSteps(f), "icons")
	if _, ok := buttonByID(icons, "gnome-profile"); ok {
		t.Error("profile button shown although the profile exists")
	}
	notes := strings.Join(icons.notes, "\n")
	if !strings.Contains(notes, "--profile=termocode") || !strings.Contains(notes, "TERMOCODE_ICON_MODE=nerd") {
		t.Errorf("no way to open the profile told:\n%s", notes)
	}
}

func TestSetupAIStep(t *testing.T) {
	f := baseFacts(kittyInfo)
	ai := stepByID(t, buildSetupSteps(f), "ai")
	if ai.done {
		t.Fatal("AI done without a provider")
	}
	if _, ok := buttonByID(ai, "ai-setup"); !ok {
		t.Fatal("no Set up AI button")
	}
	if _, ok := buttonByID(ai, "ai-skip"); !ok {
		t.Fatal("no Skip button")
	}
	f.settled["ai"] = true
	if ai = stepByID(t, buildSetupSteps(f), "ai"); !ai.done || !strings.Contains(ai.status, "Skipped") {
		t.Fatalf("skipped: %+v", ai)
	}
	f = baseFacts(kittyInfo)
	f.aiConfigured, f.aiProvider, f.aiModel = true, "Anthropic", "claude-x"
	if ai = stepByID(t, buildSetupSteps(f), "ai"); !ai.done || !strings.Contains(ai.status, "Anthropic") || !strings.Contains(ai.status, "claude-x") {
		t.Fatalf("configured: %+v", ai)
	}
}

func TestSetupThemeStepOptional(t *testing.T) {
	th := stepByID(t, buildSetupSteps(baseFacts(kittyInfo)), "theme")
	if !th.optional || len(th.buttons) < 3 {
		t.Fatalf("theme step: %+v", th)
	}
	if b, _ := buttonByID(th, "theme:vscode-dark-plus"); !b.active {
		t.Errorf("current theme not marked: %+v", b)
	}
}

func TestSetupProgressCountsRequiredSteps(t *testing.T) {
	f := baseFacts(kittyInfo)
	f.iconMode = theme.IconModeNerdFont
	v := setupCardFromFacts(f)
	if v.progress != "1 of 2 done" {
		t.Fatalf("progress %q", v.progress)
	}
	ids := map[string]bool{}
	for _, b := range v.buttons() {
		ids[b.id] = true
	}
	if !ids["done"] || !ids["hide"] {
		t.Fatalf("footer buttons missing: %v", ids)
	}
}

func TestSetupConfigPersistence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	p := filepath.Join(dir, "termocode", "config.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(`{"theme": "tokyo-night", "icons": "unicode"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !setupCardWanted(loadSettings()) {
		t.Fatal("fresh config: card not wanted")
	}
	if err := saveSetupConfig(func(c *settingsConfig) { c.SetupDismissed = boolPtr(true) }); err != nil {
		t.Fatal(err)
	}
	if setupCardWanted(loadSettings()) {
		t.Fatal("dismissed card still wanted")
	}
	if err := settleSetupStep("ai"); err != nil {
		t.Fatal(err)
	}
	if err := settleSetupStep("ai"); err != nil {
		t.Fatal(err)
	}
	cfg := loadSettings()
	if len(cfg.SetupSettled) != 1 || cfg.SetupSettled[0] != "ai" {
		t.Fatalf("settled = %v", cfg.SetupSettled)
	}
	if cfg.Theme != "tokyo-night" || cfg.Icons != "unicode" {
		t.Fatalf("other keys lost: %+v", cfg)
	}
	if err := showSetupCardConfig(); err != nil {
		t.Fatal(err)
	}
	if !setupCardWanted(loadSettings()) {
		t.Fatal("Show Setup Card did not bring it back")
	}
	if err := saveSetupConfig(func(c *settingsConfig) { c.SetupDone = boolPtr(true) }); err != nil {
		t.Fatal(err)
	}
	if setupCardWanted(loadSettings()) {
		t.Fatal("done card still wanted")
	}
}
