package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Before-commit checks (GITX-27; B20–B22, P7, P8). All of them are off
// until the user turns them on in Settings → Git:
//
//   - git_commit_checks: large staged files, secret-looking added lines
//     and errors of the Problems list in staged files,
//   - git_commit_format: format the fully staged files with the language
//     server and stage the result,
//   - git_commit_task: a task (Tasks: Run Task) that must pass.
//
// A finding stops the commit at a Cancel-first card: Show Report opens
// the whole report, Commit Anyway commits as it is. A secret is named by
// file and line only; its text never reaches the card or the report.

const (
	gitxChecksRowID  = "setting-git-commit-checks"
	gitxFormatRowID  = "setting-git-commit-format"
	gitxTaskRowID    = "setting-git-commit-task"
	gitxLargeRowID   = "setting-git-commit-large-mb"
	gitxLargeDefault = 5
	gitxTaskTimeout  = 10 * time.Minute
	gitxTaskOutMax   = 32 << 10
	gitxCardLines    = 6
)

// gitxChecks is the state of the before-commit checks.
type gitxChecks struct {
	running bool   // a check run is on its way
	passed  bool   // the next commit skips the checks (they passed, or Commit Anyway)
	card    string // the last card's title and text (tests read it)
	report  string // the last report
}

func init() {
	settingsRows = append(settingsRows,
		settingsRow{ID: gitxChecksRowID, Label: "git_commit_checks", Kind: "bool", Default: "false",
			Desc: "Before each commit, look for large staged files, secret-looking added lines and errors of the Problems list in staged files. A finding asks before the commit (Commit Anyway or Cancel)."},
		settingsRow{ID: gitxFormatRowID, Label: "git_commit_format", Kind: "bool", Default: "false",
			Desc: "Before each commit, format the fully staged files that are open in the editor with their language server and stage the result. A file with unstaged changes too is left as it is."},
		settingsRow{ID: gitxTaskRowID, Label: "git_commit_task", Kind: "string", Default: "(none)",
			Desc: "Before each commit, run this task (its label, as in Tasks: Run Task). The commit waits; a task that fails asks before the commit. Empty or (none): no task."},
		settingsRow{ID: gitxLargeRowID, Label: "git_commit_large_mb", Kind: "int", Default: strconv.Itoa(gitxLargeDefault),
			Desc: "git_commit_checks: a staged file bigger than this many MB is a finding."},
	)
	aiSettingPolicies["git_commit_checks"] = aiNever("changes what runs before the user's commits; only the user does that")
	aiSettingPolicies["git_commit_format"] = aiNever("changes the user's files before each commit; only the user turns it on")
	aiSettingPolicies["git_commit_task"] = aiNever("sets a shell command that runs before each commit; only the user does that")
	aiSettingPolicies["git_commit_large_mb"] = aiNever("changes what runs before the user's commits; only the user does that")
	registerGitx(gitxFeature{
		palette: []picker.Item{{ID: "git-commit-checks", Title: "Git: Run Before-Commit Checks", Hint: "the checks of Settings → Git on what is staged; no commit"}},
		policies: map[string]aiActionPolicy{
			"git-commit-checks": aiNever("runs the user's before-commit task, a shell command"),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			if id != "git-commit-checks" {
				return nil, false
			}
			return m.gitxRunChecksNow(), true
		},
	})
}

// gitxCheckCfg is what the settings turn on.
type gitxCheckCfg struct {
	scan, format bool
	task         string
	largeMB      int
}

func (c gitxCheckCfg) any() bool { return c.scan || c.format || c.task != "" }

// gitxCheckSettings reads the check settings from config.json.
func gitxCheckSettings() gitxCheckCfg {
	cfg := gitxCheckCfg{largeMB: gitxLargeDefault}
	path, err := settingsConfigPath()
	if err != nil {
		return cfg
	}
	var s struct {
		Scan    *bool   `json:"git_commit_checks"`
		Format  *bool   `json:"git_commit_format"`
		Task    *string `json:"git_commit_task"`
		LargeMB *int    `json:"git_commit_large_mb"`
	}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	cfg.scan = s.Scan != nil && *s.Scan
	cfg.format = s.Format != nil && *s.Format
	if s.Task != nil {
		cfg.task = strings.TrimSpace(*s.Task)
	}
	if s.LargeMB != nil && *s.LargeMB > 0 {
		cfg.largeMB = *s.LargeMB
	}
	return cfg
}

// gitxCheckSettingValue renders a check row. ok=false: not ours.
func gitxCheckSettingValue(id string) (string, bool) {
	c := gitxCheckSettings()
	switch id {
	case gitxChecksRowID:
		return strconv.FormatBool(c.scan), true
	case gitxFormatRowID:
		return strconv.FormatBool(c.format), true
	case gitxTaskRowID:
		if c.task == "" {
			return "(none)", true
		}
		return c.task, true
	case gitxLargeRowID:
		return strconv.Itoa(c.largeMB), true
	}
	return "", false
}

