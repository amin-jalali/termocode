// Package setup implements the `termocode setup` subcommand: it verifies
// runtime prerequisites (neovim, truecolor terminal) and installs the
// JetBrainsMono Nerd Font on Linux.
package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/amin-jalali/termocode/internal/fetch"
	"github.com/amin-jalali/termocode/internal/lspinstall"
	"github.com/amin-jalali/termocode/internal/nerdterm"
	"github.com/amin-jalali/termocode/internal/spawn"
)

const (
	jbmURL  = "https://github.com/ryanoasis/nerd-fonts/releases/download/v3.2.1/JetBrainsMono.zip"
	jbmName = "JetBrainsMono Nerd Font"
)

// Run executes the setup checks and font install. Returns a process exit code.
func Run(args []string) int {
	for i, a := range args {
		switch a {
		case "--install":
			if i+1 >= len(args) {
				fmt.Println("usage: termocode setup --install <name>")
				return 2
			}
			return installByName(args[i+1])
		case "--test-colors":
			printColorGradient()
			return 0
		case "--test-bg":
			printBgSwatches()
			return 0
		}
	}

	fmt.Println("== termocode setup ==")
	fmt.Println()

	checkNvim()
	checkTerminal()
	if err := ensureNerdFont(); err != nil {
		fmt.Printf("  ✗ font install failed: %v\n", err)
		return 1
	}
	termFont := DetectTerminalFont(true)
	nerdName := installedNerdName(DetectNerdFont(false))
	printTerminalFont(os.Stdout, termFont, nerdName)

	fmt.Println()
	fmt.Println("== language servers ==")
	reportLanguageServers()

	fmt.Println()
	printTerminalInstructions(termFont, nerdName)
	return 0
}

// reportLanguageServers walks the shared lspinstall registry (the same
// table the in-editor "LSP: Manage Language Servers…" uses): gopls is
// auto-installed when Go is present (as setup always did), every other
// server / adapter is reported with its exact install recipe.
func reportLanguageServers() {
	for _, cat := range []lspinstall.Category{lspinstall.CategoryLSP, lspinstall.CategoryDAP} {
		if cat == lspinstall.CategoryDAP {
			fmt.Println()
			fmt.Println("== debug adapters ==")
		}
		for _, t := range lspinstall.ByCategory(cat) {
			st := lspinstall.StatusOf(t)
			switch {
			case st.State == lspinstall.Managed:
				fmt.Printf("✓ %s installed (managed: %s)\n", t.DisplayName(), st.Path)
			case st.State == lspinstall.System:
				fmt.Printf("✓ %s installed (%s)\n", t.DisplayName(), st.Path)
			case t.Name == "gopls":
				installTool(t)
			default:
				fmt.Printf("? %s missing → %s\n", t.DisplayName(), installHint(t))
			}
		}
	}
	fmt.Println()
	fmt.Println("Install any of these with `termocode setup --install <name>`")
	fmt.Println("or from the palette: LSP: Manage Language Servers… / DAP: Install Adapter…")
}

// installHint is the plan's command line, or the reason it can't run here.
func installHint(t lspinstall.Tool) string {
	dir, _ := lspinstall.ToolDir(t.Name)
	p, err := lspinstall.BuildPlan(t, dir, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err.Error()
	}
	return p.HumanCommand()
}

// installTool runs the managed install for t, printing progress. Returns
// false on failure. A missing host toolchain prints the precise hint.
func installTool(t lspinstall.Tool) bool {
	fmt.Printf("→ installing %s (%s)...\n", t.DisplayName(), installHint(t))
	out, err := lspinstall.Install(context.Background(), t, func(line string) {
		fmt.Printf("    %s\n", line)
	})
	if err != nil {
		var mt *lspinstall.MissingToolchainError
		var off *lspinstall.OfflineError
		switch {
		case errors.As(err, &mt):
			fmt.Printf("  ? skipped: %s needs %s — %s\n", t.Name, mt.Bin, mt.Hint)
		case errors.As(err, &off):
			fmt.Printf("  ✗ offline: could not reach the network to install %s\n", t.Name)
		default:
			fmt.Printf("  ✗ install failed: %v\n", err)
			if strings.TrimSpace(out) != "" {
				fmt.Println(out)
			}
		}
		return false
	}
	bin, _ := lspinstall.BinDir()
	fmt.Printf("  ✓ installed (termocode adds %s to nvim's PATH)\n", bin)
	return true
}

