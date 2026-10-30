package containers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// ── Docker Hub search and podman connections (CP-12) ─────────────────────

// SearchResult is one row of `<engine> search`.
type SearchResult struct {
	Name        string // the reference to pull by ("nginx", "bitnami/nginx")
	Description string // control characters removed, at most 200 runes
	Stars       int
	Official    bool
}

// DefaultSearchLimit is how many results a search asks for.
const DefaultSearchLimit = 25

// ParseSearch reads `search --format '{{json .}}'` lines from docker
// (Name, StarCount, IsOfficial as strings) and podman (Name with the
// registry host, Stars as a number, Official "[OK]"). A name that is not
// a plain repository is dropped.
func ParseSearch(data []byte) ([]SearchResult, error) {
	var out []SearchResult
	err := parseLines(data, func(raw map[string]json.RawMessage) error {
		repo, err := ParseRemoteRepo(firstString(raw, "Name", "name"))
		if err != nil {
			return nil
		}
		off := strings.ToLower(firstString(raw, "IsOfficial", "Official", "is_official"))
		out = append(out, SearchResult{
			Name:        repo.Ref(),
			Description: cleanText(firstString(raw, "Description", "description"), 200),
			Stars:       firstInt(raw, "StarCount", "Stars", "star_count"),
			Official:    off == "true" || off == "[ok]" || off == "ok",
		})
		return nil
	})
	return out, err
}

// ansiRe matches terminal escape sequences (CSI and OSC).
var ansiRe = lazyre.New(`\x1b(?:\[[0-9;?]*[ -/]*[@-~]|\][^\x07\x1b]*(?:\x07|\x1b\\)?)`)

// cleanText drops escape sequences and control characters and cuts s to
// max runes.
func cleanText(s string, max int) string {
	s = ansiRe.ReplaceAllString(s, "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	if r := []rune(s); len(r) > max {
		s = string(r[:max-1]) + "…"
	}
	return strings.TrimSpace(s)
}

// ValidSearchTerm accepts a search word: 1..100 characters of letters,
// digits and "._-/:", not starting with "-" and without "..".
func ValidSearchTerm(term string) error {
	if term == "" {
		return errors.New("type a word to search for, like postgres")
	}
	if len(term) > 100 {
		return errors.New("the search word is too long (max 100)")
	}
	if strings.HasPrefix(term, "-") {
		return errors.New("the search word cannot start with a dash")
	}
	if strings.Contains(term, "..") {
		return errors.New("the search word cannot contain ..")
	}
	for _, r := range term {
		if !(r < unicode.MaxASCII && (unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune("._-/:", r))) {
			return errors.New("use letters, digits and . _ - / : only")
		}
	}
	return nil
}

// Search runs `<engine> search --format {{json .}} --limit N --no-trunc
// <term>` (the engine asks Docker Hub; no HTTP of ours). limit is clamped
// to 1..100.
func (e *Engine) Search(ctx context.Context, term string, limit int) ([]SearchResult, error) {
	term = strings.ToLower(strings.TrimSpace(term))
	if err := ValidSearchTerm(term); err != nil {
		return nil, err
	}
	limit = min(max(limit, 1), 100)
	out, stderr, code, err := e.captureTimeout(ctx, 30*time.Second, "search", "--format", "{{json .}}", "--limit", strconv.Itoa(limit), "--no-trunc", term)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		msg := firstLine(stderr)
		if msg == "" {
			msg = fmt.Sprintf("search exited %d", code)
		}
		return nil, errors.New(cleanText(msg, 300))
	}
	return ParseSearch([]byte(out))
}

// ParseConnections reads `podman system connection ls --format json`: a
// JSON array (podman 4 / 5) or JSON lines (older). Each connection is a
// Context (Current = the default one). The identity file path is not
// kept.
func ParseConnections(data []byte) ([]Context, error) {
	data = bytes.TrimSpace(data)
	var rows []map[string]json.RawMessage
	switch {
	case len(data) == 0 || string(data) == "null":
		return nil, nil
	case data[0] == '[':
		if err := json.Unmarshal(data, &rows); err != nil {
			return nil, err
		}
	default:
		if err := parseLines(data, func(raw map[string]json.RawMessage) error {
			rows = append(rows, raw)
			return nil
		}); err != nil {
			return nil, err
		}
	}
	var out []Context
	for _, raw := range rows {
		name := firstString(raw, "Name", "name")
		if name == "" {
			continue
		}
		uri := firstString(raw, "URI", "Uri", "uri", "URL")
		c := Context{Name: name, Current: truthy(firstString(raw, "Default", "default", "IsDefault")), DockerEndpoint: uri, Host: uri}
		if truthy(firstString(raw, "IsMachine", "is_machine")) {
			c.Description = "podman machine"
		}
		out = append(out, c)
	}
	return out, nil
}

// ListConnections runs `podman system connection ls --format json`. It
// reads podman's own files: it never carries the session connection.
func (e *Engine) ListConnections(ctx context.Context) ([]Context, error) {
	plain := *e
	plain.Context = ""
	return parseList(ctx, &plain, []string{"system", "connection", "ls", "--format", "json"}, ParseConnections)
}