// applyGitxCheckSetting checks and saves a check row. ok=false: not ours.
func (m *Model) applyGitxCheckSetting(id, value string) (tea.Cmd, bool) {
	value = strings.TrimSpace(value)
	var key string
	var v any
	switch id {
	case gitxChecksRowID, gitxFormatRowID:
		key = "git_commit_checks"
		if id == gitxFormatRowID {
			key = "git_commit_format"
		}
		b, err := parseBoolLoose(value)
		if err != nil {
			return m.pushToastDetail(toast.Errr, key+": expected true or false", ""), true
		}
		v = b
	case gitxTaskRowID:
		if value == "(none)" {
			value = ""
		}
		key, v = "git_commit_task", value
	case gitxLargeRowID:
		key = "git_commit_large_mb"
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 100000 {
			return m.pushToastDetail(toast.Errr, key+": expected a whole number of MB (1 or more)", ""), true
		}
		v = n
	default:
		return nil, false
	}
	if err := saveHTTPSetting(key, v); err != nil {
		return m.pushToastDetail(toast.Errr, "Settings error", err.Error()), true
	}
	return m.pushToastDetail(toast.Info, fmt.Sprintf("%s = %v", key, v), ""), true
}

// gitxCheckTask is the task to run, found on the UI loop.
type gitxCheckTask struct {
	label, line, dir string
	env              map[string]string
	missing          bool
}

// gitxCheckRun is one run of the checks.
type gitxCheckRun struct {
	cwd  string
	cfg  gitxCheckCfg
	task gitxCheckTask
	nv   *nvim.Client
}

// gitxCheckResult is what a run found.
type gitxCheckResult struct {
	findings []string // one short line each, for the card
	report   string
}

// gitxCheckRunFor reads the settings and the task for a run in cwd.
func (m *Model) gitxCheckRunFor(cwd string) gitxCheckRun {
	r := gitxCheckRun{cwd: cwd, cfg: gitxCheckSettings(), nv: m.nvim}
	if r.cfg.task != "" {
		r.task = gitxCheckTask{label: r.cfg.task, missing: true}
		list, _ := m.allTasks()
		v := m.taskVars()
		for _, t := range list {
			if t.Label == r.cfg.task {
				r.task = gitxCheckTask{label: t.Label, line: t.CommandLine(v), dir: t.Dir(v), env: t.Env}
				break
			}
		}
	}
	return r
}

// gitxBeforeCommit starts the checks before a commit. ok=false: no
// checks (turned off, or they already passed for this commit); commit.
func (m *Model) gitxBeforeCommit(cwd, msg string, amend, signoff bool, after string) (tea.Cmd, bool) {
	if m.gitx.checks.passed {
		m.gitx.checks.passed = false
		return nil, false
	}
	run := m.gitxCheckRunFor(cwd)
	if !run.cfg.any() {
		return nil, false
	}
	if m.gitx.checks.running {
		return m.gitxInfo("The before-commit checks are still running"), true
	}
	m.gitx.checks.running = true
	return tea.Batch(m.gitxInfo("Running the before-commit checks…"), func() tea.Msg {
		res := run.do()
		return gitxDoneMsg{apply: func(m *Model) tea.Cmd {
			m.gitx.checks.running = false
			m.gitx.checks.report = res.report
			if len(res.findings) == 0 {
				m.gitx.checks.passed = true
				return m.gitCommitStaged(cwd, msg, amend, signoff, after)
			}
			return m.gitxChecksCard(res, func(m *Model) tea.Cmd {
				m.gitx.checks.passed = true
				return m.gitCommitStaged(cwd, msg, amend, signoff, after)
			})
		}}
	}), true
}

// gitxRunChecksNow runs the checks on what is staged and shows the
// report; it never commits.
func (m *Model) gitxRunChecksNow() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	run := m.gitxCheckRunFor(cwd)
	run.cfg.scan = true // the palette row runs the scan even when it is off
	if m.gitx.checks.running {
		return m.gitxInfo("The before-commit checks are still running")
	}
	m.gitx.checks.running = true
	return tea.Batch(m.gitxInfo("Running the before-commit checks…"), func() tea.Msg {
		res := run.do()
		return gitxDoneMsg{apply: func(m *Model) tea.Cmd {
			m.gitx.checks.running = false
			m.gitx.checks.report = res.report
			return tea.Batch(fetchGitCmd(), m.gitxShowCheckReport())
		}}
	})
}

