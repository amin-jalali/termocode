package app

import (
	"path/filepath"
	"regexp"

	"github.com/amin-jalali/termocode/internal/toast"
)

// nvimWriteLine matches nvim's raw write message: `"path" [New] 9L, 81B
// written` (or "3 lines, 20 bytes written" with shortmess changed).
var nvimWriteLine = regexp.MustCompile(`^"(.+)"(?: \[[^\]]*\])* .*\bwritten$`)

// nvimPromptToast turns nvim's last message into a toast: its raw write
// line becomes a short clean "Saved <name>" note (WEBP-10: the web showed
// the full path and the line/byte counts); any other message is a warning
// as written.
func nvimPromptToast(text string) (toast.Severity, string) {
	if m := nvimWriteLine.FindStringSubmatch(text); m != nil {
		return toast.Info, "Saved " + filepath.Base(m[1])
	}
	return toast.Warn, text
}
