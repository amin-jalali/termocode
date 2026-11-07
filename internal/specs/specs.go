// Package specs is the spec pipeline (CMP-75). A spec is a folder
// .termocode/specs/<name>/ in the project: requirements.md (bugfix.md
// for a bug: current, expected and unchanged behavior), design.md,
// tasks.md and, at the end, review.md. The phases go in order:
// Requirements, Design, Tasks, Build (the tasks are done and ticked) and
// Review. The AI or the user writes each file; the user approves each
// phase before the next one starts. An approval is bound to the file's
// bytes, so a file changed after its approval needs approval again
// (ticking a task does not count as a change of the task list).
// spec.json keeps the kind, the goal and the approvals.
//
// The package is pure file work on a local folder: no AI, no UI.
package specs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Dir is where specs live, relative to the project root.
const Dir = ".termocode/specs"

// StateFile is the spec's own file in its folder.
const StateFile = "spec.json"

// MaxName is the longest spec folder name drafted from a goal.
const MaxName = 40

// Kind is a feature spec or a bugfix spec.
type Kind string

const (
	Feature Kind = "feature"
	Bugfix  Kind = "bugfix"
)

// Phase is one step of the pipeline.
type Phase int

const (
	Requirements Phase = iota
	Design
	Tasks
	Build
	Review
)

// Phases are all phases in order.
var Phases = []Phase{Requirements, Design, Tasks, Build, Review}

var phaseNames = [...]string{"requirements", "design", "tasks", "build", "review"}
var phaseTitles = [...]string{"Requirements", "Design", "Tasks", "Build", "Review"}

// Name is the phase's key in spec.json.
func (p Phase) Name() string { return phaseNames[p] }

// Title is the phase as the UI shows it.
func (p Phase) Title() string { return phaseTitles[p] }

// Status is where a phase stands.
type Status int

const (
	Missing  Status = iota // no file yet
	Template               // the file is still the empty template
	Draft                  // written, not approved
	Approved               // approved and unchanged since
	Stale                  // approved, then changed: needs approval again
)

// State is spec.json.
type State struct {
	Kind    Kind      `json:"kind"`
	Goal    string    `json:"goal"`
	Created time.Time `json:"created"`
	// Approved maps a phase name to the sha256 of its file when the user
	// approved it.
	Approved map[string]string `json:"approved,omitempty"`
	// ApprovedAt maps a phase name to when.
	ApprovedAt map[string]time.Time `json:"approved_at,omitempty"`
}

// Spec is one spec folder.
type Spec struct {
	Name string
	Path string // the folder
	State
}

// File is the file name of a phase (Build ticks tasks.md).
func (s Spec) File(p Phase) string {
	switch p {
	case Requirements:
		if s.Kind == Bugfix {
			return "bugfix.md"
		}
		return "requirements.md"
	case Design:
		return "design.md"
	case Tasks, Build:
		return "tasks.md"
	}
	return "review.md"
}

// FilePath is the full path of a phase's file.
func (s Spec) FilePath(p Phase) string { return filepath.Join(s.Path, s.File(p)) }

// Rel is a phase's file relative to the project root (slash separated).
func (s Spec) Rel(p Phase) string { return Dir + "/" + s.Name + "/" + s.File(p) }

