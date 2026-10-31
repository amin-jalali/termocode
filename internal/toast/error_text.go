package toast

import (
	"regexp"
	"strings"

	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
)

// Recognize file-operation errors without rearranging ordinary sentences.
// Keep the original Toast detail intact for logs and other screens.
var pathErrorText = regexp.MustCompile(`^(.*?\b(?:open|stat|lstat|read|write|mkdir|chmod|chown|remove|unlink|rename|link|symlink|readdir) )(.+): ([^:\n]+)$`)

func errorDetailText(text string, width int) string {
	parts := pathErrorText.FindStringSubmatch(text)
	if parts == nil {
		return runewidth.Truncate(text, width, "…")
	}
	reason := parts[3]
	prefix := reason + " · " + parts[1]
	room := width - runewidth.StringWidth(prefix)
	if room < 3 {
		return runewidth.Truncate(reason, width, "…")
	}
	return prefix + middlePath(parts[2], room)
}

// middlePath keeps both ends of a path, including its file name when it fits.
func middlePath(path string, width int) string {
	if runewidth.StringWidth(path) <= width {
		return path
	}
	r := []rune(path)
	suffix := path
	if i := strings.LastIndexAny(path, `/\`); i >= 0 {
		suffix = path[i:]
	}
	tail := []rune(suffix)
	budget := width - 1
	// Reserve some root context unless the file name needs all the room.
	tailBudget := budget
	if runewidth.StringWidth(string(tail)) > budget {
		tailBudget = budget * 2 / 3
	}
	for len(tail) > 0 && runewidth.StringWidth(string(tail)) > tailBudget {
		tail = tail[1:]
	}
	head := runewidth.Truncate(string(r), budget-runewidth.StringWidth(string(tail)), "")
	return head + "…" + string(tail)
}
