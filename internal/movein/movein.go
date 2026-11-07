// Package movein brings a user's work over from another agent tool that
// is closing or that they leave (CMP-86). Read walks the tool's export
// (a JSON file, JSON Lines, Markdown notes, or a folder of them) and
// finds four kinds of things by the shape of the data, not by a fixed
// format: scheduled tasks, tasks of a board, setup lines of a repo, and
// memory notes for the agent. Plan turns them into termocode's own files:
//
//   - a scheduled task → .termocode/workflows/<name>.jsonc with an agent
//     step (read-only; the schedule stays off until the user turns it on)
//   - setup lines → .termocode/worktrees.json "setup" (run only after
//     the trust card)
//   - memory notes → AGENTS.md (created, or the new notes appended)
//   - tasks → .termocode/imported/tasks.md, a checklist
//
// The plan never overwrites a file and shows every write first; Apply
// writes it after the user said yes. Everything in the export is data:
// it never becomes a template, a command that runs by itself, or a
// trusted workflow.
package movein

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/workflow"
)

// Task is one task of a board.
type Task struct {
	Title, Body, Status string
	Done                bool
	From                string // the export file
}

// Scheduled is one scheduled task. Cron or Preset is set when the
// schedule was understood; else Note says why it was left out.
type Scheduled struct {
	Title, Prompt          string
	Cron, Preset, Time, TZ string
	Note                   string
	From                   string
}

// Memory is one note for the agent.
type Memory struct {
	Text, From string
}

// Found is what Read found.
type Found struct {
	Tasks     []Task
	Scheduled []Scheduled
	Setup     []string
	Memory    []Memory
	// Skipped names the files (and lines) that held nothing known.
	Skipped []string
}

// Empty reports whether nothing was found.
func (f Found) Empty() bool {
	return len(f.Tasks) == 0 && len(f.Scheduled) == 0 && len(f.Setup) == 0 && len(f.Memory) == 0
}

// Limits of Read: an export is data from elsewhere.
const (
	maxFiles    = 5000
	maxFileSize = 32 << 20
	maxText     = 64 << 10 // one prompt, note or task body
)

// Read reads the export at path (a file or a folder).
func Read(path string) (Found, error) {
	var f Found
	st, err := os.Stat(path)
	if err != nil {
		return f, err
	}
	if !st.IsDir() {
		f.readFile(path, filepath.Base(path))
		return f, nil
	}
	n := 0
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			rel, _ := filepath.Rel(path, p)
			f.Skipped = append(f.Skipped, rel+": "+err.Error())
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(path, p)
		if !d.Type().IsRegular() { // links and devices are not followed
			f.Skipped = append(f.Skipped, rel+": not a plain file")
			return nil
		}
		if n++; n > maxFiles {
			f.Skipped = append(f.Skipped, fmt.Sprintf("more than %d files; the rest was not read", maxFiles))
			return filepath.SkipAll
		}
		f.readFile(p, filepath.ToSlash(rel))
		return nil
	})
	return f, err
}

// memoryDirs hold notes in Markdown.
var memoryDirs = map[string]bool{"memory": true, "memories": true, "knowledge": true, "notes": true}

// memoryFiles are notes by their name.
var memoryFiles = map[string]bool{"agents.md": true, "claude.md": true, "memory.md": true, "memories.md": true}

