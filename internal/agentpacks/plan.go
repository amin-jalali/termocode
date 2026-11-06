package agentpacks

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Target is an outside agent that can take a pack.
type Target struct {
	ID, Title string
	Base      string // its folder under home
	// place maps a pack file to its path under Base ("" = no place).
	place func(f File) string
}

// targets are the agents with a known folder layout.
var targets = []Target{
	{ID: "claude", Title: "Claude Code", Base: ".claude", place: func(f File) string { return f.Rel }},
	{ID: "codex", Title: "Codex", Base: ".codex", place: func(f File) string {
		switch f.Kind {
		case KindSkill:
			return f.Rel
		case KindCommand:
			return "prompts/" + filepath.Base(f.Rel)
		}
		return "" // Codex has no agent or rule files
	}},
}

// Targets lists the agents a pack can go to.
func Targets() []Target { return append([]Target(nil), targets...) }

// TargetFor finds a target by id.
func TargetFor(id string) (Target, bool) {
	for _, t := range targets {
		if t.ID == id {
			return t, true
		}
	}
	return Target{}, false
}

// Op is what a plan does to one file.
type Op string

const (
	OpAdd    Op = "add"
	OpChange Op = "change"
	OpSame   Op = "same"
)

// Change is one file of a plan.
type Change struct {
	Kind Kind
	Path string // absolute
	Op   Op
	data []byte
	old  []byte // what the preview saw (nil: no file)
}

// Plan is an install of a pack into one agent, for the preview card.
type Plan struct {
	Name, Version, Source, Commit string
	Target                        string
	Home                          string
	Changes                       []Change
	Skipped                       map[Kind]int // parts the agent has no place for
	Always, OnUse                 int          // token cost (see Pack.Tokens)
}

// PlanInstall builds the install of the parts in kinds (nil: all) of
// pack p into agent target under home. stateDir holds the undo records:
// a pack already installed into that agent is refused (its version stays
// pinned until it is undone).
func PlanInstall(p Pack, target, home, stateDir string, kinds []Kind) (Plan, error) {
	t, ok := TargetFor(target)
	if !ok {
		return Plan{}, fmt.Errorf("no pack format for %q", target)
	}
	if kinds != nil && len(kinds) == 0 {
		return Plan{}, errors.New("pick at least one part of the pack")
	}
	recs, err := Installed(stateDir)
	if err != nil {
		return Plan{}, err
	}
	for _, r := range recs {
		if r.Name == p.Name && r.Target == target {
			return Plan{}, fmt.Errorf("%s %s is installed in %s: undo it first to install another version", r.Name, r.Version, t.Title)
		}
	}
	pl := Plan{Name: p.Name, Version: p.Version, Source: p.Source, Commit: p.Commit, Target: target, Home: home, Skipped: map[Kind]int{}}
	base := filepath.Join(home, t.Base)
	for _, f := range p.Files {
		if !hasKind(kinds, f.Kind) {
			continue
		}
		rel := t.place(f)
		if rel == "" {
			pl.Skipped[f.Kind]++
			continue
		}
		a, u := fileTokens(f)
		pl.Always += a
		pl.OnUse += u
		c := Change{Kind: f.Kind, Path: filepath.Join(base, filepath.FromSlash(rel)), Op: OpAdd, data: f.Data}
		old, err := os.ReadFile(c.Path)
		switch {
		case err == nil && bytes.Equal(old, f.Data):
			c.Op, c.old = OpSame, old
		case err == nil:
			c.Op, c.old = OpChange, old
		case !errors.Is(err, os.ErrNotExist):
			return Plan{}, err
		}
		pl.Changes = append(pl.Changes, c)
	}
	if len(pl.Changes) == 0 {
		return Plan{}, fmt.Errorf("%s has nothing %s can use", p.Name, t.Title)
	}
	sort.SliceStable(pl.Changes, func(i, j int) bool {
		a, b := pl.Changes[i], pl.Changes[j]
		if (a.Op == OpChange) != (b.Op == OpChange) {
			return a.Op == OpChange
		}
		return a.Path < b.Path
	})
	return pl, nil
}

