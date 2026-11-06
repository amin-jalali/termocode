package app

import (
	"encoding/json"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Autolinks (GITX-29, O24, P11): in a terminal tab a click on a commit
// hash of the repo opens the commit view, #123 opens the issue (or PR)
// on the remote's web page, and an issue key (ABC-123) opens the page of
// the git_issue_link setting. Open URL does the same for the word under
// the cursor when its line has no URL.

const gitxIssueLinkRowID = "setting-git-issue-link"

func init() {
	settingsRows = append(settingsRows, settingsRow{ID: gitxIssueLinkRowID, Label: "git_issue_link", Kind: "string", Default: "(none)",
		Desc: "The web page of an issue key such as ABC-123, with {key} where the key goes (https://tracker.example.com/browse/{key}). A click on a key in a terminal tab opens it. Empty or (none): keys are no links."})
	aiSettingPolicies["git_issue_link"] = aiNever("sets the page a click on an issue key opens; only the user does that")
}

// gitxIssueLink reads git_issue_link from config.json ("" = none).
func gitxIssueLink() string {
	path, err := settingsConfigPath()
	if err != nil {
		return ""
	}
	var s struct {
		Link string `json:"git_issue_link"`
	}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return strings.TrimSpace(s.Link)
}

func gitxIssueLinkValue(id string) (string, bool) {
	if id != gitxIssueLinkRowID {
		return "", false
	}
	if v := gitxIssueLink(); v != "" {
		return v, true
	}
	return "(none)", true
}

func (m *Model) applyGitxIssueLink(id, value string) (tea.Cmd, bool) {
	if id != gitxIssueLinkRowID {
		return nil, false
	}
	value = strings.TrimSpace(value)
	if value == "(none)" {
		value = ""
	}
	if value != "" {
		if _, ok := git.KeyLink(value, "ABC-1"); !ok {
			return m.pushToastDetail(toast.Errr, "git_issue_link: expected an http(s) link with {key}", "for example https://tracker.example.com/browse/{key}"), true
		}
	}
	if err := saveHTTPSetting("git_issue_link", value); err != nil {
		return m.pushToastDetail(toast.Errr, "Settings error", err.Error()), true
	}
	if value == "" {
		value = "(none)"
	}
	return m.pushToastDetail(toast.Info, "git_issue_link = "+value, ""), true
}

// gitAutoLinkAt runs the autolink under byte offset off of line, read in
// folder dir. ok=false: no link there (a hash not in the repo, #123 with
// no remote page, a key with no git_issue_link).
func (m *Model) gitAutoLinkAt(line string, off int, dir string) (tea.Cmd, bool) {
	l, ok := git.AutoLinkAt(line, off)
	if !ok || dir == "" {
		return nil, false
	}
	switch l.Kind {
	case git.LinkSHA:
		full, err := git.ResolveCommit(dir, l.Text)
		if err != nil {
			return nil, false
		}
		return m.gitOpenCommitViewCmd(full), true
	case git.LinkIssue:
		base, err := git.RemoteWeb(dir)
		if err != nil {
			return nil, false
		}
		return m.gitOpenWebLink(git.IssueLink(base, l.Text)), true
	case git.LinkKey:
		link, ok := git.KeyLink(gitxIssueLink(), l.Text)
		if !ok {
			return nil, false
		}
		return m.gitOpenWebLink(link), true
	}
	return nil, false
}

// gitOpenWebLink opens link in the browser; over SSH (no browser on this
// machine) it copies the link instead.
func (m *Model) gitOpenWebLink(link string) tea.Cmd {
	if mcpOverSSH() {
		m.copyToClipboard(link)
		return m.pushToastDetail(toast.Info, "Copied "+link, "open it in a browser on your own computer")
	}
	if err := osOpen(link); err != nil {
		return m.pushToastDetail(toast.Warn, "Open the page in your browser", link)
	}
	return m.pushToastDetail(toast.Info, "Opened", link)
}

// gitAutoLinkDir is the repo the links are read in: the workspace, so
// a hash opens in the commit view of the same repo.
func (m *Model) gitAutoLinkDir() string {
	dir, err := m.rootDir()
	if err != nil {
		return ""
	}
	return dir
}