func (f *Found) readFile(p, rel string) {
	st, err := os.Stat(p)
	if err != nil {
		f.Skipped = append(f.Skipped, rel+": "+err.Error())
		return
	}
	if st.Size() > maxFileSize {
		f.Skipped = append(f.Skipped, rel+": too big")
		return
	}
	base := strings.ToLower(filepath.Base(p))
	ext := filepath.Ext(base)
	b, err := os.ReadFile(p)
	if err != nil {
		f.Skipped = append(f.Skipped, rel+": "+err.Error())
		return
	}
	before := f.count()
	switch ext {
	case ".json":
		var v any
		if err := json.Unmarshal(b, &v); err != nil {
			f.Skipped = append(f.Skipped, rel+": not valid JSON")
			return
		}
		f.walk(v, "", false, rel)
	case ".jsonl", ".ndjson":
		bad := 0
		sc := bufio.NewScanner(bytes.NewReader(b))
		sc.Buffer(make([]byte, 0, 64<<10), maxFileSize)
		for sc.Scan() {
			line := bytes.TrimSpace(sc.Bytes())
			if len(line) == 0 {
				continue
			}
			var v any
			if err := json.Unmarshal(line, &v); err != nil {
				bad++
				continue
			}
			f.walk(v, "", true, rel)
		}
		if bad > 0 {
			f.Skipped = append(f.Skipped, fmt.Sprintf("%s: %d line(s) are not JSON", rel, bad))
		}
	case ".md", ".markdown", ".txt":
		dir := strings.ToLower(filepath.Base(filepath.Dir(p)))
		if !memoryFiles[base] && !memoryDirs[dir] {
			f.Skipped = append(f.Skipped, rel+": not a known kind of file")
			return
		}
		if t := clip(strings.TrimSpace(string(b))); t != "" && utf8.ValidString(t) {
			f.Memory = append(f.Memory, Memory{Text: t, From: rel})
		}
	case ".sh":
		if !strings.HasPrefix(base, "setup") {
			f.Skipped = append(f.Skipped, rel+": not a known kind of file")
			return
		}
		f.addSetup(string(b))
	default:
		f.Skipped = append(f.Skipped, rel+": not a known kind of file")
		return
	}
	if f.count() == before {
		f.Skipped = append(f.Skipped, rel+": nothing known in it")
	}
}

func (f *Found) count() int {
	return len(f.Tasks) + len(f.Scheduled) + len(f.Setup) + len(f.Memory)
}

// Keys by meaning, in the order they are tried.
var (
	titleKeys    = []string{"title", "name", "summary", "subject"}
	promptKeys   = []string{"prompt", "instructions", "task_prompt", "taskPrompt"}
	scheduleKeys = []string{"cron", "cron_expression", "cronExpression", "schedule", "frequency", "recurrence"}
	bodyKeys     = []string{"description", "body", "details", "content", "prompt", "text"}
	statusKeys   = []string{"status", "state", "column", "stage"}
	timeKeys     = []string{"time", "at", "time_of_day", "timeOfDay"}
	tzKeys       = []string{"timezone", "tz", "time_zone", "timeZone"}
	setupKeys    = map[string]bool{"setup_script": true, "setupScript": true, "setup_commands": true, "setupCommands": true, "environment_setup": true, "environmentSetup": true, "setup": true}
	memoryKeys   = map[string]bool{"memory": true, "memories": true, "agents_md": true, "agentsMd": true, "knowledge": true}
	noteKeys     = []string{"content", "text", "body", "memory", "note"}
)

func str(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok {
			if s = strings.TrimSpace(s); s != "" {
				return s
			}
		}
	}
	return ""
}

func has(m map[string]any, keys []string) bool {
	for _, k := range keys {
		if _, ok := m[k]; ok {
			return true
		}
	}
	return false
}

func clip(s string) string {
	if len(s) <= maxText {
		return s
	}
	cut := maxText
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// oneLine makes s fit one line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func (f *Found) walk(v any, key string, inArray bool, from string) {
	switch x := v.(type) {
	case []any:
		for _, it := range x {
			f.walk(it, key, true, from)
		}
	case map[string]any:
		if s, ok := scheduledOf(x); ok {
			s.From = from
			f.Scheduled = append(f.Scheduled, s)
			return
		}
		if t, ok := taskOf(x, inArray); ok {
			t.From = from
			f.Tasks = append(f.Tasks, t)
			return
		}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			val := x[k]
			switch {
			case setupKeys[k]:
				f.setupFrom(val)
			case memoryKeys[k]:
				f.memoryFrom(val, from)
			default:
				f.walk(val, k, false, from)
			}
		}
	}
}

func (f *Found) setupFrom(v any) {
	switch x := v.(type) {
	case string:
		f.addSetup(x)
	case []any:
		for _, it := range x {
			if s, ok := it.(string); ok {
				f.addSetup(s)
			}
		}
	}
}

