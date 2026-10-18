package migrate

import (
	"strings"

	"github.com/amin-jalali/termocode/internal/theme"
)

// ThemeNames maps source theme labels to built-in theme IDs.
var ThemeNames = map[string]string{
	"dark+ (default dark)": "vscode-dark-plus", "default dark modern": "vscode-dark-plus", "vscode dark+": "vscode-dark-plus",
	"github dark": "github-dark", "one dark pro": "one-dark", "atom one dark": "one-dark",
	"solarized dark": "solarized-dark",
}

func mapTheme(name string) (string, string) {
	if id := ThemeNames[strings.ToLower(strings.TrimSpace(name))]; id != "" {
		return id, id
	}
	for _, t := range theme.AvailableThemes() {
		if strings.EqualFold(t.Name, name) || strings.EqualFold(t.ID, name) {
			return t.ID, t.ID
		}
	}
	needle := normalizeThemeName(name)
	bestID, bestDistance := "", int(^uint(0)>>1)
	for _, t := range theme.AvailableThemes() {
		for _, candidate := range []string{t.Name, t.ID} {
			distance := themeNameDistance(needle, normalizeThemeName(candidate))
			if distance < bestDistance {
				bestID, bestDistance = t.ID, distance
			}
		}
	}
	if bestID == "" {
		bestID = "vscode-dark-plus"
	}
	return bestID, bestID + " (suggestion; closest built-in by theme name)"
}

func normalizeThemeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func themeNameDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i, ar := range a {
		current := make([]int, len(b)+1)
		current[0] = i + 1
		for j, br := range b {
			cost := 0
			if ar != br {
				cost = 1
			}
			current[j+1] = min(current[j]+1, min(previous[j+1]+1, previous[j]+cost))
		}
		previous = current
	}
	return previous[len(b)]
}
