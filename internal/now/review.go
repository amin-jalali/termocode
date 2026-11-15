package now

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
)

// The review of one pull request, in one fixed order: the question it
// answers (and its task), the solution in one line, the changes grouped
// with the main change first, one proof row per check bound to the
// commit it ran on, the risk with its reasons and one short suggestion.
// An approval is bound to the commit the user saw (CanApprove).

// MaxReviewLine caps the question and the solution.
const MaxReviewLine = 200

// Proof states.
const (
	ProofOK      = "ok"
	ProofFail    = "fail"
	ProofRunning = "running"
	ProofStale   = "stale"
	ProofNotRun  = "notrun"
)

// Risk levels.
const (
	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"
)

var (
	headRe = regexp.MustCompile(`(?im)^\s{0,3}(?:#{1,6}\s*|\*\*)?(why|problem|question|context|what|solution|how|changes?)\s*(?:\*\*)?\s*:?\s*(.*)$`)
	taskRe = regexp.MustCompile(`\b[A-Z][A-Z0-9]{1,9}-\d+\b|#\d+\b`)
)

// ReviewText reads the question, its task and the solution from a pull
// request's title and body.
func ReviewText(title, body string) (question, task, solution string) {
	parts := map[string]string{}
	var cur string
	var first []string
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		if m := headRe.FindStringSubmatch(line); m != nil {
			switch strings.ToLower(m[1]) {
			case "why", "problem", "question", "context":
				cur = "q"
			case "what", "solution", "how", "change", "changes":
				cur = "s"
			}
			if rest := strings.TrimSpace(m[2]); rest != "" && parts[cur] == "" {
				parts[cur] = rest
			}
			continue
		}
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "#") {
			cur = "x" // another heading ends the part
			continue
		}
		if cur == "" {
			if t == "" && len(first) > 0 {
				cur = "x"
			} else if t != "" {
				first = append(first, t)
			}
			continue
		}
		if (cur == "q" || cur == "s") && parts[cur] == "" && t != "" {
			parts[cur] = t
		}
	}
	question, solution = parts["q"], parts["s"]
	if solution == "" && len(first) > 0 && question == "" {
		solution = strings.Join(first, " ")
	}
	if solution == "" {
		solution = title
	}
	question, solution = cutLine(question), cutLine(solution)
	task = taskRe.FindString(question)
	return question, task, solution
}

func cutLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= MaxReviewLine {
		return s
	}
	cut := MaxReviewLine - len("…")
	for cut > 0 && s[cut]&0xC0 == 0x80 {
		cut--
	}
	return s[:cut] + "…"
}

// ReviewFile is one changed file.
type ReviewFile struct {
	Path string
	Add  int
	Del  int
}

// ReviewGroup is a group of changes; Main is the main change.
type ReviewGroup struct {
	Title string
	Files []string
	Add   int
	Del   int
	Main  bool
}

// fileKind names a file by what it is: code, test, config or docs.
func fileKind(p string) string {
	base := strings.ToLower(path.Base(p))
	ext := path.Ext(base)
	switch {
	case strings.Contains(base, "_test.") || strings.Contains(base, ".test.") || strings.Contains(base, ".spec.") ||
		strings.HasPrefix(p, "test/") || strings.Contains(p, "/testdata/") || strings.HasPrefix(p, "e2e/"):
		return "test"
	case ext == ".md" || ext == ".txt" || ext == ".rst" || strings.HasPrefix(p, "docs/"):
		return "docs"
	case ext == ".yaml" || ext == ".yml" || ext == ".json" || ext == ".toml" || ext == ".ini" || ext == ".env" || ext == ".lock" ||
		base == "go.mod" || base == "go.sum" || base == "dockerfile" || base == "makefile":
		return "config"
	}
	return "code"
}

var kindOrder = []string{"code", "test", "config", "docs"}

// ReviewGroups puts the biggest code change first (the main change) and
// the rest in one group that names its kinds.
func ReviewGroups(files []ReviewFile) []ReviewGroup {
	if len(files) == 0 {
		return nil
	}
	main := -1
	for i, f := range files {
		if fileKind(f.Path) != "code" {
			continue
		}
		if main < 0 || f.Add+f.Del > files[main].Add+files[main].Del {
			main = i
		}
	}
	if main < 0 {
		main = 0
		for i, f := range files {
			if f.Add+f.Del > files[main].Add+files[main].Del {
				main = i
			}
		}
	}
	m := files[main]
	out := []ReviewGroup{{Title: path.Base(m.Path), Files: []string{m.Path}, Add: m.Add, Del: m.Del, Main: true}}
	rest := ReviewGroup{}
	kinds := map[string]bool{}
	for i, f := range files {
		if i == main {
			continue
		}
		rest.Files = append(rest.Files, f.Path)
		rest.Add += f.Add
		rest.Del += f.Del
		kinds[fileKind(f.Path)] = true
	}
	if len(rest.Files) > 0 {
		var names []string
		for _, k := range kindOrder {
			if kinds[k] {
				names = append(names, k)
			}
		}
		rest.Title = "Rest: " + strings.Join(names, ", ")
		out = append(out, rest)
	}
	return out
}

// ProofIn is one check as the source gives it: its state and the commit
// it ran on.
type ProofIn struct {
	Name  string
	State string // ok, fail, running, notrun
	SHA   string
	URL   string
}

