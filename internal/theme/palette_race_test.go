package theme

import (
	"sync"
	"testing"
)

// A second Model applies the theme that is already active while a command
// of the first one still renders on its own goroutine (help page, markdown
// preview). Applying the same palette again must not write the colour
// vars, so the render never races with it (run with -race).
func TestSetPaletteSamePaletteDoesNotWrite(t *testing.T) {
	t.Cleanup(func() { ApplyTheme("vscode-dark-plus") })
	th, _ := ApplyTheme("vscode-dark-plus")

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = Hex(SyntaxKeyword) + Hex(BgEditor) + Hex(TextPrimary) + Hex(Shadow)
			_ = len(BracketPairs) + BracketPairs[0]
		}
	}()
	for range 200 {
		SetPalette(th.Palette)
	}
	close(stop)
	wg.Wait()
}

// Hex reads the hex map while the user switches themes: the map is
// swapped whole, never written in place.
func TestHexWhileThemeSwitches(t *testing.T) {
	t.Cleanup(func() { ApplyTheme("vscode-dark-plus") })
	a, _ := LookupTheme("vscode-dark-plus")
	b, _ := LookupTheme("github-dark")

	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			for c := range 256 {
				_ = Hex(c)
			}
		}
	}()
	for i := range 200 {
		if i%2 == 0 {
			SetPalette(a.Palette)
		} else {
			SetPalette(b.Palette)
		}
	}
	close(stop)
	wg.Wait()
}
