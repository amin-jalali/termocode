package app

import (
	"os/exec"
	"runtime"
	"time"
)

// defaultNowFunc is the production clock used by the recent-files hint code.
func defaultNowFunc() time.Time { return time.Now() }

// defaultOpen launches the OS file manager / handler for `path`. We pick the
// platform-correct executable and run it in the background so termocode
// stays interactive — the launched app gets stdout/stderr redirected to
// /dev/null implicitly via Run() with default fields when we don't wait.
func defaultOpen(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("cmd", "/C", "start", "", path)
	default:
		// Linux + most BSDs: xdg-open is the standard de-facto launcher.
		cmd = exec.Command("xdg-open", path)
	}
	// Start (not Run) so we don't block the UI on a spawned GUI process.
	return cmd.Start()
}

// pickerKindRecentFiles is the picker enum value for the "Open Recent File"
// overlay. Defined here (next to its consumer) since palette.go owns the
// other enum constants — keeping this with the file-manager helpers is a
// minor exception.