// Slug makes a folder name from a goal: lower case words joined by -,
// cut on a word to MaxName bytes. "" when no word is left.
func Slug(goal string) string {
	words := strings.FieldsFunc(strings.ToLower(goal), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	var b strings.Builder
	for _, w := range words {
		add := len(w)
		if b.Len() > 0 {
			add++
		}
		if b.Len()+add > MaxName {
			if b.Len() == 0 { // one long word: cut it on a rune
				for _, r := range w {
					if b.Len()+len(string(r)) > MaxName {
						break
					}
					b.WriteRune(r)
				}
			}
			break
		}
		if b.Len() > 0 {
			b.WriteByte('-')
		}
		b.WriteString(w)
	}
	return b.String()
}

// validName: a folder name made by Slug (no path, no dot first).
func validName(name string) bool {
	if name == "" || strings.HasPrefix(name, ".") || strings.ContainsAny(name, `/\`) || name != filepath.Base(name) {
		return false
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' {
			return false
		}
	}
	return true
}

func specDir(root string) string { return filepath.Join(root, filepath.FromSlash(Dir)) }

// Create makes a new spec for goal under root: its folder (named from
// the goal; -2, -3 … when taken), spec.json and the first file as a
// template.
func Create(root, goal string, kind Kind, now time.Time) (Spec, error) {
	goal = strings.TrimSpace(strings.Join(strings.Fields(goal), " "))
	base := Slug(goal)
	if base == "" {
		return Spec{}, errors.New("type a goal with at least one word")
	}
	if kind != Bugfix {
		kind = Feature
	}
	if err := os.MkdirAll(specDir(root), 0o755); err != nil {
		return Spec{}, err
	}
	name := base
	for n := 2; ; n++ {
		err := os.Mkdir(filepath.Join(specDir(root), name), 0o755)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return Spec{}, err
		}
		if n > 99 {
			return Spec{}, errors.New("too many specs with this name")
		}
		name = base + "-" + strconv.Itoa(n)
	}
	s := Spec{Name: name, Path: filepath.Join(specDir(root), name), State: State{Kind: kind, Goal: goal, Created: now}}
	if err := s.save(); err != nil {
		return Spec{}, err
	}
	if err := os.WriteFile(s.FilePath(Requirements), []byte(s.template()), 0o644); err != nil {
		return Spec{}, err
	}
	return s, nil
}

// template is the first file of a new spec.
func (s Spec) template() string {
	if s.Kind == Bugfix {
		return "# Bugfix: " + s.Goal + "\n\n## Current behavior\n\n## Expected behavior\n\n## Unchanged behavior\n\n## Steps to reproduce\n"
	}
	return "# Requirements: " + s.Goal + "\n\n## User stories\n\n## Acceptance criteria\n\n- WHEN … THEN …\n"
}

func (s Spec) save() error {
	b, err := json.MarshalIndent(s.State, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.Path, StateFile+".tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.Path, StateFile))
}

// Load reads the spec name under root.
func Load(root, name string) (Spec, error) {
	if !validName(name) {
		return Spec{}, fmt.Errorf("%q is not a spec name", name)
	}
	s := Spec{Name: name, Path: filepath.Join(specDir(root), name)}
	b, err := os.ReadFile(filepath.Join(s.Path, StateFile))
	if err != nil {
		return Spec{}, err
	}
	if err := json.Unmarshal(b, &s.State); err != nil {
		return Spec{}, fmt.Errorf("%s/%s/%s: %v", Dir, name, StateFile, err)
	}
	if s.Kind != Bugfix {
		s.Kind = Feature
	}
	return s, nil
}

// List reads every spec under root, newest first (by name when made at
// the same time). A folder without a readable spec.json is skipped.
func List(root string) ([]Spec, error) {
	ents, err := os.ReadDir(specDir(root))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Spec
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		if s, err := Load(root, e.Name()); err == nil {
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Created.Equal(out[j].Created) {
			return out[i].Created.After(out[j].Created)
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

var tickRe = regexp.MustCompile(`(?m)^([-*] \[)[xX](\])`)

// hashOf is the approval hash of a phase's file: for Tasks the ticks
// are ignored (Build ticks them).
func hashOf(p Phase, b []byte) string {
	if p == Tasks {
		b = tickRe.ReplaceAll(b, []byte("$1 $2"))
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func (s Spec) read(p Phase) ([]byte, error) { return os.ReadFile(s.FilePath(p)) }

// Status is where phase p stands now.
func (s Spec) Status(p Phase) Status {
	b, err := s.read(p)
	if err != nil {
		return Missing
	}
	if want, ok := s.Approved[p.Name()]; ok {
		if hashOf(p, b) == want {
			return Approved
		}
		return Stale
	}
	if strings.TrimSpace(string(b)) == "" || (p == Requirements && string(b) == s.template()) {
		return Template
	}
	return Draft
}

// Current is the first phase that is not approved; done when all are.
func (s Spec) Current() (Phase, bool) {
	for _, p := range Phases {
		if s.Status(p) != Approved {
			return p, false
		}
	}
	return Review, true
}

// Approve records the user's approval of phase p, bound to its file as
// it is now. Every phase before it must be approved (and unchanged);
// the file must be written; Build needs every task ticked.
func (s *Spec) Approve(p Phase, now time.Time) error {
	for _, q := range Phases[:p] {
		if s.Status(q) != Approved {
			return fmt.Errorf("approve %s first", q.Title())
		}
	}
	switch s.Status(p) {
	case Missing:
		return fmt.Errorf("%s is not written yet", s.File(p))
	case Template:
		return fmt.Errorf("%s is still the empty template", s.File(p))
	}
	b, err := s.read(p)
	if err != nil {
		return err
	}
	if p == Build {
		ts := ParseTasks(b)
		done, all := Progress(ts)
		switch {
		case all == 0:
			return errors.New("tasks.md has no tasks")
		case done < all:
			return fmt.Errorf("%d tasks are still open in tasks.md", all-done)
		}
	}
	if s.Approved == nil {
		s.Approved = map[string]string{}
	}
	if s.ApprovedAt == nil {
		s.ApprovedAt = map[string]time.Time{}
	}
	s.Approved[p.Name()] = hashOf(p, b)
	s.ApprovedAt[p.Name()] = now
	return s.save()
}

// Task is one tick box of tasks.md.
type Task struct {
	N     int    // its number (written, or the next free one)
	Title string // without the number and the (after …)
	Done  bool
	After []int // the tasks it waits for
	Line  int   // 1-based line in the file
}

var (
	taskRe  = regexp.MustCompile(`^[-*] \[([ xX])\] +(.*)$`)
	numRe   = regexp.MustCompile(`^(\d+)[.)] +`)
	afterRe = regexp.MustCompile(`(?i)\s*\(after ([\d,\s]+)\)\s*$`)
)

// ParseTasks reads the top-level tick boxes of tasks.md (indented ones
// are sub steps of the task above).
func ParseTasks(b []byte) []Task {
	var out []Task
	next := 1
	for i, l := range strings.Split(string(b), "\n") {
		m := taskRe.FindStringSubmatch(strings.TrimRight(l, " \r\t"))
		if m == nil {
			continue
		}
		t := Task{Done: m[1] != " ", Title: m[2], Line: i + 1}
		if a := afterRe.FindStringSubmatch(t.Title); a != nil {
			for _, f := range strings.FieldsFunc(a[1], func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
				if n, err := strconv.Atoi(f); err == nil {
					t.After = append(t.After, n)
				}
			}
			t.Title = strings.TrimSpace(t.Title[:len(t.Title)-len(a[0])])
		}
		if n := numRe.FindStringSubmatch(t.Title); n != nil {
			t.N, _ = strconv.Atoi(n[1])
			t.Title = strings.TrimSpace(t.Title[len(n[0]):])
		} else {
			t.N = next
		}
		if t.N >= next {
			next = t.N + 1
		}
		out = append(out, t)
	}
	return out
}

// Progress is how many tasks are ticked, of all.
func Progress(ts []Task) (done, all int) {
	for _, t := range ts {
		if t.Done {
			done++
		}
	}
	return done, len(ts)
}

// Waves groups the open tasks: a task comes in the first wave after all
// the open tasks it waits for. Tasks that wait on each other (a cycle)
// come together in the last wave. Numbers in each wave are sorted.
func Waves(ts []Task) [][]int {
	open := map[int]Task{}
	for _, t := range ts {
		if !t.Done {
			open[t.N] = t
		}
	}
	var waves [][]int
	for len(open) > 0 {
		var w []int
		for n, t := range open {
			ready := true
			for _, a := range t.After {
				if _, waits := open[a]; waits && a != n {
					ready = false
					break
				}
			}
			if ready {
				w = append(w, n)
			}
		}
		if len(w) == 0 { // a cycle: the rest together
			for n := range open {
				w = append(w, n)
			}
		}
		sort.Ints(w)
		for _, n := range w {
			delete(open, n)
		}
		waves = append(waves, w)
	}
	return waves
}

// joinInts is "2, 3".
func joinInts(ns []int) string {
	s := make([]string, len(ns))
	for i, n := range ns {
		s[i] = strconv.Itoa(n)
	}
	return strings.Join(s, ", ")
}

// NextWave is the numbers of the tasks that can start now ("" when none
// is open).
func (s Spec) NextWave() string {
	b, err := s.read(Tasks)
	if err != nil {
		return ""
	}
	if w := Waves(ParseTasks(b)); len(w) > 0 {
		return joinInts(w[0])
	}
	return ""
}

// Prompt is what the AI is asked for phase p. root is the project root
// (the paths in it are relative to it).
func (s Spec) Prompt(p Phase, root string) string {
	req := s.Rel(Requirements)
	var b strings.Builder
	fmt.Fprintf(&b, "Spec %q (%s), goal: %s\n\n", s.Name, s.Kind, s.Goal)
	switch p {
	case Requirements:
		if s.Kind == Bugfix {
			fmt.Fprintf(&b, "Write the bugfix spec in %s. Find the cause in the code, but do not fix it yet. Fill in:\n"+
				"- Current behavior: what happens now, with the steps to see it.\n"+
				"- Expected behavior: what should happen instead.\n"+
				"- Unchanged behavior: what must keep working exactly as it does now; the fix must not change it.\n", req)
		} else {
			fmt.Fprintf(&b, "Write the requirements in %s: short user stories, then acceptance criteria, each as WHEN <condition> THEN <expected result>. "+
				"Ask me about anything unclear before you write. Do not write the design or code yet.\n", req)
		}
	case Design:
		fmt.Fprintf(&b, "Read %s (approved). Write the design in %s: the parts of the code to change, data and interfaces, error handling, and how it is tested. "+
			"Keep it short. Do not write code yet.\n", req, s.Rel(Design))
	case Tasks:
		fmt.Fprintf(&b, "Read %s and %s (both approved). Write %s: small tasks in order, one tick box each, like\n"+
			"- [ ] 1. <task>\n- [ ] 2. <task> (after 1)\n"+
			"Each task names its test. A task that needs another one says (after N). Do not write code yet.\n", req, s.Rel(Design), s.Rel(Tasks))
	case Build:
		next := s.NextWave()
		fmt.Fprintf(&b, "Build the spec. Read %s, %s and %s (all approved). Work through the open tasks in order (next: %s); "+
			"a task waits for the tasks in its (after N). For each task: write its test first, then the code, run the test, then tick the task (- [x]) in tasks.md. "+
			"Do not change the requirements, the design or the task list. Stop when every task is ticked.\n", req, s.Rel(Design), s.Rel(Tasks), next)
	case Review:
		what := "each acceptance criterion"
		if s.Kind == Bugfix {
			what = "the expected behavior and each unchanged behavior"
		}
		fmt.Fprintf(&b, "Review the work against %s and %s. Write %s: for %s, say whether the code and its tests meet it and where; then list anything missed or risky. "+
			"Do not change code.\n", req, s.Rel(Design), s.Rel(Review), what)
	}
	return b.String()
}
