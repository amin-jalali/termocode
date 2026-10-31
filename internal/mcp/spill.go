package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Big results (AI-4, AI-5, AI-6): a tool result over its limit is never
// cut silently. The whole (cleaned, redacted) text goes to a file in the
// session's spill folder; the model gets the head, the size and an id,
// and reads the rest in parts with ResultReadTool.

// ResultReadTool is the agent tool that reads a saved result in parts.
const ResultReadTool = "mcp_read_result"

// HardMaxResult caps a tool's own maxResultSizeChars (a server can not
// fill the model's context).
const HardMaxResult = 256 << 10

// resultPart is how much ResultReadTool returns by default.
const resultPart = 32 << 10

// Spill keeps the big results of one session (files 0600 in Dir, 0700).
type Spill struct {
	Dir string
	mu  sync.Mutex
	n   int
}

// NewSpill uses dir (made on the first save).
func NewSpill(dir string) *Spill { return &Spill{Dir: dir} }

var spillIDRe = regexp.MustCompile(`^r[0-9]+-[A-Za-z0-9_-]{1,80}$`)

// save writes text and returns its id.
func (s *Spill) save(server, tool, text string) (string, error) {
	s.mu.Lock()
	s.n++
	id := fmt.Sprintf("r%d-%s", s.n, wireSafe(server+"-"+tool))
	s.mu.Unlock()
	if len(id) > 80 {
		id = id[:80]
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return "", err
	}
	return id, os.WriteFile(filepath.Join(s.Dir, id+".txt"), []byte(text), 0o600)
}

// read returns length bytes of a saved result from offset.
func (s *Spill) read(id string, offset, length int) (string, error) {
	if !spillIDRe.MatchString(id) {
		return "", fmt.Errorf("no saved result %q", id)
	}
	b, err := os.ReadFile(filepath.Join(s.Dir, id+".txt"))
	if err != nil {
		return "", fmt.Errorf("no saved result %q", id)
	}
	if length <= 0 || length > resultPart {
		length = resultPart
	}
	if offset < 0 || offset > len(b) {
		return "", fmt.Errorf("offset %d is past the end (%d bytes)", offset, len(b))
	}
	end := min(offset+length, len(b))
	for end < len(b) && !utf8.RuneStart(b[end]) {
		end--
	}
	part := string(b[offset:end])
	if end >= len(b) {
		return part + fmt.Sprintf("\n[bytes %d–%d of %d: end of the result]", offset, end, len(b)), nil
	}
	return part + fmt.Sprintf("\n[bytes %d–%d of %d; next offset %d]", offset, end, len(b), end), nil
}

// Tool is the agent tool that reads saved results (read only).
func (s *Spill) Tool() ai.Tool {
	return ai.Tool{
		Name:        ResultReadTool,
		Display:     "mcp.read_result",
		Description: "Read a part of a big MCP tool result that was saved to a file (the result names its id).",
		Params: map[string]any{
			"id":     map[string]any{"type": "string", "description": "the saved result's id"},
			"offset": map[string]any{"type": "integer", "description": "byte offset to start at (default 0)"},
			"length": map[string]any{"type": "integer", "description": fmt.Sprintf("bytes to read (default and most %d)", resultPart)},
		},
		Required: []string{"id"},
		Describe: func(a map[string]any) string { return "Read MCP result " + ai.ArgString(a, "id") },
		Run: func(_ context.Context, a map[string]any) (string, error) {
			return s.read(ai.ArgString(a, "id"), ai.ArgInt(a, "offset"), ai.ArgInt(a, "length"))
		},
	}
}

// MaxResultSize is the tool's own result limit from its _meta
// ("maxResultSizeChars", also with a vendor prefix "x/maxResultSizeChars"),
// at most HardMaxResult; 0 when it sends none.
func (t Tool) MaxResultSize() int {
	for k, v := range t.Meta {
		if k != "maxResultSizeChars" && !strings.HasSuffix(k, "/maxResultSizeChars") {
			continue
		}
		if f, ok := v.(float64); ok && f >= 1 {
			if f > HardMaxResult {
				return HardMaxResult
			}
			return int(f)
		}
	}
	return 0
}

// cutUTF8 cuts s to at most n bytes on a rune boundary.
func cutUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// bigResult is the text the model gets for a result over limit.
func bigResult(o AdapterOptions, server, tool, text string, limit int) string {
	if o.Warn != nil {
		o.Warn(server, tool, len(text))
	}
	head := cutUTF8(text, limit)
	if o.Spill == nil {
		return head + fmt.Sprintf("\n… [truncated, %d more bytes]", len(text)-len(head))
	}
	id, err := o.Spill.save(server, tool, text)
	if err != nil {
		return head + fmt.Sprintf("\n… [truncated, %d more bytes; saving the rest failed: %v]", len(text)-len(head), err)
	}
	return head + fmt.Sprintf("\n… [the result is %d bytes; this is the first %d. The whole result is saved as id %q: call %s with that id and offset %d to read on]",
		len(text), len(head), id, ResultReadTool, len(head))
}
