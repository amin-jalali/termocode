package app

import (
	"os"
	"testing"
)

// GITX-29 (O24, P11): a click on a commit hash of the repo opens the
// commit view; #123 opens the issue (or PR) on the remote's page; an
// issue key opens the git_issue_link page. Anything else is not a link,
// so the click goes on to the terminal.
func TestGitxAutoLinkClick(t *testing.T) {
	path, err := settingsConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path) })
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	var opened []string
	old := osOpen
	osOpen = func(u string) error { opened = append(opened, u); return nil }
	t.Cleanup(func() { osOpen = old })

	m, dir := covGitRepo(t)
	head := covGit(t, dir, "rev-parse", "HEAD")
	line := "commit " + head[:8] + " fixes #12 and ABC-7"
	if cmd, ok := m.gitAutoLinkAt(line, 9, dir); !ok || cmd == nil || m.commitViewS().want != head {
		t.Fatalf("hash: ok=%v want=%q", ok, m.commitViewS().want)
	}
	if _, ok := m.gitAutoLinkAt("commit 1a2b3c4d here", 9, dir); ok {
		t.Error("a hash that is not in the repo is a link")
	}
	if _, ok := m.gitAutoLinkAt(line, 2, dir); ok {
		t.Error("a plain word is a link")
	}
	if _, ok := m.gitAutoLinkAt(line, 23, dir); ok || len(opened) != 0 {
		t.Errorf("#12 with no remote: ok=%v opened %v", ok, opened)
	}
	covGit(t, dir, "remote", "add", "origin", "git@github.com:o/r.git")
	if _, ok := m.gitAutoLinkAt(line, 23, dir); !ok || len(opened) != 1 || opened[0] != "https://github.com/o/r/issues/12" {
		t.Fatalf("#12: ok=%v opened %v", ok, opened)
	}
	if _, ok := m.gitAutoLinkAt(line, len(line)-2, dir); ok || len(opened) != 1 {
		t.Errorf("a key with no git_issue_link: ok=%v opened %v", ok, opened)
	}

	if v, ok := gitxSettingValue(gitxIssueLinkRowID); !ok || v != "(none)" {
		t.Fatalf("default value %q %v", v, ok)
	}
	if c := settingsCategories()[gitxIssueLinkRowID]; c != "Git" {
		t.Fatalf("category %q", c)
	}
	if _, ok := aiSettingPolicies["git_issue_link"]; !ok {
		t.Fatal("no AI policy for git_issue_link")
	}
	if _, ok := m.applyGitxSetting(gitxIssueLinkRowID, "javascript:alert(1)"); !ok || gitxIssueLink() != "" {
		t.Fatalf("a bad link was saved: %q", gitxIssueLink())
	}
	if _, ok := m.applyGitxSetting(gitxIssueLinkRowID, "https://t.example.com/browse/{key}"); !ok {
		t.Fatal("not our row")
	}
	if _, ok := m.gitAutoLinkAt(line, len(line)-2, dir); !ok || len(opened) != 2 || opened[1] != "https://t.example.com/browse/ABC-7" {
		t.Fatalf("key: ok=%v opened %v", ok, opened)
	}

	// Over SSH there is no browser here: the link is copied instead.
	t.Setenv("SSH_CONNECTION", "1.2.3.4 5 6.7.8.9 22")
	var copied []string
	defer SetClipboardSink(func(s string) { copied = append(copied, s) })()
	if _, ok := m.gitAutoLinkAt(line, 23, dir); !ok || len(opened) != 2 || len(copied) != 1 || copied[0] != "https://github.com/o/r/issues/12" {
		t.Fatalf("over SSH: opened %v copied %v", opened, copied)
	}
}