// gitxChecksCard asks what to do with the findings.
func (m *Model) gitxChecksCard(res gitxCheckResult, commit func(m *Model) tea.Cmd) tea.Cmd {
	n := len(res.findings)
	title := "Before-commit checks: " + gitxPlural(n, "finding")
	lines := res.findings
	if len(lines) > gitxCardLines {
		lines = append(append([]string(nil), lines[:gitxCardLines-1]...), fmt.Sprintf("…and %d more (Show Report)", n-gitxCardLines+1))
	}
	text := "• " + strings.Join(lines, "\n• ") + "\n\nCommit Anyway commits as it is. Show Report lists everything."
	m.gitx.checks.card = title + "\n" + text
	m.gitxCard(title, text, func(m *Model, id string) tea.Cmd {
		switch id {
		case "report":
			return m.gitxShowCheckReport()
		case "commit":
			return commit(m)
		}
		return nil
	}, confirm.Button{ID: "report", Title: "Show Report"}, confirm.Button{ID: "commit", Title: "Commit Anyway", Style: confirm.StyleDestructive})
	return nil
}

// gitxShowCheckReport opens the last report as a read-only page.
func (m *Model) gitxShowCheckReport() tea.Cmd {
	m.ensureEditorWindowCurrent()
	if err := m.openReadOnlyPage("Before-Commit Checks", m.gitx.checks.report, "", "text"); err != nil {
		return m.gitxFail("Could not open the report", err)
	}
	return nil
}

// gitxFormatted is what gitxFormatFile says when it changed the file.
const gitxFormatted = "formatted"

// gitxFormatFile formats one file that is open in the editor with its
// language server and saves it. It returns gitxFormatted, or why the
// file was left as it is. A var: tests stand in for the server.
var gitxFormatFile = func(c *nvim.Client, abs string) (string, error) {
	if c == nil {
		return "the editor is not running", nil
	}
	return c.EvalLuaStringArgs(gitxFormatLua, abs)
}

const gitxFormatLua = `
local path = ...
local b = vim.fn.bufnr(path)
if b == -1 or not vim.api.nvim_buf_is_loaded(b) then return 'not open in the editor' end
if vim.bo[b].modified then return 'has unsaved edits in the editor' end
local get = vim.lsp.get_clients or vim.lsp.get_active_clients
local can = false
for _, c in ipairs(get({ bufnr = b })) do
  if (c.server_capabilities or {}).documentFormattingProvider then can = true end
end
if not can then return 'no formatter for this file' end
local tick = vim.api.nvim_buf_get_changedtick(b)
local ok, e = pcall(vim.lsp.buf.format, { bufnr = b, async = false, timeout_ms = 3000 })
if not ok then return 'format failed: ' .. tostring(e) end
if vim.api.nvim_buf_get_changedtick(b) == tick then return 'already formatted' end
vim.api.nvim_buf_call(b, function() vim.cmd('silent! write') end)
return 'formatted'
`

// do runs the checks; it runs off the UI loop.
func (r gitxCheckRun) do() gitxCheckResult {
	var res gitxCheckResult
	var rep strings.Builder
	fmt.Fprintf(&rep, "Before-commit checks, %s\n\n", time.Now().Format("2006-01-02 15:04:05"))
	add := func(f string) { res.findings = append(res.findings, f) }

	if r.cfg.format {
		rep.WriteString(r.format())
	}
	files, err := git.StagedFiles(r.cwd)
	if err != nil {
		add("Could not read the staged files: " + err.Error())
	}
	if r.cfg.scan {
		limit := int64(r.cfg.largeMB) << 20
		var big []string
		for _, f := range files {
			if f.Size > limit {
				line := fmt.Sprintf("%s is %s (limit %d MB)", f.Path, gitxMB(f.Size), r.cfg.largeMB)
				big = append(big, line)
				add(line)
			}
		}
		gitxReportSection(&rep, fmt.Sprintf("Large files (over %d MB)", r.cfg.largeMB), big)

		var secrets []string
		lines, err := git.StagedAddedLines(r.cwd)
		if err != nil {
			add("Could not read the staged changes: " + err.Error())
		}
		for _, l := range lines {
			if gitxLooksSecret(l.Text) {
				where := fmt.Sprintf("%s:%d", l.Path, l.Line)
				secrets = append(secrets, where)
				add(where + " looks like a secret (a key or token)")
			}
		}
		gitxReportSection(&rep, "Secret-looking added lines (the text is not shown)", secrets)

		probs := r.problems(files)
		if len(probs) > 0 {
			add(gitxPlural(len(probs), "error") + " in the Problems list in staged files")
		}
		gitxReportSection(&rep, "Errors in the Problems list (staged files)", probs)
	}
	if r.cfg.task != "" {
		out, f := r.runTask()
		if f != "" {
			add(f)
		}
		rep.WriteString(out)
	}
	if len(res.findings) == 0 {
		rep.WriteString("No problems found.\n")
	}
	res.report = rep.String()
	return res
}

