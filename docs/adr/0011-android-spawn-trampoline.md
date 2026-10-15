# ADR 0011 — Start child processes through Android's shell

Status: **Accepted** (2026-10-15)

## Context
The Google Play build of Termux targets a new Android SDK. There, an app
may not `execve` a file in its own data dir (W^X). Termux's own programs
still run: `libtermux-exec.so` (in `LD_PRELOAD`) hooks libc's `execve`
and starts them through the system linker.

Go does not call libc's `execve`; it makes the system call itself. So on
a tablet with Play Store Termux (Android 16, Termux googleplay.2026.02.11)
a Go program got `fork/exec …/usr/bin/bash: permission denied` for every
Termux program: termocode could not start nvim, git or a shell.

Two more findings from the same test:
- `os.Executable()` returns `/apex/…/linker64`, because termux-exec ran
  termocode through the linker. termux-exec puts the real path in
  `TERMUX_EXEC__PROC_SELF_EXE`.
- `/system/bin/linker64 <file>` runs ELF files only. Some Termux
  commands are scripts (`usr/bin/nvim` is a wrapper), so calling the
  linker ourselves is not enough.

## Decision
All child processes start through `internal/spawn` (`Command`,
`CommandContext`, `Exec`, `Executable`). On Android a program outside the
system dirs runs as

    /system/bin/sh -c 'exec "$0" "$@"' <path> <args...>

Android's own shell may exec app files, and it loads the termux-exec hook,
so ELF files and scripts both work. `sh` execs in place, so the pid,
process group, Pdeathsig and pipes stay the program's. The arguments are
never parsed by the shell (`"$@"`).

`spawn.Executable` uses `TERMUX_EXEC__PROC_SELF_EXE` when
`/proc/self/exe` is the linker.

On other systems the package changes nothing. A test
(`TestNoDirectExec`) fails when code outside the package calls
`exec.Command`, `exec.CommandContext`, `syscall.Exec` or `os.Executable`.

## Consequences
- termocode runs in both the F-Droid and the Play Store Termux (checked on
  a real tablet: editor, git, terminal, session restart, quit).
- One extra `sh` start per child process on Android (a few ms).
- On Android, `cmd.Path` / `cmd.Args` show the shell call. Code that
  edits `cmd.Args` after creating the command must create it again or
  call `spawn.Fix`.
- Processes that nvim, git or the shell start are not ours: they use
  libc, so termux-exec handles them.
