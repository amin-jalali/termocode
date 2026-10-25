# Presentation GIFs

Short GIFs of the real program, for the README, the docs site and the
website. Each `name.gif` has a sidecar `name.txt` with the caption of every
scene and its time range (the captions are **not** burned into the frames;
put them on the page or in the video editor).

| GIF | Length | Shows | Caption |
|---|---|---|---|
| `hero.gif` | 25 s | open a project, edit a file, run a query, give the AI a task, **drop the connection**, reconnect: file, result and answer are still there | One terminal. Your whole project. / Edit, query, ship — on the server. / AI works with your IDE. / Connection lost. / Your work lives on. |
| `db.gif` | 9 s | type a query, the result grid, edit rows, the Submit card with the full SQL | Your database, in your terminal. |
| `containers.gif` | 8 s | container list, follow the logs, restart one container | Containers without leaving the editor. |
| `ai-confirm.gif` | 9 s | the AI proposes a change, the review card, Accept | AI asks before it acts. |
| `agents.gif` | 12 s | the planner builds the plan, steps run in parallel on their own models, cost on the cards, `u` opens AI: Usage | Agents plan the work, run in parallel, and show what each one cost. |
| `mcp.gif` | 9 s | an outside agent asks to run a tool, the confirm card names it, Allow, the status bar chip, the server status | Your agents get the IDE's tools. |
| `secrets.gif` | 9 s | the Secret Store (names only), lock, unlock with the masked passphrase | Secrets stay secret. |
| `import.gif` | 10 s | pick an editor, review old and new values, apply, the clickable toast and its Undo card | Bring your setup with you. |

All of them are 200 x 50 cells, the dark theme and the JetBrainsMono Nerd
Font, under 5 MB, and loop.

## What is real, what is canned

* Every frame is drawn by the real program. Nothing is a mock-up and no
  frame is edited.
* `hero.gif` runs the **real binary** in a pseudo-terminal with a real
  persistent session. The disconnect is real: the client gets SIGHUP, as
  from sshd when the connection dies, and exits. The dead terminal keeps its
  last screen for a moment, then it is reset (what closing a dead window
  does), which is the dark screen. The session keeps running with no client;
  the demo model finishes the answer meanwhile; a second client attaches
  and shows everything. The database is a real SQLite file read through the
  real `sqlite3`.
* `ai-confirm.gif`, `agents.gif` and the AI part of `hero.gif` use **demo
  models**: a scripted model on a loopback port, or the scripted providers of
  the `agentsdemo` hook. No network, no API keys. The sidecar of each of these
  says "demo models".
* `containers.gif` runs the real Containers view against a **fake `docker`**
  script on `PATH` (sample containers, no engine).
* `db.gif`, `mcp.gif`, `secrets.gif` and `import.gif` use the dev demo hooks
  (`dbdemo:`, `mcpservedemo:`, `secretsdemo:`, `importdemo:`): canned state in
  a sandbox config dir, the same hooks as the screenshots and golden tests. The
  keys you see pressed are real; the database result and the MCP agent are made
  up. No real server, secret or home path is involved.

## Re-recording

You need `nvim`, `git`, `sqlite3` (3.33 or newer) and a Nerd Font `.ttf`.
From the repository root:

```sh
go run ./cmd/recordgifs                    # all GIFs, into assets/gifs
go run ./cmd/recordgifs -only db,secrets   # some of them
go run ./cmd/recordgifs -only hero         # the hero (about 30 s)
go run ./cmd/recordgifs -frames /tmp/f     # also write every frame as PNG to look at
go run ./cmd/recordgifs -font ~/fonts/JetBrainsMonoNerdFontMono-Regular.ttf
```

The font is `-font`, then `$TERMOCODE_SHOTS_FONT`, then
`~/.local/share/fonts/JetBrainsMonoNF/JetBrainsMonoNerdFontMono-Regular.ttf`.
A GIF over 5 MB is an error.

How it works:

* `cmd/recordgifs` is the command. It encodes with
  `internal/devtools/ansipng` and writes the `.txt` sidecars.
* `internal/devtools/gifkit` has the scenes (`scenes.go`), the demo model
  (`fakemodel.go`) and the fake `docker` (`record.go`). A scene is a
  `shotkit` script: `Step.Hold` is how long a frame shows, a negative hold
  is a setup step that is not recorded, `Step.Live` records without waiting
  for the screen to settle (a streaming answer).
* The hero is `e2e/record_hero_test.go` (build tags `e2e,record`). The
  command runs it with `go -C e2e test -tags e2e,record -run TestRecordHero`
  and `GIF_FRAMES_DIR` set. It samples the VT emulator while the scenes
  run, plays a scene that took longer than its time in the GIF a little
  faster and holds the last screen of a shorter one.

To change a scene, edit `scenes.go`, run `-only <name> -frames <dir>` and
look at the PNGs. Everything runs in a private config dir and a temp copy
of this repository; it never touches your own config, terminals, Docker or
network.
