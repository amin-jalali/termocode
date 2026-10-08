# Screenshots

Images used by the top-level `README.md`. **No images are committed yet** —
this file lists the shots we need and how to make them.

## Automatic: `demo.tape` (vhs)

[`demo.tape`](demo.tape) is a [vhs](https://github.com/charmbracelet/vhs) script.
It opens termocode on this repo and records:

| File | Shows |
|---|---|
| `demo.gif` | the whole run below, as an animated GIF (README hero) |
| `editor.png` | `internal/app/model.go` open after Quick Open (`Ctrl+P`) — tabs, explorer, gutter, status bar |
| `search.png` | Find in Files (`Alt+F`) for `ExecLua` with results in the sidebar |
| `terminal.png` | the integrated terminal (`Ctrl+T`) under the editor |

```sh
go build -o termocode ./cmd/termocode
vhs assets/screenshots/demo.tape
```

You need `nvim`, `git`, `rg` and the JetBrainsMono Nerd Font on the machine
(`termocode setup` installs the font on Linux).

## By hand

vhs cannot press function keys, so take these with your terminal's screenshot
tool (window about 160×48 cells, Dark+ theme, Nerd Font):

| File | How |
|---|---|
| `palette.png` | `F1`, type `theme` — command palette over the editor (shows the frosted overlay) |
| `git.png` | Source Control view with a few changed files, the commit box and the commit graph |
| `diff.png` | Click a changed file in Source Control — side-by-side diff |
| `debug.png` | A Go file with a breakpoint (`F9`) hit under delve |
| `doctor.png` | Palette → `Help: Run Doctor` |

## Rules

- PNG for stills, GIF for `demo.gif`. Keep each file under ~2 MB.
- No personal paths, user names or tokens in the frame — use this repo or a
  clean demo folder.
- When you add an image, link it from the top-level `README.md`.
