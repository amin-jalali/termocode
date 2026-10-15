package setup

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// A separate GNOME Terminal profile that uses a Nerd Font. It is made
// only when the user presses "Create termocode profile" on the setup
// card, after a preview of the plan. The user's default profile and the
// default-profile setting are never touched.

// GnomeProfileName is the visible name of the profile termocode makes.
const GnomeProfileName = "termocode"

const (
	gnomeListSchema    = "org.gnome.Terminal.ProfilesList"
	gnomeProfileSchema = "org.gnome.Terminal.Legacy.Profile:/org/gnome/terminal/legacy/profiles:/:"
)

// Gsettings runs `gsettings args...` and returns its output.
type Gsettings func(ctx context.Context, args ...string) (string, error)

// RunGsettings runs the real gsettings tool.
func RunGsettings(ctx context.Context, args ...string) (string, error) {
	cmd := spawn.CommandContext(ctx, "gsettings", args...)
	cmd.WaitDelay = 100 * time.Millisecond
	out, err := cmd.Output()
	return string(out), err
}

// GnomeProfilePlan is what creating the profile will do.
type GnomeProfilePlan struct {
	Name   string   // visible name ("termocode")
	UUID   string   // the new profile's id
	Font   string   // Pango font, e.g. "JetBrainsMono Nerd Font 13"
	List   []string // the profile list after the change
	Exists bool     // a profile with Name is there already: nothing to do
}

// ProfilePath is the dconf path of the new profile.
func (p GnomeProfilePlan) ProfilePath() string {
	return "/org/gnome/terminal/legacy/profiles:/:" + p.UUID + "/"
}

// Settings are the keys set on the NEW profile, in order.
func (p GnomeProfilePlan) Settings() [][2]string {
	return [][2]string{
		{"visible-name", quoteGV(p.Name)},
		{"use-system-font", "false"},
		{"font", quoteGV(p.Font)},
	}
}

// Commands are the exact gsettings calls ApplyGnomeProfile runs, in
// order. The list change comes last, so the profile has its keys when it
// appears in the menu.
func (p GnomeProfilePlan) Commands() [][]string {
	var out [][]string
	for _, kv := range p.Settings() {
		out = append(out, []string{"set", gnomeProfileSchema + p.UUID + "/", kv[0], kv[1]})
	}
	return append(out, []string{"set", gnomeListSchema, "list", gvList(p.List)})
}

// FindGnomeProfile returns the id of the profile called name ("" = none).
// Read-only.
func FindGnomeProfile(ctx context.Context, gs Gsettings, name string) (string, error) {
	ids, err := gnomeProfileIDs(ctx, gs)
	if err != nil {
		return "", err
	}
	for _, id := range ids {
		out, err := gs(ctx, "get", gnomeProfileSchema+id+"/", "visible-name")
		if err == nil && unquoteGV(out) == name {
			return id, nil
		}
	}
	return "", nil
}

func gnomeProfileIDs(ctx context.Context, gs Gsettings) ([]string, error) {
	out, err := gs(ctx, "get", gnomeListSchema, "list")
	if err != nil {
		return nil, fmt.Errorf("gsettings: %w", err)
	}
	return parseGVList(out), nil
}

// PlanGnomeProfile reads the profile list (read-only) and plans a new
// profile GnomeProfileName that uses family at size (0 = 12). newID makes
// the id (nil = a random UUID).
func PlanGnomeProfile(ctx context.Context, gs Gsettings, family string, size int, newID func() string) (GnomeProfilePlan, error) {
	if size <= 0 {
		size = 12
	}
	p := GnomeProfilePlan{Name: GnomeProfileName, Font: family + " " + strconv.Itoa(size)}
	ids, err := gnomeProfileIDs(ctx, gs)
	if err != nil {
		return p, err
	}
	if id, err := FindGnomeProfile(ctx, gs, p.Name); err == nil && id != "" {
		p.UUID, p.List, p.Exists = id, ids, true
		return p, nil
	}
	if newID == nil {
		newID = newUUID
	}
	p.UUID = newID()
	p.List = append(append([]string(nil), ids...), p.UUID)
	return p, nil
}

// ApplyGnomeProfile runs the plan's gsettings calls, then reads the
// profile list back: an error says the change did not stick.
func ApplyGnomeProfile(ctx context.Context, gs Gsettings, p GnomeProfilePlan) error {
	if p.Exists {
		return nil
	}
	if p.UUID == "" {
		return errors.New("no profile id")
	}
	for _, c := range p.Commands() {
		if _, err := gs(ctx, c...); err != nil {
			return fmt.Errorf("gsettings %s %s: %w", c[0], c[2], err)
		}
	}
	id, err := FindGnomeProfile(ctx, gs, p.Name)
	if err != nil {
		return err
	}
	if id != p.UUID {
		return errors.New("gsettings did not keep the new profile")
	}
	return nil
}

// FontSize is the trailing point size of a Pango font name
// ("Ubuntu Mono 13" → 13), 0 when there is none.
func FontSize(font string) int {
	f := strings.Fields(font)
	if len(f) < 2 {
		return 0
	}
	n, err := strconv.Atoi(f[len(f)-1])
	if err != nil || n <= 0 || n > 72 {
		return 0
	}
	return n
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// quoteGV writes s as a GVariant string.
func quoteGV(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "'", `\'`) + "'"
}

func unquoteGV(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		s = s[1 : len(s)-1]
		s = strings.ReplaceAll(strings.ReplaceAll(s, `\'`, "'"), `\\`, `\`)
	}
	return s
}

// parseGVList reads "['a', 'b']" (or "@as []").
func parseGVList(s string) []string {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "@as"))
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "["), "]"))
	var out []string
	for _, part := range strings.Split(s, ",") {
		if v := unquoteGV(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func gvList(items []string) string {
	q := make([]string, len(items))
	for i, it := range items {
		q[i] = quoteGV(it)
	}
	return "[" + strings.Join(q, ", ") + "]"
}
