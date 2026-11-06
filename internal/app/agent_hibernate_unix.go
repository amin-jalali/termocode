//go:build unix

package app

import (
	"errors"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// agentKillForeground stops the foreground job of the terminal whose
// shell is shellPid (AGX-15): SIGTERM to its process group, then SIGKILL
// after grace when it is still there. The shell itself is never sent a
// signal.
func agentKillForeground(shellPid int, grace time.Duration) error {
	pgid, err := agentForegroundPgid(shellPid)
	if err != nil {
		return err
	}
	if own, err := syscall.Getpgid(shellPid); err == nil && own == pgid {
		return errors.New("no program runs in the tab")
	}
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil {
		return err
	}
	crash.Go("agent-hibernate-kill", func() {
		time.Sleep(grace)
		if syscall.Kill(-pgid, 0) == nil {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		}
	})
	return nil
}

// agentForegroundPgid is the foreground process group of pid's terminal
// (field tpgid of /proc/<pid>/stat on Linux, ps elsewhere).
func agentForegroundPgid(pid int) (int, error) {
	var field string
	if runtime.GOOS == "linux" {
		b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if err != nil {
			return 0, err
		}
		s := string(b)
		i := strings.LastIndexByte(s, ')') // the command name may hold spaces
		if i < 0 {
			return 0, errors.New("cannot read the process")
		}
		f := strings.Fields(s[i+1:])
		if len(f) < 6 {
			return 0, errors.New("cannot read the process")
		}
		field = f[5] // state ppid pgrp session tty_nr tpgid
	} else {
		out, err := spawn.Command("ps", "-o", "tpgid=", "-p", strconv.Itoa(pid)).Output()
		if err != nil {
			return 0, err
		}
		field = strings.TrimSpace(string(out))
	}
	pg, err := strconv.Atoi(field)
	if err != nil || pg <= 1 {
		return 0, errors.New("no program runs in the tab")
	}
	return pg, nil
}