// Writes is the number of files Apply writes.
func (pl Plan) Writes() int { return len(pl.Changes) - pl.Same() }

// Same is the number of files that are already the same.
func (pl Plan) Same() int {
	n := 0
	for _, c := range pl.Changes {
		if c.Op == OpSame {
			n++
		}
	}
	return n
}

// Lines are the card lines: "~ path   (replaces your file)" first, then
// "+ path"; files that are the same are left out.
func (pl Plan) Lines() []string {
	var out []string
	for _, c := range pl.Changes {
		switch c.Op {
		case OpChange:
			out = append(out, "~ "+Short(pl.Home, c.Path)+"   (replaces your file)")
		case OpAdd:
			out = append(out, "+ "+Short(pl.Home, c.Path))
		}
	}
	return out
}

// Short writes path with ~ for home.
func Short(home, path string) string {
	if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") {
		return "~/" + filepath.ToSlash(rel)
	}
	return path
}

// RecFile is one written file of an install.
type RecFile struct {
	Path   string `json:"path"`
	Backup string `json:"backup,omitempty"` // the old file's copy ("" = there was none)
	Hash   string `json:"hash"`             // of what the install wrote
}

// Record is an install that can be undone.
type Record struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Version string    `json:"version"`
	Source  string    `json:"source,omitempty"`
	Commit  string    `json:"commit,omitempty"`
	Target  string    `json:"target"`
	Home    string    `json:"home"`
	At      time.Time `json:"at"`
	Files   []RecFile `json:"files"`
}

