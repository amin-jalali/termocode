package ghci

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
)

// MaxLogLines caps the log part that goes to the AI chat.
const MaxLogLines = 200

// FailedLog is the failing part of a run's log.
type FailedLog struct {
	Job       string   // the first failed job
	Step      string   // its failing step
	OtherJobs []string // more failed jobs (named in the prompt)
	Lines     []string // ANSI and timestamps stripped, trimmed to MaxLogLines
	All       []string // every line of Job (for the log preview)
	Total     int      // lines of Job before trimming
}

// FetchFailedLog runs `gh run view <id> --log-failed` and parses it.
func FetchFailedLog(ctx context.Context, run Runner, dir string, id int64) (FailedLog, error) {
	if id <= 0 {
		return FailedLog{}, fmt.Errorf("no run")
	}
	out, errOut, err := run(ctx, dir, "run", "view", strconv.FormatInt(id, 10), "--log-failed")
	if err != nil {
		return FailedLog{}, classify(err, errOut)
	}
	return ParseFailedLog(out), nil
}

// tsRe is the timestamp GitHub puts before each log line.
var tsRe = regexp.MustCompile(`^\x{FEFF}?\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?Z ?`)

// errRe finds the first line that looks like the failure.
var errRe = regexp.MustCompile(`(?i)##\[error\]|\berror\b|--- fail|\bfail(ed|ure)?\b|panic:|✗`)

// ParseFailedLog reads `gh run view --log-failed` output: one
// "job<TAB>step<TAB>timestamp text" line per log line.
func ParseFailedLog(out string) FailedLog {
	var fl FailedLog
	var lines []string
	var steps []string
	errStep := ""
	for _, raw := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		parts := strings.SplitN(raw, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		job, step, text := parts[0], parts[1], parts[2]
		if fl.Job == "" {
			fl.Job = job
		}
		if job != fl.Job {
			if !contains(fl.OtherJobs, job) {
				fl.OtherJobs = append(fl.OtherJobs, job)
			}
			continue
		}
		text = tsRe.ReplaceAllString(ansi.Strip(text), "")
		if strings.HasPrefix(text, "##[endgroup]") {
			continue
		}
		lines = append(lines, text)
		steps = append(steps, step)
		if errStep == "" && errRe.MatchString(text) {
			errStep = step
		}
	}
	fl.Total = len(lines)
	fl.All = lines
	fl.Lines = TrimLog(lines, MaxLogLines)
	switch {
	case errStep != "" && errStep != "UNKNOWN STEP":
		fl.Step = errStep
	case len(steps) > 0:
		fl.Step = steps[len(steps)-1]
	}
	if fl.Step == "UNKNOWN STEP" {
		fl.Step = ""
	}
	return fl
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// TrimLog keeps at most max lines: the window that starts a little
// before the first error line, else the last max lines.
func TrimLog(lines []string, max int) []string {
	if len(lines) <= max {
		return lines
	}
	for i, l := range lines {
		if errRe.MatchString(l) {
			start := i - 20
			if start < 0 {
				start = 0
			}
			if start+max > len(lines) {
				start = len(lines) - max
			}
			return lines[start : start+max]
		}
	}
	return lines[len(lines)-max:]
}

// FixPrompt is the chat message that asks the AI to fix a failed run:
// where it failed and the failing log part, with secrets redacted.
func FixPrompt(branch string, r Run, fl FailedLog) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CI failed on branch %s", branch)
	if r.Workflow != "" {
		fmt.Fprintf(&b, " (workflow %s)", r.Workflow)
	}
	b.WriteString(".\n")
	if fl.Job != "" {
		fmt.Fprintf(&b, "Job: %s\n", fl.Job)
	}
	if fl.Step != "" {
		fmt.Fprintf(&b, "Step: %s\n", fl.Step)
	}
	if len(fl.OtherJobs) > 0 {
		fmt.Fprintf(&b, "Other failed jobs: %s\n", strings.Join(fl.OtherJobs, ", "))
	}
	if r.URL != "" {
		fmt.Fprintf(&b, "Run: %s\n", r.URL)
	}
	if len(fl.Lines) > 0 {
		if len(fl.Lines) < fl.Total {
			fmt.Fprintf(&b, "\nFailing log (%d of %d lines):\n", len(fl.Lines), fl.Total)
		} else {
			b.WriteString("\nFailing log:\n")
		}
		b.WriteString("```text\n")
		b.WriteString(ai.Redact(strings.Join(fl.Lines, "\n")))
		b.WriteString("\n```\n")
	}
	b.WriteString("\nFind the cause and fix it.")
	return b.String()
}
