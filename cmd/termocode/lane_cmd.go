package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/agentlog"
	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/jsonc"
	"github.com/amin-jalali/termocode/internal/lanemove"
)

// `termocode lane pack|info|unpack|pull|push` (CMP-6): move a lane — a
// worktree with its commits, its changes not committed, its new files,
// its notes and (with --chats) its Claude Code chats — from one machine
// to another. Unpack always makes a NEW worktree; nothing is written over
// and the old lane stays where it was until you archive it there.

const laneUsage = `termocode lane: move a lane (a worktree and its work) to another machine

  termocode lane pack [--out FILE|-] [--chats] [DIR]
        pack the worktree DIR is in (default: here) into one .tlane file
        (default: ~/.local/share/termocode/lanes/<name>-<time>.tlane)
  termocode lane info FILE
        show what a pack holds
  termocode lane unpack [--repo DIR] [--name BRANCH] FILE|-
        make a new worktree of the repository DIR is in from a pack
  termocode lane pull [--name BRANCH] [--chats] HOST:DIR
        pack DIR on HOST over ssh and unpack it here (termocode on both ends)
  termocode lane push [--chats] [DIR] HOST:REPO
        pack DIR here and unpack it on HOST in the repository REPO

A pack holds the commits the other side may lack (git bundle), the
changes not committed (a patch), the new files git does not track yet
(ignored files stay; big ones are left out), and the worktree's notes.
--chats adds the Claude Code chats of the folder, so "claude --resume"
goes on there. The repository must be on both machines; git fetch first
when the other side lacks the base commits.
`

func runLane(args []string) int {
	return runLaneIn(args, os.Stdin, os.Stdout, os.Stderr, os.Getenv)
}

func runLaneIn(args []string, in io.Reader, out, errw io.Writer, getenv func(string) string) int {
	fail := func(f string, a ...any) int {
		fmt.Fprintf(errw, "termocode lane: "+f+"\n", a...)
		return 1
	}
	usage := func() int { fmt.Fprint(errw, laneUsage); return 2 }
	if len(args) == 0 {
		return usage()
	}
	if a := args[0]; a == "-h" || a == "--help" || a == "help" {
		fmt.Fprint(out, laneUsage)
		return 0
	}
	home, _ := os.UserHomeDir()
	claudeHome := agentlog.HomesFrom(home, getenv).Claude
	fs := flag.NewFlagSet("lane "+args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	outFile := fs.String("out", "", "")
	chats := fs.Bool("chats", false, "")
	repo := fs.String("repo", ".", "")
	name := fs.String("name", "", "")
	if err := fs.Parse(args[1:]); err != nil {
		return usage()
	}
	rest := fs.Args()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	switch args[0] {
	case "pack":
		if len(rest) > 1 {
			return usage()
		}
		dir := "."
		if len(rest) == 1 {
			dir = rest[0]
		}
		opt := lanemove.PackOptions{Meta: lanemove.MetaOf(dir)}
		if *chats {
			opt.ClaudeHome = claudeHome
		}
		if *outFile == "-" {
			if _, err := lanemove.Pack(dir, opt, out); err != nil {
				return fail("%v", err)
			}
			return 0
		}
		file, man, err := lanemove.PackFile(dir, *outFile, lanemove.Dir(getenv, home), opt)
		if err != nil {
			return fail("%v", err)
		}
		fmt.Fprintf(out, "packed %s into %s\n%s\n", man.Title(), file, laneInfoText(man))
		return 0

	case "info":
		if len(rest) != 1 {
			return usage()
		}
		man, err := lanemove.InspectFile(rest[0])
		if err != nil {
			return fail("%v", err)
		}
		fmt.Fprintln(out, laneInfoText(man))
		return 0

	case "unpack":
		if len(rest) != 1 {
			return usage()
		}
		file := rest[0]
		if file == "-" {
			tmp, err := laneSpool(in)
			if err != nil {
				return fail("%v", err)
			}
			defer os.Remove(tmp)
			file = tmp
		}
		return laneUnpack(file, *repo, *name, claudeHome, home, out, fail)

	case "pull":
		if len(rest) != 1 {
			return usage()
		}
		host, dir, err := lanemove.ParseRemote(rest[0])
		if err != nil {
			return fail("%v", err)
		}
		f, err := os.CreateTemp("", "termocode-lane-*"+lanemove.Ext)
		if err != nil {
			return fail("%v", err)
		}
		tmp := f.Name()
		defer os.Remove(tmp)
		err = lanemove.Fetch(ctx, host, dir, *chats, nil, false, f)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return fail("the lane could not be packed on %s: %v", host, err)
		}
		return laneUnpack(tmp, *repo, *name, claudeHome, home, out, fail)

	case "push":
		if len(rest) < 1 || len(rest) > 2 {
			return usage()
		}
		dir := "."
		if len(rest) == 2 {
			dir = rest[0]
		}
		host, remoteRepo, err := lanemove.ParseRemote(rest[len(rest)-1])
		if err != nil {
			return fail("%v", err)
		}
		opt := lanemove.PackOptions{Meta: lanemove.MetaOf(dir)}
		if *chats {
			opt.ClaudeHome = claudeHome
		}
		pr, pw := io.Pipe()
		crash.Go("main.runLaneIn", func() {
			_, err := lanemove.Pack(dir, opt, pw)
			pw.CloseWithError(err)
		})
		said, err := lanemove.Send(ctx, host, remoteRepo, nil, false, pr)
		_ = pr.Close()
		if err != nil {
			return fail("the lane could not be unpacked on %s: %v", host, err)
		}
		if said != "" {
			fmt.Fprintln(out, said)
		}
		fmt.Fprintf(out, "the lane is on %s now; this one stays here until you archive it (x in WORKTREES)\n", host)
		return 0
	}
	return usage()
}

