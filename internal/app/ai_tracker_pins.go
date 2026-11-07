package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Pinned tracker blocks (MOB-13): the AI pins a read-only MCP tool call
// ("my open Linear issues") to the phone's Home with tracker_pin, which
// always asks on the laptop (Deny first, no "Always allow"). Pins live in
// the laptop's config dir (phone-tracker-pins.json, 0600) and are shared
// by every paired phone. A pin re-runs on the laptop only while its tool
// still runs at once (read-only, trusted server, not off, in the chat's
// MCP profile, tools not changed): the same rules as the chat, so a pin
// can never run what the AI could not run without asking. Arguments that
// look like a key are refused; results reach the phone as rows, redacted.

const trackerPinsFile = "phone-tracker-pins.json"

// trackerPin is one stored pin.
type trackerPin struct {
	ID      string         `json:"id"`
	Title   string         `json:"title"`
	Server  string         `json:"server"`
	Tool    string         `json:"tool"`
	Args    map[string]any `json:"args,omitempty"`
	Refresh int            `json:"refresh"`
	Created time.Time      `json:"created"`
}

// trackerMu guards the pins file (the AI tool runs off the UI thread).
var trackerMu sync.Mutex

func trackerPinsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, trackerPinsFile), nil
}

// trackerLoad reads the pins; a missing file is no pins. Pins that do not
// pass the checks (a hand edit) are dropped.
func trackerLoad() ([]trackerPin, error) {
	p, err := trackerPinsPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var pins []trackerPin
	if err := json.Unmarshal(b, &pins); err != nil {
		return nil, fmt.Errorf("%s: %w", trackerPinsFile, err)
	}
	kept := pins[:0]
	for _, pin := range pins {
		if trackerValidID(pin.ID) && pin.Server != "" && pin.Tool != "" && aiArgsSecretError(pin.Args) == nil {
			kept = append(kept, pin)
		}
		if len(kept) == webproto.MaxTrackerPins {
			break
		}
	}
	return kept, nil
}

