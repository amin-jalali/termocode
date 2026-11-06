package git

import (
	"fmt"
	"strings"
)

// Tags, remotes and force push (GITX-11, GITX-13, GITX-14).

func gitRemoteRun(dir string, args ...string) error {
	cmd := remoteCmd(dir, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmdError(err, out)
	}
	return nil
}

// CreateTag tags rev: a lightweight tag when message is "", else an
// annotated one. An existing tag is refused.
func CreateTag(dir, name, rev, message string) error {
	if rev == "" {
		rev = "HEAD"
	}
	if message == "" {
		return gitRun(dir, "tag", "--", name, rev)
	}
	return gitRun(dir, "tag", "-a", "-m", message, "--", name, rev)
}

// DeleteTag deletes a local tag.
func DeleteTag(dir, name string) error {
	return gitRun(dir, "tag", "-d", "--", name)
}

// TagTip is the short hash of the commit a tag points at ("" when
// unknown): `git tag <name> <tip>` brings a deleted tag back.
func TagTip(dir, name string) string {
	out, err := runOut(dir, "git", "rev-parse", "--short", "--verify", "-q", "refs/tags/"+name+"^{commit}")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// TagInfo is one tag's details.
type TagInfo struct {
	Name      string
	Annotated bool
	Tagger    string // "Name <email>" (annotated only)
	Date      string // YYYY-MM-DD of the tag (annotated) or the commit
	Message   string // annotated only
	Commit    string // short hash
	Subject   string // the commit's subject
}

// TagDetail reads a tag's details.
func TagDetail(dir, name string) (TagInfo, error) {
	out, err := runCombined(dir, "git", "for-each-ref", "--format=%(objecttype)%1f%(taggername) %(taggeremail)%1f%(taggerdate:short)%1f%(contents)", "refs/tags/"+name)
	if err != nil {
		return TagInfo{}, err
	}
	f := strings.SplitN(strings.TrimRight(out, "\n"), "\x1f", 4)
	if len(f) < 4 {
		return TagInfo{}, fmt.Errorf("no tag %s", name)
	}
	t := TagInfo{Name: name, Annotated: f[0] == "tag"}
	if t.Annotated {
		t.Tagger, t.Date, t.Message = strings.TrimSpace(f[1]), f[2], strings.TrimSpace(f[3])
	}
	t.Commit = TagTip(dir, name)
	head, err := runOut(dir, "git", "log", "-1", "--format=%s%x1f%cs", "refs/tags/"+name+"^{commit}")
	if err == nil {
		s, d, _ := strings.Cut(strings.TrimSpace(head), "\x1f")
		t.Subject = s
		if t.Date == "" {
			t.Date = d
		}
	}
	return t, nil
}

// PushTag pushes one tag to remote.
func PushTag(dir, remote, name string) error {
	return gitRemoteRun(dir, "push", remote, "refs/tags/"+name)
}

// PushAllTags pushes every local tag to remote.
func PushAllTags(dir, remote string) error {
	return gitRemoteRun(dir, "push", remote, "--tags")
}

// DeleteRemoteTag deletes a tag on remote; the local tag stays.
func DeleteRemoteTag(dir, remote, name string) error {
	return gitRemoteRun(dir, "push", remote, "--delete", "refs/tags/"+name)
}

// Remote is one configured remote.
type Remote struct {
	Name, FetchURL, PushURL string
}

// ListRemotes lists the remotes, sorted by name.
func ListRemotes(dir string) ([]Remote, error) {
	out, err := runCombined(dir, "git", "remote", "-v")
	if err != nil {
		return nil, err
	}
	var rs []Remote
	idx := map[string]int{}
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 3 {
			continue
		}
		i, ok := idx[f[0]]
		if !ok {
			i = len(rs)
			idx[f[0]] = i
			rs = append(rs, Remote{Name: f[0]})
		}
		switch f[2] {
		case "(fetch)":
			rs[i].FetchURL = f[1]
		case "(push)":
			rs[i].PushURL = f[1]
		}
	}
	return rs, nil
}

// AddRemote adds a remote; a name in use is refused.
func AddRemote(dir, name, url string) error { return gitRun(dir, "remote", "add", "--", name, url) }

// RenameRemote renames a remote (its tracking branches move too).
func RenameRemote(dir, old, name string) error { return gitRun(dir, "remote", "rename", "--", old, name) }

// SetRemoteURL changes a remote's URL.
func SetRemoteURL(dir, name, url string) error {
	return gitRun(dir, "remote", "set-url", "--", name, url)
}

// RemoveRemote removes a remote and its tracking branches.
func RemoveRemote(dir, name string) error { return gitRun(dir, "remote", "remove", "--", name) }

// UpstreamCandidates lists the remote-tracking branches ("origin/main")
// a branch can track, without the remotes' HEAD aliases.
func UpstreamCandidates(dir string) []string {
	out, err := runOut(dir, "git", "for-each-ref", "--format=%(refname:short)", "refs/remotes")
	if err != nil {
		return nil
	}
	var names []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if strings.Contains(l, "/") && !strings.HasSuffix(l, "/HEAD") {
			names = append(names, l)
		}
	}
	return names
}

// SetUpstream makes branch track upstream ("origin/main").
func SetUpstream(dir, branch, upstream string) error {
	return gitRun(dir, "branch", "--set-upstream-to="+upstream, "--", branch)
}

// UnsetUpstream stops branch tracking its upstream.
func UnsetUpstream(dir, branch string) error {
	return gitRun(dir, "branch", "--unset-upstream", "--", branch)
}

// RemoteTip is the full hash of remote/branch as this repository last
// saw it ("" when unknown).
func RemoteTip(dir, remote, branch string) string {
	out, err := runOut(dir, "git", "rev-parse", "--verify", "-q", "refs/remotes/"+remote+"/"+branch)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// PushForceWithLease replaces remote's branch with the local one, but
// only while the remote branch is still at expect (the commit the user
// saw): somebody else's new commits are never overwritten.
func PushForceWithLease(dir, remote, branch, expect string) error {
	if expect == "" {
		return fmt.Errorf("no known commit of %s/%s to check against: fetch first", remote, branch)
	}
	return gitRemoteRun(dir, "push", "--force-with-lease=refs/heads/"+branch+":"+expect, remote, "refs/heads/"+branch+":refs/heads/"+branch)
}