// installByName handles `termocode setup --install <name>`.
func installByName(name string) int {
	t, ok := lspinstall.Lookup(name)
	if !ok {
		fmt.Printf("unknown tool %q; known:", name)
		for _, t := range lspinstall.All() {
			fmt.Printf(" %s", t.Name)
		}
		fmt.Println()
		return 2
	}
	if installTool(t) {
		return 0
	}
	return 1
}

// printBgSwatches emits the four chrome backgrounds side-by-side using
// 24-bit SGR. If the four blocks look identical, your terminal is silently
// downgrading truecolor to a 256-color palette whose dark range collapses
// these shades onto the same index.
func printBgSwatches() {
	fmt.Println("Backgrounds (each block is 16 cols wide). They should be visibly different shades:")
	fmt.Println()
	swatches := []struct {
		name string
		hex  string
	}{
		{"Editor    #1C1C1C", "1c1c1c"},
		{"Sidebar   #262626", "262626"},
		{"ActivBar  #303030", "303030"},
		{"TitleBar  #3A3A3A", "3a3a3a"},
		{"StatusBr  #0087D7", "0087d7"},
	}
	for _, s := range swatches {
		var r, g, b int
		fmt.Sscanf(s.hex, "%2x%2x%2x", &r, &g, &b)
		// truecolor bg + bright fg label
		fmt.Printf("\x1b[48;2;%d;%d;%dm\x1b[38;2;220;220;220m %-32s \x1b[0m\n", r, g, b, s.name)
	}
	fmt.Println()
	fmt.Println("If the first four rows look the same shade of gray:")
	fmt.Println("  - your terminal probably lacks 24-bit color, or")
	fmt.Println("  - your monitor cannot resolve 10-unit RGB steps in the dark range.")
	fmt.Println("Try kitty / wezterm / alacritty / iTerm2 / Windows Terminal.")
}

// printColorGradient emits a truecolor gradient. If the terminal supports
// 24-bit color, the result is a smooth blend; otherwise it shows visible bands.
func printColorGradient() {
	fmt.Println("If the gradient below is smooth, your terminal supports truecolor:")
	fmt.Println()
	for i := 0; i < 77; i++ {
		r := 255 - (i * 255 / 76)
		g := i * 510 / 76
		if g > 255 {
			g = 510 - g
		}
		b := i * 255 / 76
		fmt.Printf("\x1b[48;2;%d;%d;%dm \x1b[0m", r, g, b)
	}
	fmt.Println()
	fmt.Println()
	fmt.Println("If you see distinct bands, switch to a modern terminal:")
	fmt.Println("  kitty, wezterm, ghostty, alacritty, iTerm2, Windows Terminal.")
}

func checkNvim() {
	if path, err := exec.LookPath("nvim"); err == nil {
		fmt.Printf("✓ neovim: %s\n", path)
		return
	}
	fmt.Println("✗ neovim not found.")
	switch runtime.GOOS {
	case "linux":
		fmt.Println("  Install: sudo apt install neovim")
	case "darwin":
		fmt.Println("  Install: brew install neovim")
	default:
		fmt.Println("  See https://neovim.io/")
	}
}

func checkTerminal() {
	colorterm := os.Getenv("COLORTERM")
	term := os.Getenv("TERM")
	if colorterm == "truecolor" || colorterm == "24bit" {
		fmt.Printf("✓ terminal: truecolor advertised (TERM=%s)\n", term)
		return
	}
	fmt.Printf("? terminal: COLORTERM not set (TERM=%q). This does NOT mean\n", term)
	fmt.Println("  your terminal lacks truecolor — many modern terminals (gnome-terminal,")
	fmt.Println("  ghostty, ...) support it without advertising. Test visually with:")
	fmt.Println("    termocode setup --test-colors")
	fmt.Println("  If the gradient is smooth, you have truecolor; otherwise switch to")
	fmt.Println("  kitty, wezterm, alacritty, iTerm2, or Windows Terminal.")
}