// addSetup adds the command lines of a script: no empty lines, no
// comments, no shebang, none twice.
func (f *Found) addSetup(script string) {
	for _, l := range strings.Split(script, "\n") {
		l = strings.TrimSpace(strings.TrimSuffix(l, "\r"))
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		dup := false
		for _, s := range f.Setup {
			if s == l {
				dup = true
				break
			}
		}
		if !dup {
			f.Setup = append(f.Setup, l)
		}
	}
}

func (f *Found) memoryFrom(v any, from string) {
	add := func(s string) {
		if s = clip(strings.TrimSpace(s)); s != "" {
			f.Memory = append(f.Memory, Memory{Text: s, From: from})
		}
	}
	switch x := v.(type) {
	case string:
		add(x)
	case []any:
		for _, it := range x {
			switch y := it.(type) {
			case string:
				add(y)
			case map[string]any:
				add(str(y, noteKeys...))
			}
		}
	case map[string]any:
		add(str(x, noteKeys...))
	}
}

// presetWords are the schedule words we understand.
var presetWords = map[string]string{
	"hourly": "hourly", "every hour": "hourly",
	"daily": "daily", "every day": "daily", "nightly": "daily",
	"weekdays": "weekdays", "every weekday": "weekdays", "weekday": "weekdays",
	"weekly": "weekly", "every week": "weekly",
	"monthly": "monthly", "every month": "monthly",
}

// isCron reports whether s looks like a 5-field cron line.
func isCron(s string) bool {
	fields := strings.Fields(s)
	if len(fields) != 5 {
		return false
	}
	for _, x := range fields {
		if strings.Trim(x, "0123456789*/,-") != "" {
			return false
		}
	}
	return true
}

// clock is "HH:MM" from "9:30" or "09:30"; "" when it is no time.
func clock(s string) string {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok || len(m) < 2 {
		return ""
	}
	m = m[:2]
	if len(h) == 1 {
		h = "0" + h
	}
	if len(h) != 2 || h < "00" || h > "23" || m < "00" || m > "59" || strings.Trim(h+m, "0123456789") != "" {
		return ""
	}
	return h + ":" + m
}

// scheduledOf reads a scheduled task: a prompt and a schedule.
func scheduledOf(m map[string]any) (Scheduled, bool) {
	prompt := str(m, promptKeys...)
	if prompt == "" || !has(m, scheduleKeys) {
		return Scheduled{}, false
	}
	s := Scheduled{Prompt: clip(prompt), Title: oneLine(str(m, titleKeys...))}
	if s.Title == "" {
		s.Title = oneLine(prompt)
		if r := []rune(s.Title); len(r) > 40 {
			s.Title = string(r[:40])
		}
	}
	s.TZ = str(m, tzKeys...)
	at := str(m, timeKeys...)
	raw := ""
	for _, k := range scheduleKeys {
		switch v := m[k].(type) {
		case string:
			if raw == "" {
				raw = strings.TrimSpace(v)
			}
		case map[string]any: // {"cron": …} or {"frequency": …, "time": …}
			if raw == "" {
				raw = str(v, append([]string{"expression", "preset"}, scheduleKeys...)...)
			}
			if at == "" {
				at = str(v, timeKeys...)
			}
			if s.TZ == "" {
				s.TZ = str(v, tzKeys...)
			}
		}
	}
	switch p, ok := presetWords[strings.ToLower(raw)]; {
	case isCron(raw):
		s.Cron = strings.Join(strings.Fields(raw), " ")
	case ok:
		s.Preset = p
		if p != "hourly" {
			s.Time = clock(at)
		}
	case raw == "":
		s.Note = "no schedule found; the workflow runs by hand"
	default:
		s.Note = fmt.Sprintf("schedule %q not understood; the workflow runs by hand", oneLine(raw))
	}
	if strings.ContainsAny(s.TZ, " \t\\\n") {
		s.TZ = ""
	}
	return s, true
}

