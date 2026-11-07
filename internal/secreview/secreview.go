// Package secreview builds a security review request: it scans the added
// lines of a diff for risky spots (hints), reads the project's threat
// model (.termocode/threat-model.md) and writes the request the user
// sends to the AI. It never runs code and never sends anything itself.
package secreview

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// MaxModel is the most bytes of the threat model put in a request.
const MaxModel = 16 << 10

// Hint is one risky added line the reviewer should look at.
type Hint struct {
	File string
	Line int    // line in the new file, 1-based
	Kind string // what the rule found, e.g. "command exec"
	Text string // the line, trimmed (empty for secrets)
}

// Where is "file:line".
func (h Hint) Where() string { return h.File + ":" + strconv.Itoa(h.Line) }

type rule struct {
	kind   string
	re     *regexp.Regexp
	noText bool // do not copy the line (it may hold a secret)
}

var rules = []rule{
	{kind: "secret in code", noText: true, re: regexp.MustCompile(`(?i)(password|passwd|secret|api[_-]?key|token)\w*["']?\s*(:=|=|:)\s*["'][^"'\s]{6,}["']`)},
	{kind: "command exec", re: regexp.MustCompile(`\bexec\.Command(Context)?\(|\bos\.system\(|\bsubprocess\.|\bchild_process\b|Runtime\.getRuntime\(\)\.exec`)},
	{kind: "TLS check off", re: regexp.MustCompile(`InsecureSkipVerify:\s*true|verify\s*=\s*False|rejectUnauthorized:\s*false`)},
	{kind: "SQL built from strings", re: regexp.MustCompile(`(?i)["'` + "`" + `]\s*(select|insert|update|delete)\b[^"'` + "`" + `]*["'` + "`" + `]\s*\+|Sprintf\(\s*["'` + "`" + `]\s*(select|insert|update|delete)\b`)},
	{kind: "raw HTML", re: regexp.MustCompile(`template\.HTML\(|dangerouslySetInnerHTML|\.innerHTML\s*=|\|\s*safe\b`)},
	{kind: "eval", re: regexp.MustCompile(`\beval\(|\bnew Function\(`)},
	{kind: "unsafe decode", re: regexp.MustCompile(`pickle\.loads?\(|\byaml\.load\(|ObjectInputStream|\bMarshal\.load\b`)},
	{kind: "weak hash", re: regexp.MustCompile(`\b(md5|sha1)\.(New|Sum)|hashlib\.(md5|sha1)\(|createHash\(["'](md5|sha1)`)},
	{kind: "path from input", re: regexp.MustCompile(`(filepath\.Join|os\.Open|os\.ReadFile|open)\([^)]*\b(r\.URL|req\.|request\.|params|query)`)},
	{kind: "network listener", re: regexp.MustCompile(`\bhttp\.HandleFunc\(|ListenAndServe(TLS)?\(|\bnet\.Listen\(|\bapp\.(get|post|put|delete)\(`)},
	{kind: "wide file mode", re: regexp.MustCompile(`\b0o?7[67]7\b|\b0o?666\b`)},
	{kind: "unsafe memory", re: regexp.MustCompile(`\bunsafe\.Pointer\b`)},
}

