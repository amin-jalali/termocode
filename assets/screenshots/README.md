# Screenshots

Images used by the top-level `README.md` and the docs site.

## Stills: the `shots` dev tool

The PNGs here are rendered by [`internal/devtools/shots`](../../internal/devtools/shots).
It runs the real app (with an embedded `nvim`) in process on a demo copy of
this repo, drives it scene by scene and rasterises each frame cell by cell
with a Nerd Font — no terminal, window manager or screenshot tool needed:

```sh
# a Nerd Font .ttf (JetBrainsMono Nerd Font Mono works well)
go run ./internal/devtools/shots -font ~/fonts/JetBrainsMonoNerdFontMono-Regular.ttf \
    -out assets/screenshots -w 160 -h 44
go run ./internal/devtools/shots -font … -only dashboard,palette   # some scenes
```

| File | Shows |
|---|---|
| `dashboard.png` | start page: gradient wordmark, START list, recent files, workspaces |
| `editor.png` | explorer, tabs, breadcrumbs, editor, status bar |
| `palette.png` | command palette over the dimmed editor, with its shadow |
| `toast.png` | a toast card |
| `scm.png` | Source Control: commit box, change tree, graph |
| `ai-chat-answer.png` | AI chat with a Markdown answer and a code card (README hero) |
| `diff-review.png` | "Review AI changes" card: split diff, accept / reject per change |
| `md-preview.png` | Markdown preview (reader view) |
| `image-preview.png` | image preview (half-blocks; kitty / sixel show the real image) |
| `terminal.png` | integrated terminal running `ls --color` |
| `confirm.png` | confirm dialog |
| `problems.png` | Problems panel |
| `theme-*.png` | each new theme (`-dashboard` = start page in Catppuccin Latte) |
| `icons-unicode.png` | the Unicode icon set (no Nerd Font) |

## Animated: `demo.gif`

`demo.gif` is the README demo. The shots tool records it from
`shotkit.DemoScene` (no vhs, ttyd or ffmpeg needed):

```sh
go run ./internal/devtools/shots -font ~/fonts/JetBrainsMonoNerdFont-Regular.ttf \
  -w 120 -h 34 -out assets/screenshots -gif demo.gif
```

Each step's `Hold` sets how long its frame shows. Frames after the first
store only the part of the screen that changed.
[`demo.tape`](demo.tape) is an older vhs script for a recording of the real
binary.

## Rules

- PNG for stills, GIF for `demo.gif`. Render stills at `-w 160 -h 44` so they
  match; each PNG should stay under ~300 KB (the tool writes with best
  compression), `demo.gif` under ~2 MB.
- The README gallery uses `editor`, `ai-chat-answer`, `diff-review`,
  `dashboard`, `palette`, `scm`, `terminal`, `md-preview` and
  `theme-catppuccin-latte`. Regenerate them together:
  `-only dashboard,editor,ai-chat-answer,diff-review,md-preview,scm,terminal,palette,theme-catppuccin-latte`.
- No personal paths, user names or tokens in the frame — the shots tool uses
  a clean demo copy named `termocode`.
- When you add an image, link it from the top-level `README.md`.