// laneSpool copies a pack from r into a temporary file.
func laneSpool(r io.Reader) (string, error) {
	f, err := os.CreateTemp("", "termocode-lane-*"+lanemove.Ext)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, io.LimitReader(r, lanemove.MaxPackTotal+(64<<20)))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func laneUnpack(file, repo, name, claudeHome, home string, out io.Writer, fail func(string, ...any) int) int {
	res, err := lanemove.UnpackFile(file, lanemove.Into{Dir: repo, Name: name, Setting: laneWorktreeDir(home), Home: home, ClaudeHome: claudeHome})
	if err != nil && res.Folder == "" {
		return fail("%v", err)
	}
	fmt.Fprintf(out, "unpacked %s into %s\n%s\n", res.Manifest.Title(), res.Folder, res.Summary())
	if err != nil {
		return fail("%v", err)
	}
	if res.PatchAt != "" {
		return 1
	}
	return 0
}

// laneWorktreeDir is config.json "worktree_dir" ("" = next to the repo).
func laneWorktreeDir(home string) string {
	path := filepath.Join(home, ".config", "termocode", "config.json")
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		path = filepath.Join(v, "termocode", "config.json")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var cfg struct {
		Dir string `json:"worktree_dir"`
	}
	if json.Unmarshal(jsonc.Strip(b), &cfg) != nil {
		return ""
	}
	return cfg.Dir
}

// laneInfoText is what a pack holds, a few lines.
func laneInfoText(man lanemove.Manifest) string {
	var b strings.Builder
	from := man.Path
	if man.Host != "" {
		from = man.Host + ":" + from
	}
	branch := man.Branch
	if branch == "" {
		branch = "detached"
	}
	fmt.Fprintf(&b, "  lane:     %s (%s at %.7s)\n", man.Title(), branch, man.Head)
	fmt.Fprintf(&b, "  from:     %s, %s\n", from, man.PackedAt.Local().Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "  commits:  %d not on the remotes\n", man.Commits)
	fmt.Fprintf(&b, "  changes:  %d changed files, %d new files", man.Changed, len(man.Files))
	if len(man.Skipped) > 0 {
		fmt.Fprintf(&b, " (%d left out: too big or not plain files)", len(man.Skipped))
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "  chats:    %d Claude Code chats\n", len(man.Chats))
	fmt.Fprintf(&b, "  notes:    %d", len(man.Notes))
	return b.String()
}