var hunkRe = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// ScanDiff returns the hints of the added lines of a unified diff, one per
// line (the first rule that matches), at most max (0: no limit).
func ScanDiff(diff string, max int) []Hint {
	var out []Hint
	file, line := "", 0
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "+++ "):
			file = strings.TrimPrefix(strings.TrimPrefix(l, "+++ "), "b/")
			if file == "/dev/null" {
				file = ""
			}
			continue
		case strings.HasPrefix(l, "--- "), strings.HasPrefix(l, "diff --git "):
			continue
		case strings.HasPrefix(l, "@@"):
			if m := hunkRe.FindStringSubmatch(l); m != nil {
				line, _ = strconv.Atoi(m[1])
			}
			continue
		}
		if file == "" || line == 0 {
			continue
		}
		switch {
		case strings.HasPrefix(l, "+"):
			code := l[1:]
			for _, r := range rules {
				if !r.re.MatchString(code) {
					continue
				}
				h := Hint{File: file, Line: line, Kind: r.kind}
				if !r.noText {
					h.Text = clip(strings.TrimSpace(code), 120)
				}
				out = append(out, h)
				break
			}
			line++
		case strings.HasPrefix(l, "-"), strings.HasPrefix(l, `\`):
		default:
			line++
		}
		if max > 0 && len(out) >= max {
			return out
		}
	}
	return out
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// Path is where the project keeps its threat model.
func Path(root string) string { return filepath.Join(root, ".termocode", "threat-model.md") }

// Load reads the threat model (at most MaxModel bytes). ok is false when
// there is none.
func Load(root string) (text string, ok bool, err error) {
	b, err := os.ReadFile(Path(root))
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if len(b) > MaxModel {
		b = append(b[:MaxModel:MaxModel], "\n[cut here: the threat model is longer]\n"...)
	}
	return string(b), true, nil
}

// Template is a new threat model to fill in, with the STRIDE threats.
func Template(project string) string {
	return "# Threat model: " + project + `

The security review reads this file. Keep it short and true.

## Assets
- What must stay safe (data, keys, accounts, money)?

## Entry points
- Where does outside input come in (HTTP routes, CLI flags, files, queues)?

## Trust boundaries
- Where does trusted code meet untrusted input or another service?

## Threats (STRIDE)
- Spoofing: who could pretend to be someone else?
- Tampering: what data could be changed on the way or at rest?
- Repudiation: what actions need a log?
- Information disclosure: what could leak (logs, errors, files)?
- Denial of service: what could be made slow or crash?
- Elevation of privilege: how could a user get more rights?

## Out of scope
- What this model does not cover.
`
}

// Input is what one review covers.
type Input struct {
	Branch bool   // the branch since Base; else the uncommitted changes
	Base   string // base branch of a branch review
	Diff   string
	Cut    bool   // the diff was cut
	Model  string // the saved threat model ("" = none)
	Hints  []Hint
}

// Prompt is the request the user sends to the AI.
func Prompt(in Input) string {
	var b strings.Builder
	if in.Branch {
		fmt.Fprintf(&b, "Do a security review of this branch as a pull request into %s.\n", in.Base)
	} else {
		b.WriteString("Do a security review of my uncommitted changes.\n")
	}
	if strings.TrimSpace(in.Model) == "" {
		b.WriteString("No threat model is saved for this project. First write a short one: assets, entry points, trust boundaries and the STRIDE threats that fit. I can save it in .termocode/threat-model.md.\n")
	} else {
		b.WriteString("Use the project's threat model below.\n")
	}
	b.WriteString("Then list only real, reachable security findings, most important first. For each one give:\n" +
		"- severity: P0 (critical), P1 (high), P2 (medium) or P3 (low)\n" +
		"- file:line\n" +
		"- the STRIDE threat and how an attacker would use it\n" +
		"- the fix\n" +
		"If there are none, say \"No findings\". End with what the threat model should add.\n")
	if strings.TrimSpace(in.Model) != "" {
		b.WriteString("\nThreat model:\n```markdown\n" + strings.TrimRight(in.Model, "\n") + "\n```\n")
	}
	if len(in.Hints) > 0 {
		b.WriteString("\nA quick scan marked these added lines (check them, they may be fine):\n")
		for _, h := range in.Hints {
			b.WriteString("- " + h.Where() + " " + h.Kind)
			if h.Text != "" {
				b.WriteString(": " + h.Text)
			}
			b.WriteString("\n")
		}
	}
	diff := strings.TrimRight(in.Diff, "\n")
	if in.Cut {
		diff += "\n[cut here: the rest is longer]"
	}
	b.WriteString("\n```diff\n" + diff + "\n```")
	return b.String()
}