func hash(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func recDir(stateDir string) string { return filepath.Join(stateDir, "agentpacks") }

// safeID makes a file name of name@target.
func safeID(name, target string) string {
	var b strings.Builder
	for _, r := range name {
		if r == '-' || r == '_' || r == '.' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	s := strings.TrimLeft(b.String(), ".")
	if s == "" {
		s = "pack"
	}
	return s + "@" + target
}

// Apply writes the plan. The old files are copied to stateDir first and
// the undo record is saved before any agent file changes. When a file
// changed since the preview nothing is written. A failed write puts back
// what was already written.
func Apply(pl Plan, stateDir string) (Record, error) {
	for _, c := range pl.Changes {
		cur, err := os.ReadFile(c.Path)
		if errors.Is(err, os.ErrNotExist) {
			cur, err = nil, nil
		}
		if err != nil {
			return Record{}, err
		}
		if (c.old == nil) != (cur == nil) || !bytes.Equal(cur, c.old) {
			return Record{}, fmt.Errorf("%s changed since the preview: open the preview again", Short(pl.Home, c.Path))
		}
	}
	rec := Record{ID: safeID(pl.Name, pl.Target), Name: pl.Name, Version: pl.Version, Source: pl.Source, Commit: pl.Commit, Target: pl.Target, Home: pl.Home, At: time.Now().UTC()}
	bdir := filepath.Join(recDir(stateDir), rec.ID)
	if err := os.MkdirAll(bdir, 0o700); err != nil {
		return rec, err
	}
	for i, c := range pl.Changes {
		if c.Op == OpSame {
			continue
		}
		rf := RecFile{Path: c.Path, Hash: hash(c.data)}
		if c.Op == OpChange {
			rf.Backup = fmt.Sprintf("%d.old", i)
			if err := os.WriteFile(filepath.Join(bdir, rf.Backup), c.old, 0o600); err != nil {
				return rec, err
			}
		}
		rec.Files = append(rec.Files, rf)
	}
	if err := saveRecord(stateDir, rec); err != nil {
		return rec, err
	}
	for _, rf := range rec.Files {
		var data []byte
		for _, c := range pl.Changes {
			if c.Path == rf.Path {
				data = c.data
			}
		}
		if err := writeFile(rf.Path, data); err != nil {
			_, _ = Undo(stateDir, rec.ID)
			return rec, err
		}
	}
	return rec, nil
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if strings.HasSuffix(path, ".sh") {
		mode |= 0o100 // skill scripts keep working
	}
	tmp := path + ".termocode-tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func saveRecord(stateDir string, rec Record) error {
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(recDir(stateDir), rec.ID+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Installed lists the installs that can be undone, by name.
func Installed(stateDir string) ([]Record, error) {
	ents, err := os.ReadDir(recDir(stateDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(recDir(stateDir), e.Name()))
		if err != nil {
			return nil, err
		}
		var r Record
		if json.Unmarshal(b, &r) != nil || r.ID+".json" != e.Name() {
			continue // not ours
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// fileState: what undo does to one file now.
func (rf RecFile) state() (Op, error) {
	cur, err := os.ReadFile(rf.Path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil // gone already
	}
	if err != nil {
		return "", err
	}
	if hash(cur) != rf.Hash {
		return OpSame, nil // the user changed it: kept
	}
	if rf.Backup != "" {
		return OpChange, nil
	}
	return OpAdd, nil
}

// UndoLines are the card lines of an undo: "↺ path" (the old file comes
// back), "- path" (added by the pack: removed), "= path   (you changed
// it: kept)".
func (r Record) UndoLines() []string {
	var back, gone, kept []string
	for _, rf := range r.Files {
		op, err := rf.state()
		s := Short(r.Home, rf.Path)
		switch {
		case err != nil:
			kept = append(kept, "= "+s+"   (can not read it: kept)")
		case op == OpChange:
			back = append(back, "↺ "+s)
		case op == OpAdd:
			gone = append(gone, "- "+s)
		case op == OpSame:
			kept = append(kept, "= "+s+"   (you changed it: kept)")
		}
	}
	return append(append(back, gone...), kept...)
}

// UndoResult says what an undo did.
type UndoResult struct {
	Restored, Removed int
	Kept              []string
}

// Undo puts back the old files of install id, removes the files it added
// (and the folders that are then empty) and keeps any file the user
// changed after the install.
func Undo(stateDir, id string) (UndoResult, error) {
	var res UndoResult
	recs, err := Installed(stateDir)
	if err != nil {
		return res, err
	}
	var rec *Record
	for i := range recs {
		if recs[i].ID == id {
			rec = &recs[i]
		}
	}
	if rec == nil {
		return res, fmt.Errorf("no install %q to undo", id)
	}
	bdir := filepath.Join(recDir(stateDir), rec.ID)
	base := rec.Home
	if t, ok := TargetFor(rec.Target); ok {
		base = filepath.Join(rec.Home, t.Base)
	}
	for _, rf := range rec.Files {
		op, err := rf.state()
		switch {
		case err != nil:
			res.Kept = append(res.Kept, rf.Path)
		case op == OpSame:
			res.Kept = append(res.Kept, rf.Path)
		case op == OpChange:
			old, err := os.ReadFile(filepath.Join(bdir, rf.Backup))
			if err != nil {
				return res, err
			}
			if err := writeFile(rf.Path, old); err != nil {
				return res, err
			}
			res.Restored++
		case op == OpAdd:
			if err := os.Remove(rf.Path); err != nil {
				return res, err
			}
			res.Removed++
			pruneEmpty(filepath.Dir(rf.Path), base)
		}
	}
	if err := os.RemoveAll(bdir); err != nil {
		return res, err
	}
	return res, os.Remove(filepath.Join(recDir(stateDir), rec.ID+".json"))
}

// pruneEmpty removes empty folders from dir up to (not with) stop.
func pruneEmpty(dir, stop string) {
	for dir != stop && strings.HasPrefix(dir, stop+string(filepath.Separator)) {
		if os.Remove(dir) != nil { // not empty
			return
		}
		dir = filepath.Dir(dir)
	}
}
