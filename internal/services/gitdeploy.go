package services

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// GitDeploy is "deploy a compose stack from git": a shallow clone (or a
// fast-forward pull of an earlier clone) under Base, then `compose up -d`
// in it. Nothing runs here; Steps gives the exact commands for the card.
type GitDeploy struct {
	URL    string // the repository (https://, ssh:// or user@host:path)
	Ref    string // branch or tag; "" = the default branch
	Name   string // the folder name, from the URL
	Dir    string // Base/Name
	Exists bool   // Dir is already there: pull instead of clone
}

var (
	gitScpURL  = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[A-Za-z0-9._~/-]+$`)
	gitRefOK   = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)
	gitNameBad = regexp.MustCompile(`[^A-Za-z0-9._-]`)
)

// PlanGitDeploy reads "url" or "url#ref" and plans the deploy under base.
// A flag, a space, a control character, a local file URL or an odd ref is
// refused, so nothing the user typed can become an option.
func PlanGitDeploy(spec, base string) (GitDeploy, error) {
	// A control character anywhere (a pasted newline too) is refused, not
	// trimmed away: only plain spaces around the URL are forgiven.
	if strings.IndexFunc(spec, func(r rune) bool { return r < ' ' || r == 0x7f }) >= 0 {
		return GitDeploy{}, errors.New("not a repository URL")
	}
	spec = strings.Trim(spec, " ")
	url, ref, _ := strings.Cut(spec, "#")
	if url == "" {
		return GitDeploy{}, errors.New("type a git repository URL")
	}
	if strings.HasPrefix(url, "-") || strings.IndexFunc(spec, func(r rune) bool { return r <= ' ' || r == 0x7f }) >= 0 {
		return GitDeploy{}, errors.New("not a repository URL")
	}
	var repoPath string
	switch {
	case strings.HasPrefix(url, "https://"), strings.HasPrefix(url, "ssh://"):
		_, rest, _ := strings.Cut(url, "://")
		_, repoPath, _ = strings.Cut(rest, "/")
	case gitScpURL.MatchString(url):
		_, repoPath, _ = strings.Cut(url, ":")
	default:
		return GitDeploy{}, errors.New("use an https://, ssh:// or user@host:path URL")
	}
	if ref != "" && (strings.HasPrefix(ref, "-") || strings.Contains(ref, "..") || !gitRefOK.MatchString(ref)) {
		return GitDeploy{}, errors.New("not a branch or tag name")
	}
	repoPath = strings.Trim(repoPath, "/")
	name := gitNameBad.ReplaceAllString(strings.TrimSuffix(path.Base(repoPath), ".git"), "-")
	if repoPath == "" || name == "" || name == "." || name == ".." {
		return GitDeploy{}, errors.New("the URL names no repository")
	}
	d := GitDeploy{URL: url, Ref: ref, Name: name, Dir: filepath.Join(base, name)}
	if fi, err := os.Stat(d.Dir); err == nil && fi.IsDir() {
		d.Exists = true
	}
	return d, nil
}

// Steps are the commands in order: clone or pull, then compose up.
func (d GitDeploy) Steps(docker string) [][]string {
	if docker == "" {
		docker = "docker"
	}
	var get []string
	if d.Exists {
		get = []string{"git", "-C", d.Dir, "pull", "--ff-only"}
	} else {
		get = []string{"git", "clone", "--depth", "1"}
		if d.Ref != "" {
			get = append(get, "--branch", d.Ref)
		}
		get = append(get, "--", d.URL, d.Dir)
	}
	return [][]string{get, {docker, "compose", "--project-directory", d.Dir, "up", "-d"}}
}

// GitDeployBase is where deployed stacks are cloned:
// $XDG_DATA_HOME/termocode/stacks, else ~/.local/share/termocode/stacks.
func GitDeployBase() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "termocode", "stacks")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "share", "termocode", "stacks")
}
