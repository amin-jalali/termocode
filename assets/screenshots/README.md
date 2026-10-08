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
| `dashboard.png` | start page: gradient wordmark, action tiles |
| `editor.png` | explorer, tabs, breadcrumbs, editor, status bar |
| `palette.png` | command palette over the dimmed editor, with its shadow |
| `toast.png` | a toast card |
| `scm.png` | Source Control: commit box, change tree, graph |
| `ai-chat-answer.png` | AI chat with a Markdown answer and a code card |
| `image-preview.png` | image preview (half-blocks; kitty / sixel show the real image) |
| `terminal.png` | integrated terminal running `ls --color` |
| `confirm.png` | confirm dialog |
| `problems.png` | Problems panel |
| `theme-*.png` | each new theme (`-dashboard` = start page in Catppuccin Latte) |
| `icons-unicode.png` | the Unicode icon set (no Nerd Font) |

## Animated: `demo.tape` (vhs)

[`demo.tape`](demo.tape) is a [vhs](https://github.com/charmbracelet/vhs) script
for an animated `demo.gif` (not committed yet):

```sh
go build -o termocode ./cmd/termocode
vhs assets/screenshots/demo.tape
```

## Rules

- PNG for stills, GIF for `demo.gif`. Keep each file under ~2 MB.
- No personal paths, user names or tokens in the frame — the shots tool uses
  a clean demo copy named `termocode`.
- When you add an image, link it from the top-level `README.md`.