func ensureNerdFont() error {
	if hasNerdFont() {
		fmt.Println("✓ Nerd Font already installed")
		return nil
	}

	fmt.Println("✗ Nerd Font not detected.")
	if runtime.GOOS != "linux" {
		fmt.Printf("  Auto-install supported on Linux only. Install %s manually:\n", jbmName)
		fmt.Println("  " + NerdFontsURL)
		return nil
	}

	fmt.Printf("  Installing %s ...\n", jbmName)
	dest, err := InstallNerdFont(context.Background(), func(line string) { fmt.Println("    " + line) })
	if err != nil {
		return err
	}
	fmt.Printf("  ✓ installed to %s\n", dest)
	return nil
}

// NerdFontName is the Nerd Font family InstallNerdFont installs.
const NerdFontName = jbmName

// NerdFontsURL is where to get a Nerd Font by hand.
const NerdFontsURL = "https://www.nerdfonts.com/font-downloads"

// InstallNerdFont downloads JetBrainsMono Nerd Font into
// ~/.local/share/fonts/JetBrainsMonoNF and refreshes the font cache
// (Linux only). progress gets short status lines. It changes no terminal
// setting: the terminal must still be told to use the font.
func InstallNerdFont(ctx context.Context, progress func(string)) (string, error) {
	if progress == nil {
		progress = func(string) {}
	}
	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("automatic install works on Linux only; get %s from %s", jbmName, NerdFontsURL)
	}
	dest, err := installJetBrainsMonoLinux(ctx, progress)
	InvalidateNerdFontCache()
	return dest, err
}

func installJetBrainsMonoLinux(ctx context.Context, progress func(string)) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	fontDir := filepath.Join(home, ".local/share/fonts/JetBrainsMonoNF")
	if err := os.MkdirAll(fontDir, 0o755); err != nil {
		return "", err
	}

	tmp, err := os.CreateTemp("", "termocode-jbm-*.zip")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	progress("downloading...")
	last := int64(-1)
	err = fetch.Download(ctx, jbmURL, tmpPath, func(done, total int64) {
		if total <= 0 {
			return
		}
		if pct := done * 100 / total; pct/10 != last/10 {
			last = pct
			progress(fmt.Sprintf("downloading... %d%%", pct))
		}
	})
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}

	progress("extracting...")
	if err := fetch.ExtractZip(tmpPath, fontDir, fetch.FlattenSuffix(".ttf", ".otf")); err != nil {
		return "", fmt.Errorf("extract: %w", err)
	}

	progress("refreshing font cache...")
	if err := spawn.CommandContext(ctx, "fc-cache", "-f", filepath.Join(home, ".local/share/fonts")).Run(); err != nil {
		return "", fmt.Errorf("fc-cache: %w", err)
	}

	return fontDir, nil
}

// printTerminalInstructions ends setup. When the terminal already draws
// with a Nerd Font there is nothing left to do; when its font is known
// the steps were printed above; else the generic list helps.
func printTerminalInstructions(term nerdterm.Info, nerd string) {
	fmt.Println("== final step ==")
	if term.Nerd {
		fmt.Println("Your terminal already draws Nerd Font icons. Run `termocode` to launch.")
		return
	}
	if term.Known() {
		fmt.Printf("Set your terminal font to \"%s\" (steps above), then run `termocode`.\n", nerd)
		fmt.Println("Until then termocode shows Unicode icons, which work in any font.")
		return
	}
	fmt.Printf("Configure your terminal to use \"%s\".\n", nerd)
	fmt.Println()
	fmt.Println("  gnome-terminal:    Preferences → Profiles → Text → Custom font")
	fmt.Println("  alacritty:         ~/.config/alacritty/alacritty.toml → font.normal.family")
	fmt.Println("  kitty:             ~/.config/kitty/kitty.conf → font_family")
	fmt.Println("  wezterm:           ~/.config/wezterm/wezterm.lua → wezterm.font(...)")
	fmt.Println("  ghostty:           ~/.config/ghostty/config → font-family")
	fmt.Println("  iTerm2:            Settings → Profiles → Text → Font")
	fmt.Println("  Windows Terminal:  Settings → profile → Appearance → Font face")
	fmt.Println()
	fmt.Println("After that, run `termocode` to launch.")
}