// doneWords are statuses of finished tasks (lower case, no spaces,
// _ or -).
var doneWords = map[string]bool{"done": true, "completed": true, "complete": true, "closed": true,
	"merged": true, "resolved": true, "cancelled": true, "canceled": true, "archived": true, "finished": true}

// taskOf reads a task of a list: a title and a body or a status.
func taskOf(m map[string]any, inArray bool) (Task, bool) {
	if !inArray {
		return Task{}, false
	}
	t := Task{Title: oneLine(str(m, titleKeys...)), Body: clip(str(m, bodyKeys...)), Status: oneLine(str(m, statusKeys...))}
	if t.Title == "" || (t.Body == "" && t.Status == "") {
		return Task{}, false
	}
	norm := strings.NewReplacer(" ", "", "_", "", "-", "").Replace(strings.ToLower(t.Status))
	t.Done = doneWords[norm]
	return t, true
}

// ── Plan and Apply ───────────────────────────────────────────────────────

// Mode is what a write does.
type Mode string

const (
	ModeCreate Mode = "create" // a new file
	ModeAppend Mode = "append" // added at the end of a file that exists
	ModeSkip   Mode = "skip"   // not written; Why says why
)

// Write is one row of the plan.
type Write struct {
	Path    string // in the workspace, with /
	Mode    Mode
	Content []byte // the file (create) or the added text (append)
	Why     string // a skip's reason, or a note
	Preview []string
}

// Options change the plan.
type Options struct {
	// Agent runs the workflows of scheduled tasks: codex (default) or
	// claude-code.
	Agent string
}

// Slug makes a workflow name: a-z, 0-9 and -, a letter first, at most
// 40 characters.
func Slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
			continue
		}
		dash = true
	}
	out := b.String()
	if out == "" {
		return "imported"
	}
	if out[0] < 'a' || out[0] > 'z' {
		out = "task-" + out
	}
	return cut(out, 40)
}