// ProofRow is one proof row: State is ok, fail, running, stale (it ran
// on an older commit) or notrun; SHA is short; Fix offers the fix (a red
// row), Again a new run (a stale one).
type ProofRow struct {
	Name  string
	State string
	SHA   string
	URL   string
	Fix   bool
	Again bool
}

var proofOrder = map[string]int{ProofFail: 0, ProofStale: 1, ProofRunning: 2, ProofOK: 3, ProofNotRun: 4}

// ShortSHA is the 7-digit commit.
func ShortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// ReviewProof makes the proof rows for head: one per check name (the
// first given wins), a check of another commit is stale; red first,
// then stale, running, green, not run.
func ReviewProof(head string, in []ProofIn) []ProofRow {
	seen := map[string]bool{}
	var out []ProofRow
	for _, p := range in {
		if p.Name == "" || seen[p.Name] {
			continue
		}
		seen[p.Name] = true
		r := ProofRow{Name: p.Name, State: p.State, SHA: ShortSHA(p.SHA), URL: p.URL}
		if p.SHA != "" && head != "" && p.SHA != head && !strings.HasPrefix(head, p.SHA) && !strings.HasPrefix(p.SHA, head) {
			r.State, r.Again = ProofStale, true
		}
		if _, ok := proofOrder[r.State]; !ok {
			r.State = ProofNotRun
		}
		r.Fix = r.State == ProofFail
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return proofOrder[out[i].State] < proofOrder[out[j].State] })
	return out
}

// Risk is the review's risk band.
type Risk struct {
	Level   string
	Reasons []string
}

var riskRules = []struct {
	level  string
	reason string
	match  func(p string) bool
}{
	{RiskHigh, "touches sign-in or secrets (path rule)", func(p string) bool {
		return hasAny(p, "auth", "secret", "token", "password", "credential", "crypto", "keyring", "login")
	}},
	{RiskHigh, "changes a database migration (path rule)", func(p string) bool { return hasAny(p, "migration", "migrate/", "schema.sql") }},
	{RiskMedium, "touches network code (path rule)", func(p string) bool {
		return hasAny(p, "webhook", "http", "net/", "socket", "grpc", "client", "api/")
	}},
	{RiskMedium, "changes CI or the build (path rule)", func(p string) bool {
		return hasAny(p, ".github/", "dockerfile", "makefile", ".gitlab-ci", "build.")
	}},
}

func hasAny(p string, words ...string) bool {
	p = strings.ToLower(p)
	for _, w := range words {
		if strings.Contains(p, w) {
			return true
		}
	}
	return false
}

var riskRank = map[string]int{RiskLow: 0, RiskMedium: 1, RiskHigh: 2}

// largeChange is how many changed lines make a change large.
const largeChange = 400

// ReviewRisk rates the files by path rules and size; the higher wins and
// its reason comes first.
func ReviewRisk(files []ReviewFile) Risk {
	r := Risk{Level: RiskLow}
	add := func(level, reason string) {
		for _, x := range r.Reasons {
			if x == reason {
				return
			}
		}
		if riskRank[level] > riskRank[r.Level] {
			r.Level = level
			r.Reasons = append([]string{reason}, r.Reasons...)
			return
		}
		r.Reasons = append(r.Reasons, reason)
	}
	lines, onlySafe := 0, true
	for _, f := range files {
		lines += f.Add + f.Del
		k := fileKind(f.Path)
		if k != "docs" && k != "test" {
			onlySafe = false
		}
		for _, rule := range riskRules {
			if rule.match(f.Path) {
				add(rule.level, rule.reason)
			}
		}
	}
	if lines > largeChange {
		add(RiskMedium, fmt.Sprintf("large change: %d lines", lines))
	}
	if len(r.Reasons) == 0 {
		if onlySafe && len(files) > 0 {
			r.Reasons = []string{"docs and tests only"}
		} else {
			r.Reasons = []string{"no risky paths"}
		}
	}
	return r
}

// Suggestion is the review's one short suggestion; Notes counts what
// stands before an approval.
type Suggestion struct {
	Text  string
	Notes int
}

// ReviewSuggestion is one short line from the proof.
func ReviewSuggestion(rows []ProofRow) Suggestion {
	var red, stale []string
	for _, r := range rows {
		switch r.State {
		case ProofFail:
			red = append(red, r.Name)
		case ProofStale:
			stale = append(stale, r.Name)
		}
	}
	switch {
	case len(red) > 0:
		return Suggestion{Text: "Approve after " + strings.Join(red, ", ") + " is green.", Notes: len(red) + len(stale)}
	case len(stale) > 0:
		return Suggestion{Text: "Run " + strings.Join(stale, ", ") + " again on the new code.", Notes: len(stale)}
	}
	return Suggestion{Text: "Looks ready to approve."}
}

// CanApprove refuses an approval when the head moved since the user
// looked (seen) or the user's commit is not known.
func CanApprove(seen, head string) error {
	if seen == "" {
		return errors.New("Open the review first: the approval needs the commit you saw.")
	}
	if seen != head && !strings.HasPrefix(head, seen) {
		return fmt.Errorf("New code came in. You saw %s. Now it is %s. Approve was not sent.", ShortSHA(seen), ShortSHA(head))
	}
	return nil
}
