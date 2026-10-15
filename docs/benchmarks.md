# Benchmarks

How much CPU and memory termocode uses, and how it handles very large
files. Every number on this page was measured on the machine below with
the method below. If you measure something different on your machine,
please open an issue with your numbers.

Measured on 2026-10-15, commit `79c2c40`.

## The machine

| | |
|---|---|
| CPU | Intel Core i7-13700H (20 threads) |
| RAM | 32 GB |
| OS | Ubuntu 24.04.4, Linux 7.0 |
| Neovim | 0.12.2 |
| gopls | 0.22.0 |
| Go | 1.26.2 |

## How it was measured

termocode runs as a real binary in a pseudo-terminal (the e2e test harness
in `e2e/`, with a VT emulator as the "terminal"). The test sends key and
mouse input as bytes, like a real terminal does. CPU is read from
`/proc/<pid>/stat` (user + system time) for termocode and every child
process, over the length of each action. Memory is the resident size
(`VmRSS`). Each test ran 3 times; the tables show the **median**.

Things to know:

- The emulator draws nothing on a real screen, so the time a GPU terminal
  needs to paint is not included.
- termocode runs with a fresh, empty configuration and the default theme.
- The CPU percent is of one CPU core.

## CPU and memory while editing

A 900-line Go file in a Go module, window 240 × 62 cells. The Go language
server (gopls) runs, as it does for any Go project.

| Action | termocode | Neovim | gopls |
|---|---|---|---|
| Idle (nothing happens) | 0–1 % | 0 % | 0 % |
| Holding ↓ / ↑ (one key every 30 ms) | 18 % | 11 % | 8 % |
| Scrolling with the mouse wheel | 28 % | 24 % | 0 % |
| Moving the mouse over the window | 12 % | 0 % | 0 % |
| Typing code | 11 % | 22 % | 66–70 % |

| Memory | |
|---|---|
| termocode | 38 MB |
| Neovim | 20 MB |
| gopls | 538 MB, plus 124 MB for its telemetry helper |

gopls numbers are from 2 of the 3 runs: in one run it had not started
yet when the measurement began.

Most of the memory and of the CPU while typing is the language server, not
the editor. It is the same gopls that VS Code, Neovim setups and other
editors run, so it costs about the same everywhere. `gopls telemetry off`
removes the helper process.

From process start to the file explorer on screen: 46–69 ms.

## Large files

Window 200 × 50 cells. Two files in the folder: a 100 MB log
(1,093,802 lines) and a 10 MB minified JSON file (one single line).

| Action | Time |
|---|---|
| Open the 100 MB log | 1.10 s |
| Jump to the last line (`Ctrl+End`) | 126 ms |
| Back to the first line (`Ctrl+Home`) | 107 ms |
| 50 × ↓ | 105 ms |
| Go to line 999,999 | 218 ms |
| Open the 10 MB one-line JSON | 1.22 s |
| `End` (to the end of the 10 MB line) | 635 ms |

| Memory with the 100 MB log open | |
|---|---|
| termocode | 34 MB |
| Neovim | 127 MB |

termocode itself stays at about 34 MB with big files: the text lives in
Neovim, and termocode only draws the rows on screen.

## What is not measured yet

- Other editors on the same machine with the same method. Comparisons
  will be added here only when they are measured this way.
- macOS and Windows (WSL).
- Startup with many plugins in your own Neovim config.