// cut shortens a slug to n bytes, at a dash when it can.
func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	if i := strings.LastIndexByte(s, '-'); i > 0 && s[n-1] != '-' {
		// cut inside a word: drop the broken word
		if i >= n/2 {
			s = s[:i]
		}
	}
	return strings.TrimRight(s, "-")
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// q is s as a JSON string.
func q(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// noTemplate keeps text from the export from being read as a {{ … }}
// template of the workflow.
func noTemplate(s string) string {
	for strings.Contains(s, "{{") {
		s = strings.ReplaceAll(s, "{{", "{ {")
	}
	return s
}

func workflowFile(name string, s Scheduled, agent string) []byte {
	var b strings.Builder
	b.WriteString("// Brought over by `termocode import`. The agent only reads (\"read-only\");\n")
	b.WriteString("// to let it change files, put the step in an \"each\" step with\n")
	b.WriteString("// \"worktree\": true. A schedule stays off until you turn it on (o).\n")
	b.WriteString("{\n")
	fmt.Fprintf(&b, "  \"version\": 1,\n  \"name\": %s,\n  \"title\": %s,\n", q(name), q(noTemplate(s.Title)))
	desc := "Imported scheduled task"
	if s.From != "" {
		desc += " (from " + oneLine(s.From) + ")"
	}
	fmt.Fprintf(&b, "  \"description\": %s,\n", q(noTemplate(desc)))
	var sched []string
	switch {
	case s.Cron != "":
		sched = append(sched, `"cron": `+q(s.Cron))
	case s.Preset != "":
		sched = append(sched, `"preset": `+q(s.Preset))
		if s.Time != "" {
			sched = append(sched, `"time": `+q(s.Time))
		}
	}
	if len(sched) > 0 && s.TZ != "" {
		sched = append(sched, `"tz": `+q(s.TZ))
	}
	if len(sched) > 0 {
		fmt.Fprintf(&b, "  \"schedule\": { %s },\n", strings.Join(sched, ", "))
	}
	b.WriteString("  \"steps\": [\n")
	fmt.Fprintf(&b, "    { \"id\": \"task\", \"type\": \"agent\", \"agent\": %s, \"sandbox\": \"read-only\",\n", q(agent))
	fmt.Fprintf(&b, "      \"prompt\": %s }\n", q(noTemplate(s.Prompt)))
	b.WriteString("  ]\n}\n")
	return []byte(b.String())
}

func scheduleText(s Scheduled) string {
	switch {
	case s.Cron != "":
		return "cron " + s.Cron + tzText(s.TZ)
	case s.Preset != "":
		t := s.Preset
		if s.Time != "" {
			t += " at " + s.Time
		}
		return t + tzText(s.TZ)
	}
	return "by hand"
}

func tzText(tz string) string {
	if tz == "" {
		return ""
	}
	return " (" + tz + ")"
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	l = oneLine(l)
	if r := []rune(l); len(r) > 72 {
		l = string(r[:71]) + "…"
	}
	return l
}

// freeName is dir/base+ext, or base-2, base-3, … when that is taken.
func freeName(ws, dir, base, ext string, taken map[string]bool) string {
	for i := 1; ; i++ {
		name := base
		if i > 1 {
			suffix := fmt.Sprintf("-%d", i)
			name = cut(base, 40-len(suffix)) + suffix
		}
		rel := dir + "/" + name + ext
		if !taken[rel] && !exists(filepath.Join(ws, filepath.FromSlash(rel))) {
			taken[rel] = true
			return name
		}
	}
}

// Plan turns f into writes in the workspace ws. It reads ws but
// writes nothing.
func Plan(f Found, ws string, o Options) ([]Write, error) {
	agent := o.Agent
	if agent == "" {
		agent = "codex"
	}
	known := false
	for _, a := range workflow.Agents {
		known = known || a == agent
	}
	if !known {
		return nil, fmt.Errorf("agent %q is not one of %s", agent, strings.Join(workflow.Agents, ", "))
	}
	var plan []Write
	taken := map[string]bool{}

	for _, s := range f.Scheduled {
		name := freeName(ws, ".termocode/workflows", Slug(s.Title), workflow.Ext, taken)
		rel := ".termocode/workflows/" + name + workflow.Ext
		content := workflowFile(name, s, agent)
		w := Write{Path: rel, Mode: ModeCreate, Content: content, Why: s.Note,
			Preview: []string{"workflow " + name + ": " + s.Title, "runs: " + scheduleText(s),
				"agent: " + agent + ", read-only", "prompt: " + firstLine(s.Prompt)}}
		if s.Note != "" {
			w.Preview = append(w.Preview, "note: "+s.Note)
		}
		if _, _, err := workflow.Parse(filepath.Join(ws, filepath.FromSlash(rel)), content); err != nil {
			w.Mode, w.Why = ModeSkip, "the workflow would not be valid: "+oneLine(err.Error())
		}
		plan = append(plan, w)
	}

	if len(f.Setup) > 0 {
		rel := ".termocode/worktrees.json"
		w := Write{Path: rel, Mode: ModeCreate}
		for _, l := range f.Setup {
			w.Preview = append(w.Preview, "setup: "+l)
		}
		if exists(filepath.Join(ws, filepath.FromSlash(rel))) {
			w.Mode, w.Why = ModeSkip, "the file is there already; add these setup lines by hand"
		} else {
			b, err := json.MarshalIndent(map[string]any{"setup": f.Setup}, "", "  ")
			if err != nil {
				return nil, err
			}
			w.Content = append(b, '\n')
			w.Why = "setup lines run only after you trust them on a card"
		}
		plan = append(plan, w)
	}

	if len(f.Memory) > 0 {
		if w, ok := memoryWrite(f.Memory, ws); ok {
			plan = append(plan, w)
		}
	}

	if len(f.Tasks) > 0 {
		name := freeName(ws, ".termocode/imported", "tasks", ".md", taken)
		var b strings.Builder
		b.WriteString("# Imported tasks\n\n")
		for _, t := range f.Tasks {
			mark := " "
			if t.Done {
				mark = "x"
			}
			line := "- [" + mark + "] " + noTemplate(t.Title)
			if t.Status != "" {
				line += " (" + t.Status + ")"
			}
			b.WriteString(line + "\n")
			if t.Body != "" {
				for _, l := range strings.Split(strings.TrimSpace(t.Body), "\n") {
					b.WriteString(strings.TrimRight("  "+l, " ") + "\n")
				}
			}
		}
		content := b.String()
		w := Write{Path: ".termocode/imported/" + name + ".md", Mode: ModeCreate, Content: []byte(content)}
		w.Preview = previewLines(content, 8)
		plan = append(plan, w)
	}
	return plan, nil
}

// memoryWrite creates AGENTS.md or appends the notes it does not hold.
func memoryWrite(notes []Memory, ws string) (Write, bool) {
	rel := "AGENTS.md"
	p := filepath.Join(ws, rel)
	old, err := os.ReadFile(p)
	isNew := errors.Is(err, fs.ErrNotExist)
	if err != nil && !isNew {
		return Write{Path: rel, Mode: ModeSkip, Why: "cannot read it: " + err.Error(),
			Preview: []string{fmt.Sprintf("%d note(s)", len(notes))}}, true
	}
	var body strings.Builder
	n := 0
	seen := string(old)
	for _, m := range notes {
		t := strings.TrimSpace(m.Text)
		if t == "" || strings.Contains(seen, t) {
			continue
		}
		seen += "\n" + t
		n++
		if !strings.Contains(t, "\n") {
			body.WriteString("- " + t + "\n")
		} else {
			body.WriteString("\n" + t + "\n\n")
		}
	}
	if n == 0 {
		return Write{}, false
	}
	var content string
	mode := ModeAppend
	if isNew {
		mode = ModeCreate
		content = "# Agent notes\n\nNotes brought over from another tool.\n\n" + body.String()
	} else {
		lead := "\n"
		if len(old) > 0 && old[len(old)-1] != '\n' {
			lead = "\n\n"
		}
		content = lead + "## Imported notes\n\n" + body.String()
	}
	w := Write{Path: rel, Mode: mode, Content: []byte(content)}
	w.Preview = previewLines(body.String(), 8)
	return w, true
}

func previewLines(s string, max int) []string {
	var out []string
	rest := 0
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if len(out) == max {
			rest++
			continue
		}
		out = append(out, firstLine(l))
	}
	if rest > 0 {
		out = append(out, fmt.Sprintf("… %d more line(s)", rest))
	}
	return out
}