// trackerSave writes the pins atomically, private to the user.
func trackerSave(pins []trackerPin) error {
	p, err := trackerPinsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(pins, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// trackerValidID is the page's check of a pin id (12 lower hex).
func trackerValidID(id string) bool {
	if len(id) != webproto.TrackerIDLen {
		return false
	}
	for _, r := range id {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func trackerNewID() string {
	var b [webproto.TrackerIDLen / 2]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// trackerCheck says whether server.tool may run without asking now, and
// why not. It reads cached lists only (it never starts a server).
func trackerCheck(mgr *mcp.Manager, prof *mcp.Profile, server, tool string) (mcp.Tool, string) {
	set := mcp.LoadSettings(mcpConfigPath())
	if !set.Enabled {
		return mcp.Tool{}, "MCP is off on the laptop."
	}
	if mgr == nil {
		return mcp.Tool{}, "MCP has not started on the laptop yet."
	}
	var st *mcp.Status
	for _, s := range mgr.Servers() {
		if s.Name == server {
			st = &s
			break
		}
	}
	switch {
	case st == nil:
		return mcp.Tool{}, "There is no MCP server " + server + " on the laptop."
	case st.State == mcp.StateDisabled || st.State == mcp.StateInvalid:
		return mcp.Tool{}, "The server " + server + " is off on the laptop."
	case st.State == mcp.StateSignIn:
		return mcp.Tool{}, "Sign in to " + server + " on the laptop first."
	case !st.Trusted:
		return mcp.Tool{}, "Trust the server " + server + " on the laptop first."
	case st.ToolsChanged:
		return mcp.Tool{}, "The tools of " + server + " changed: review them on the laptop."
	}
	srv, tools, _ := mgr.KnownTools(server)
	for _, t := range tools {
		if t.Name != tool {
			continue
		}
		if set.ToolOff(server, tool) {
			return t, "The tool is off on the laptop."
		}
		if prof != nil && !prof.Allows(server, tool) {
			return t, "The chat's MCP profile leaves this tool out."
		}
		if exposed, mutating, _ := mcp.Gate(srv, t, set.TrustAnnotations); !exposed || mutating {
			return t, "Only a tool that only reads and runs at once can be pinned."
		}
		return t, ""
	}
	return mcp.Tool{}, "The server " + server + " does not list the tool " + tool + "."
}

// trackerArgsText is a pin's arguments as one short line, redacted.
func trackerArgsText(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	b, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	return catalogLine(string(b))
}

// trackerView is a stored pin as the phone sees it.
func trackerView(mgr *mcp.Manager, prof *mcp.Profile, p trackerPin) webproto.TrackerPin {
	_, why := trackerCheck(mgr, prof, p.Server, p.Tool)
	return webproto.TrackerPin{ID: p.ID, Title: webText(p.Title), Server: webName(p.Server), Tool: webName(p.Tool),
		Args: webText(trackerArgsText(p.Args)), Refresh: p.Refresh, Runs: why == "", Why: webText(why)}
}

// trackerRun runs a pin's tool on the laptop (no trust question: an
// untrusted server is refused) and turns the result into rows.
func trackerRun(ctx context.Context, mgr *mcp.Manager, prof *mcp.Profile, p trackerPin) webproto.TrackerBlock {
	out := webproto.TrackerBlock{ID: p.ID, Title: webText(p.Title), Rows: []webproto.TrackerRow{}, At: time.Now().UnixMilli()}
	if _, why := trackerCheck(mgr, prof, p.Server, p.Tool); why != "" {
		out.Error = webText(why)
		return out
	}
	if hits := mcp.ScanSecrets(p.Args); len(hits) > 0 {
		out.Error = "An argument looks like a key. Pin it again without it."
		return out
	}
	ctx, cancel := context.WithTimeout(ctx, webproto.TrackerRunTimeout)
	defer cancel()
	res, err := mgr.Call(ctx, nil, p.Server, p.Tool, p.Args)
	if err != nil {
		out.Error = webText(catalogLine(err.Error()))
		return out
	}
	if res.IsError {
		out.Error = webText(catalogLine(strings.TrimPrefix(res.Text(), "error: ")))
		return out
	}
	trackerRows(&out, res)
	return out
}

// trackerRows reads the list in a result: the first array in the
// structured content (or the JSON text) whose items are objects, one row
// per item; else the text, one row per line.
func trackerRows(out *webproto.TrackerBlock, res mcp.CallResult) {
	var v any
	if len(res.StructuredContent) > 0 && json.Unmarshal(res.StructuredContent, &v) == nil {
		if trackerList(out, v) {
			return
		}
	}
	text := res.UserText()
	if text == "" {
		text = res.Text()
	}
	if t := strings.TrimSpace(text); strings.HasPrefix(t, "[") || strings.HasPrefix(t, "{") {
		if json.Unmarshal([]byte(t), &v) == nil && trackerList(out, v) {
			return
		}
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(out.Rows) == webproto.MaxTrackerRows {
			out.Truncated = true
			break
		}
		out.Rows = append(out.Rows, webproto.TrackerRow{Title: webText(catalogLine(line))})
	}
	if len(out.Rows) == 0 {
		out.Text = "Nothing to show."
	}
}

// trackerKeys are the item fields read for a row, in the order tried.
var (
	trackerTitleKeys = []string{"title", "name", "summary", "subject", "text", "identifier", "key", "id"}
	trackerHintKeys  = []string{"state", "status", "priority", "assignee", "updatedAt", "updated_at", "due", "dueDate"}
	trackerRefKeys   = []string{"identifier", "key", "number", "id"}
	trackerListKeys  = []string{"items", "issues", "results", "data", "nodes", "tasks", "tickets", "records"}
)

// trackerList fills rows from v when it is (or holds) a list of objects.
func trackerList(out *webproto.TrackerBlock, v any) bool {
	list, ok := v.([]any)
	if !ok {
		obj, isObj := v.(map[string]any)
		if !isObj {
			return false
		}
		keys := append([]string{}, trackerListKeys...)
		var rest []string
		for k := range obj {
			rest = append(rest, k)
		}
		sort.Strings(rest)
		for _, k := range append(keys, rest...) {
			if l, ok := obj[k].([]any); ok && len(l) > 0 {
				if _, isItem := l[0].(map[string]any); isItem {
					list = l
					break
				}
			}
		}
		if list == nil {
			return false
		}
	}
	n := 0
	for _, it := range list {
		item, ok := it.(map[string]any)
		if !ok {
			continue
		}
		if n == webproto.MaxTrackerRows {
			out.Truncated = true
			break
		}
		n++
		row := webproto.TrackerRow{Title: trackerField(item, trackerTitleKeys)}
		if row.Title == "" {
			row.Title = "(no title)"
		}
		var hints []string
		for _, k := range trackerHintKeys {
			if h := trackerField(item, []string{k}); h != "" && len(hints) < 3 {
				hints = append(hints, h)
			}
		}
		row.Hint = strings.Join(hints, " · ")
		row.Ref = trackerField(item, trackerRefKeys)
		row.Title, row.Hint, row.Ref = webText(catalogLine(row.Title)), webText(catalogLine(row.Hint)), webText(catalogLine(row.Ref))
		out.Rows = append(out.Rows, row)
	}
	return n > 0
}

// trackerField is the first of keys that holds a short scalar (an object
// with a name or title counts: an assignee {name}).
func trackerField(item map[string]any, keys []string) string {
	for _, k := range keys {
		switch x := item[k].(type) {
		case string:
			if x != "" {
				return x
			}
		case float64:
			return fmt.Sprintf("%v", x)
		case bool:
			return fmt.Sprintf("%v", x)
		case map[string]any:
			for _, nk := range []string{"name", "title", "displayName", "login"} {
				if s, ok := x[nk].(string); ok && s != "" {
					return s
				}
			}
		}
	}
	return ""
}

// ── The AI tool ─────────────────────────────────────────────────────────

// aiTrackerTools is tracker_pin: the AI pins a read-only MCP call to the
// phone's Home. It always asks (class "": no "Always allow"). prof is the
// chat's MCP profile name when the turn started ("" for all).
func aiTrackerTools(profName string) []ai.Tool {
	prof := func() *mcp.Profile {
		if profName == "" {
			return nil
		}
		ps, _ := mcpLoadProfiles()
		if p, ok := mcp.FindProfile(ps, profName); ok {
			return &p
		}
		return nil
	}
	parse := func(args map[string]any) (trackerPin, error) {
		p := trackerPin{Refresh: 300}
		p.Server, _ = args["server"].(string)
		p.Tool, _ = args["tool"].(string)
		p.Title, _ = args["title"].(string)
		p.Title = strings.TrimSpace(p.Title)
		if a, ok := args["args"].(map[string]any); ok {
			p.Args = a
		}
		if r, ok := args["refresh_seconds"].(float64); ok {
			p.Refresh = int(r)
		}
		switch {
		case p.Server == "" || p.Tool == "":
			return p, errors.New("name the MCP server and the tool (as in mcp.<server>.<tool>)")
		case p.Title == "" || utf8.RuneCountInString(p.Title) > webproto.MaxTrackerTitle:
			return p, fmt.Errorf("give a title of 1 to %d characters", webproto.MaxTrackerTitle)
		case p.Refresh != 0 && p.Refresh < webproto.TrackerRefreshMinSec:
			return p, fmt.Errorf("refresh_seconds is 0 (only on tap) or at least %d", webproto.TrackerRefreshMinSec)
		}
		if err := aiArgsSecretError(p.Args); err != nil {
			return p, err
		}
		if b, _ := json.Marshal(p.Args); len(b) > 4<<10 {
			return p, errors.New("the arguments are too long (at most 4 KiB)")
		}
		if _, why := trackerCheck(mcpTarget.Load(), prof(), p.Server, p.Tool); why != "" {
			return p, errors.New(why)
		}
		return p, nil
	}
	return []ai.Tool{{
		Name: "tracker_pin",
		Description: "Pin a read-only MCP tool call (for example a tracker's open issues) to the phone's Home as a live block. " +
			"The laptop re-runs it while the phone's Home is open. Only tools that only read and run at once can be pinned. The user is always asked.",
		Params: map[string]any{
			"server":          map[string]any{"type": "string", "description": "The MCP server name"},
			"tool":            map[string]any{"type": "string", "description": "The tool name on that server (not the mcp__ wire name)"},
			"args":            map[string]any{"type": "object", "description": "The tool's arguments; never keys or tokens"},
			"title":           map[string]any{"type": "string", "description": "A short title for the block, at most 60 characters"},
			"refresh_seconds": map[string]any{"type": "integer", "description": "Seconds between re-runs (0: only on tap; at least 60; default 300)"},
		},
		Required: []string{"server", "tool", "title"},
		Mutating: true, // Class "": always asks, no "Always allow"
		Describe: func(args map[string]any) string {
			p, _ := parse(args)
			return fmt.Sprintf("Pin %q (%s) to the phone's Home?", p.Title, mcp.DisplayName(p.Server, p.Tool))
		},
		Validate: func(args map[string]any) error {
			_, err := parse(args)
			return err
		},
		Run: func(ctx context.Context, args map[string]any) (string, error) {
			p, err := parse(args)
			if err != nil {
				return "", err
			}
			trackerMu.Lock()
			defer trackerMu.Unlock()
			pins, err := trackerLoad()
			if err != nil {
				return "", err
			}
			if len(pins) >= webproto.MaxTrackerPins {
				return "", fmt.Errorf("the phone has %d pins already: the user unpins one first", webproto.MaxTrackerPins)
			}
			p.ID, p.Created = trackerNewID(), time.Now().UTC()
			if err := trackerSave(append(pins, p)); err != nil {
				return "", err
			}
			return fmt.Sprintf("Pinned %q to the phone's Home.", p.Title), nil
		},
	}}
}
