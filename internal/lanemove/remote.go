package lanemove

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// ── Over ssh: pull a lane from a host, push one to it ───────────────────
//
// Both ends run termocode: pull runs `termocode lane pack --out -` on the
// host and reads the pack from ssh; push runs `termocode lane unpack -`
// there and writes the pack into ssh. ssh is the user's own (keys,
// config, agent); nothing else is opened.

// ErrBadRemote: not host:folder.
var ErrBadRemote = errors.New("give it as host:folder (folder may start with ~/)")

// ParseRemote splits "host:folder". The host may be user@host or an ssh
// config name; it never starts with "-".
func ParseRemote(s string) (host, dir string, err error) {
	host, dir, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok || host == "" || strings.HasPrefix(host, "-") || strings.ContainsAny(host, " \t\n'\"`$;|&") {
		return "", "", ErrBadRemote
	}
	if dir == "" {
		dir = "."
	}
	if strings.ContainsAny(dir, "\n\x00") {
		return "", "", ErrBadRemote
	}
	return host, dir, nil
}

// quoteDir quotes a folder for the remote shell; a leading ~/ stays
// outside the quotes so the shell makes it home.
func quoteDir(dir string) string {
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	if dir == "~" {
		return dir
	}
	if rest, ok := strings.CutPrefix(dir, "~/"); ok {
		return "~/" + q(rest)
	}
	return q(dir)
}

// PackLine is the remote command of a pull; chats = the Claude Code
// chats of the folder go along.
func PackLine(dir string, chats bool) string {
	line := "termocode lane pack --out -"
	if chats {
		line += " --chats"
	}
	return line + " " + quoteDir(dir)
}

// UnpackLine is the remote command of a push.
func UnpackLine(repo string) string { return "termocode lane unpack --repo " + quoteDir(repo) + " -" }

// SSHArgs are the ssh arguments of a run on host; batch = no password
// questions (the UI has no terminal for them).
func SSHArgs(host string, extra []string, batch bool, line string) []string {
	args := []string{}
	if batch {
		args = append(args, "-o", "BatchMode=yes")
	}
	args = append(args, extra...)
	return append(args, "--", host, line)
}

// sshRun is the ssh call (tests swap it).
var sshRun = func(ctx context.Context, args []string, in io.Reader, out io.Writer) error {
	cmd := spawn.CommandContext(ctx, "ssh", args...)
	cmd.Stdin, cmd.Stdout = in, out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := lastLine(stderr.String()); msg != "" {
			return fmt.Errorf("ssh: %s", msg)
		}
		return fmt.Errorf("ssh: %w", err)
	}
	return nil
}

// Fetch packs dir on host and writes the pack to out.
func Fetch(ctx context.Context, host, dir string, chats bool, extra []string, batch bool, out io.Writer) error {
	return sshRun(ctx, SSHArgs(host, extra, batch, PackLine(dir, chats)), nil, out)
}

// Send unpacks the pack in r on host in the repository repo and
// returns what termocode there said.
func Send(ctx context.Context, host, repo string, extra []string, batch bool, r io.Reader) (string, error) {
	var out bytes.Buffer
	err := sshRun(ctx, SSHArgs(host, extra, batch, UnpackLine(repo)), r, &out)
	return strings.TrimSpace(out.String()), err
}

// lastLine is the last line of s that is not empty.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