// safePath is the file of rel in ws; it refuses a path out of ws and a
// path through a link.
func safePath(ws, rel string) (string, error) {
	lp := filepath.FromSlash(rel)
	if !filepath.IsLocal(lp) {
		return "", fmt.Errorf("%s: not inside the workspace", rel)
	}
	dir := ws
	parts := strings.Split(filepath.Dir(lp), string(filepath.Separator))
	for _, part := range parts {
		if part == "." || part == "" {
			continue
		}
		dir = filepath.Join(dir, part)
		if st, err := os.Lstat(dir); err == nil && st.Mode()&fs.ModeSymlink != 0 {
			return "", fmt.Errorf("%s: goes through a link", rel)
		}
	}
	return filepath.Join(ws, lp), nil
}

// Apply writes the create and append rows of plan in ws, in order, and
// stops at the first error. It returns the paths it wrote.
func Apply(ws string, plan []Write) ([]string, error) {
	var done []string
	for _, w := range plan {
		if w.Mode == ModeSkip {
			continue
		}
		p, err := safePath(ws, w.Path)
		if err != nil {
			return done, err
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return done, err
		}
		var fh *os.File
		switch w.Mode {
		case ModeCreate:
			fh, err = os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		case ModeAppend:
			st, lerr := os.Lstat(p)
			if lerr != nil {
				return done, lerr
			}
			if !st.Mode().IsRegular() {
				return done, fmt.Errorf("%s: not a plain file", w.Path)
			}
			fh, err = os.OpenFile(p, os.O_WRONLY|os.O_APPEND, 0)
		default:
			return done, fmt.Errorf("%s: unknown mode %q", w.Path, w.Mode)
		}
		if err != nil {
			return done, err
		}
		_, err = fh.Write(w.Content)
		if cerr := fh.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return done, err
		}
		done = append(done, w.Path)
	}
	return done, nil
}