// gitxReportSection writes one part of the report.
func gitxReportSection(b *strings.Builder, title string, lines []string) {
	if len(lines) == 0 {
		fmt.Fprintf(b, "%s: none\n\n", title)
		return
	}
	fmt.Fprintf(b, "%s:\n", title)
	for _, l := range lines {
		b.WriteString("  " + l + "\n")
	}
	b.WriteString("\n")
}

func gitxMB(n int64) string { return fmt.Sprintf("%.1f MB", float64(n)/(1<<20)) }

// gitxLooksSecret reports a line that holds an API key, a token or a
// private key (the patterns the AI redaction uses).
func gitxLooksSecret(s string) bool {
	if len(s) > 8192 {
		s = s[:8192]
	}
	if strings.Contains(s, "-----BEGIN") && strings.Contains(s, "PRIVATE KEY") {
		return true
	}
	return ai.Redact(s) != s
}

// problems lists the errors the editor knows in the staged files.
func (r gitxCheckRun) problems(files []git.StagedFile) []string {
	if r.nv == nil || len(files) == 0 {
		return nil
	}
	diags, err := r.nv.AllDiagnostics()
	if err != nil {
		return []string{"could not read the Problems list: " + err.Error()}
	}
	staged := map[string]string{}
	for _, f := range files {
		abs := filepath.Join(r.cwd, filepath.FromSlash(f.Path))
		staged[abs] = f.Path
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			staged[real] = f.Path
		}
	}
	var out []string
	for _, d := range diags {
		if d.Severity != 1 {
			continue
		}
		rel, ok := staged[filepath.Clean(d.Path)]
		if !ok {
			if real, err := filepath.EvalSymlinks(d.Path); err == nil {
				rel, ok = staged[real]
			}
		}
		if ok {
			out = append(out, fmt.Sprintf("%s:%d  %s", rel, d.Line, strings.ReplaceAll(d.Message, "\n", " ")))
		}
	}
	return out
}

// format formats the fully staged files and stages the result.
func (r gitxCheckRun) format() string {
	var b strings.Builder
	files, err := git.StagedFiles(r.cwd)
	if err != nil {
		return "Format: could not read the staged files: " + err.Error() + "\n\n"
	}
	unstaged, _ := git.UnstagedPaths(r.cwd)
	partly := map[string]bool{}
	for _, p := range unstaged {
		partly[p] = true
	}
	var done, skipped []string
	for _, f := range files {
		if partly[f.Path] {
			skipped = append(skipped, f.Path+": also has unstaged changes")
			continue
		}
		res, err := gitxFormatFile(r.nv, filepath.Join(r.cwd, filepath.FromSlash(f.Path)))
		switch {
		case err != nil:
			skipped = append(skipped, f.Path+": "+err.Error())
		case res == gitxFormatted:
			if err := git.Stage(r.cwd, f.Path); err != nil {
				skipped = append(skipped, f.Path+": formatted but not staged: "+err.Error())
				continue
			}
			done = append(done, f.Path)
		default:
			skipped = append(skipped, f.Path+": "+res)
		}
	}
	gitxReportSection(&b, "Formatted and staged", done)
	gitxReportSection(&b, "Not formatted", skipped)
	return b.String()
}

// runTask runs the before-commit task. finding is "" when it passed.
func (r gitxCheckRun) runTask() (report, finding string) {
	t := r.task
	if t.missing {
		f := fmt.Sprintf("Task %q (git_commit_task) is not in Tasks: Run Task", t.label)
		return f + "\n\n", f
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitxTaskTimeout)
	defer cancel()
	cmd := spawn.CommandContext(ctx, "sh", "-c", `cd "$1" && eval "$2"`, "sh", t.dir, t.line)
	cmd.Dir = r.cwd
	if len(t.env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range t.env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	out, err := cmd.CombinedOutput()
	text := string(out)
	if len(text) > gitxTaskOutMax {
		text = "…\n" + text[len(text)-gitxTaskOutMax:]
	}
	text = ai.Redact(text)
	var b strings.Builder
	fmt.Fprintf(&b, "Task %q: %s\n", t.label, t.line)
	switch {
	case ctx.Err() != nil:
		finding = fmt.Sprintf("Task %s did not end in %s", t.label, gitxTaskTimeout)
	case err != nil:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			finding = fmt.Sprintf("Task %s failed (exit %d)", t.label, ee.ExitCode())
		} else {
			finding = fmt.Sprintf("Task %s did not start: %v", t.label, err)
		}
	}
	if finding != "" {
		b.WriteString("  " + finding + "\n")
	} else {
		b.WriteString("  passed\n")
	}
	if strings.TrimSpace(text) != "" {
		b.WriteString("Output:\n" + text)
		if !strings.HasSuffix(text, "\n") {
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	return b.String(), finding
}
