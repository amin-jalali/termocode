package specui

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/testkit"
)

// sidebarEdge is the screen column of the sidebar's right divider (the
// first "▕" on the editor's first text row), or -1 without a sidebar.
func sidebarEdge(d *testkit.Driver) int {
	x, _, ok := locate(d, "▕    1  ")
	if !ok {
		return -1
	}
	return x
}

// Spec: Given the editor with the sidebar,
// when the user presses Ctrl+B twice,
// then the sidebar hides (the editor starts at the left) and comes back.
func TestSidebarToggle(t *testing.T) {
	d := editorWith(t, nil, "main.go", "fmt.Println")
	mustSee(t, d, "EXPLORER")
	d.Key("ctrl+b")
	mustNotSee(t, d, "EXPLORER")
	if !strings.Contains(screen(d), "fmt.Println") {
		t.Fatalf("editor gone with the sidebar:\n%s", screen(d))
	}
	d.Key("ctrl+b")
	mustSee(t, d, "EXPLORER")
}

// Spec: Given the editor with sidebar and status bar,
// when the user toggles Zen mode (Alt+Z),
// then the chrome hides and the code stays; Alt+Z brings it all back.
func TestZenModeHidesChrome(t *testing.T) {
	d := editorWith(t, nil, "main.go", "fmt.Println")
	d.Key("alt+z")
	mustNotSee(t, d, "EXPLORER")
	if s := screen(d); !strings.Contains(s, "fmt.Println") || strings.Contains(s, "Ln 1,") {
		t.Errorf("zen mode: want code without sidebar / status bar:\n%s", s)
	}
	d.Key("alt+z")
	mustSee(t, d, "EXPLORER")
	mustSee(t, d, "Ln ")
}

// notifyInit makes the embedded nvim send a warning through vim.notify
// when a *.notify file is opened, as a plugin would.
const notifyInit = `
vim.api.nvim_create_autocmd('BufReadPost', { pattern = '*.notify', callback = function()
  vim.schedule(function() vim.notify('spec plugin says hi', vim.log.levels.WARN) end)
end })
`

// Spec: Given a plugin that calls vim.notify,
// when it fires,
// then the message shows as a toast (not an nvim prompt that would block
// the editor), and the toast goes away by itself.
func TestNotifyBecomesToast(t *testing.T) {
	slow(t)
	d := testkit.New(t, testkit.Options{W: 120, H: 36, Nvim: true,
		Files:  map[string]string{"x.notify": "body\n"},
		Config: map[string]string{"../nvim/init.lua": notifyInit}})
	d.Action("open:x.notify")
	mustSee(t, d, "spec plugin says hi")
	if !d.WaitFor(func() bool { return !strings.Contains(screen(d), "spec plugin says hi") }, 15e9) {
		t.Errorf("the toast never went away:\n%s", screen(d))
	}
}

