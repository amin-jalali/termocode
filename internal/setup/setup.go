// Package setup implements the `termocode setup` subcommand: it verifies
// runtime prerequisites (neovim, truecolor terminal) and installs the
// JetBrainsMono Nerd Font on Linux.
package setup

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	jbmURL  = "https://github.com/ryanoasis/nerd-fonts/releases/download/v3.2.1/JetBrainsMono.zip"
	jbmName = "JetBrainsMono Nerd Font"
)

// Run executes the setup checks and font install. Returns a process exit code.
func Run(args []string) int {
	for _, a := range args {
		switch a {
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

	fmt.Println()
	fmt.Println("== language servers ==")
	ensureGopls()
	reportOtherLSPs()

	fmt.Println()
	printTerminalInstructions()
	return 0
}

func ensureGopls() {
	if _, err := exec.LookPath("gopls"); err == nil {
		fmt.Println("✓ gopls (Go LSP) installed")
		return
	}
	if _, err := exec.LookPath("go"); err != nil {
		fmt.Println("? gopls not installed; install Go first, then run setup again")
		return
	}
	fmt.Println("✗ gopls missing; installing via `go install`...")
	c := exec.Command("go", "install", "golang.org/x/tools/gopls@latest")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		fmt.Printf("  ✗ install failed: %v\n", err)
		return
	}
	fmt.Println("  ✓ installed (ensure $HOME/go/bin is on PATH)")
}

func reportOtherLSPs() {
	lsps := []struct {
		bin, label, hint string
	}{
		{"pyright-langserver", "pyright (Python)", "npm i -g pyright"},
		{"pylsp", "python-lsp-server (Python)", "pipx install python-lsp-server"},
		{"typescript-language-server", "ts_ls (JS/TS)", "npm i -g typescript typescript-language-server"},
		{"rust-analyzer", "rust-analyzer (Rust)", "rustup component add rust-analyzer"},
		{"clangd", "clangd (C/C++)", "apt install clangd"},
		{"lua-language-server", "lua_ls (Lua)", "https://github.com/LuaLS/lua-language-server"},
	}
	for _, l := range lsps {
		if _, err := exec.LookPath(l.bin); err == nil {
			fmt.Printf("✓ %s installed\n", l.label)
		} else {
			fmt.Printf("? %s missing → %s\n", l.label, l.hint)
		}
	}
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
		fmt.Println("  https://www.nerdfonts.com/font-downloads")
		return nil
	}

	fmt.Printf("  Installing %s ...\n", jbmName)
	dest, err := installJetBrainsMonoLinux()
	if err != nil {
		return err
	}
	fmt.Printf("  ✓ installed to %s\n", dest)
	return nil
}

func hasNerdFont() bool {
	out, err := exec.Command("fc-list").Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), "nerd")
}

func installJetBrainsMonoLinux() (string, error) {
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

	fmt.Println("    downloading...")
	if err := download(jbmURL, tmpPath); err != nil {
		return "", fmt.Errorf("download: %w", err)
	}

	fmt.Println("    extracting...")
	if err := unzipTTF(tmpPath, fontDir); err != nil {
		return "", fmt.Errorf("extract: %w", err)
	}

	fmt.Println("    refreshing font cache...")
	if err := exec.Command("fc-cache", "-f", filepath.Join(home, ".local/share/fonts")).Run(); err != nil {
		return "", fmt.Errorf("fc-cache: %w", err)
	}

	return fontDir, nil
}

func download(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

func unzipTTF(zipPath, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".ttf") &&
			!strings.HasSuffix(strings.ToLower(f.Name), ".otf") {
			continue
		}
		if err := extractOne(f, dest); err != nil {
			return err
		}
	}
	return nil
}

func extractOne(f *zip.File, dest string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	target := filepath.Join(dest, filepath.Base(f.Name))
	out, err := os.Create(target)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

func printTerminalInstructions() {
	fmt.Println("== final step ==")
	fmt.Printf("Configure your terminal to use \"%s\".\n", jbmName)
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
