# Containers

The **Containers** view shows what runs on this machine, next to your
project's own services. Its first section, **PROJECT**, lists the services
your project declares (see [Project services](services.md)). Below it come
the host sections: **CONTAINERS**, **IMAGES**, **REGISTRIES**,
**NETWORKS**, **VOLUMES**, **CONTEXTS** and **HELP**.

termocode drives the engine through its command line (`docker`, else
`podman`) with JSON output. It does not talk to a socket and installs
nothing. Open the view with the **☷** icon in the activity bar, or
**Containers: Focus** in the palette. Nothing runs before you open it.

## The view

```
CONTAINERS · TERMOCODE
docker 27 · default      5/9 up

▸ Start all  ▪ Stop all        ↻

▾ PROJECT ───────────────────── 4
▾ CONTAINERS ────────────────── 9
▾ termocode · ··············· 3/4
    ◉ postgres-1         2h 14m
▸ IMAGES ────────────────────── 14
▸ REGISTRIES ─────────────────── 2
▸ NETWORKS ───────────────────── 5
▸ VOLUMES ────────────────────── 7
▸ HELP ──────────────────────────
```

The first line names the engine, its major version and the active context.
Each section header has a count. The two lines at the bottom explain the
row under the cursor.

- **Space** (or a click) folds and opens a section or a group. A section
  loads when you open it, and only while the view is on screen.
- **m**, or the **⋯** on the title row, picks which sections show. The
  choice is the same in every workspace (`containers_sections` in
  `config.json`); folded sections are kept per workspace. The menu also has
  **Refresh all** and **Prune system…**. HELP is in the list too.
- **R** refreshes everything.
- CONTAINERS groups containers by compose project (this folder's project
  first, marked `·`, then `other` for the rest) and shows the short service
  name, for example `postgres-1`.
- CONTEXTS shows only when a second Docker context exists.
- No Docker or Podman? CONTAINERS says so and HELP stays; the project's
  local commands still work.

termocode reads the engine when the view opens, on `R` and after your
actions. While the view is on screen one `docker events` process tells it
when something changed. When you hide the view (another activity item,
`Ctrl+B`, zen mode) that process stops: a hidden view runs nothing.

## Keys

| Key | Action |
|---|---|
| `j` `k` `g` `G` | move |
| `Tab` / `Shift+Tab` | jump to the next / previous section header (wraps around) |
| `?` | show the keys of the row under the cursor |
| `Space` | fold / open the section or group under the cursor |
| `Enter` / `l` | on a container: logs tab; on a context: use it; on an image tag: run it (asks); on a HELP row: run it |
| `s` / `x` / `r` | start / stop / restart the selected container; on a project group, start / stop / restart that project |
| `p` | pause or unpause the selected container |
| `e` | shell in the selected container (terminal tab) |
| `t` | attach to a container; on an image, tag it |
| `S` | follow live stats |
| `i` | inspect: a read-only JSON page in the editor |
| `o` | open the first published port in the browser |
| `y` | copy the ID, name, host or endpoint of the row |
| `u` | pull an image |
| `P` / `h` | push an image (asks first) / image history |
| `n` | create a network or volume; connect a registry |
| `d` | read volume disk usage (on VOLUMES) |
| `z` | prune what the section heading offers (asks first) |
| `D` | remove, sign out or make default (asks first) |
| `R` | refresh all |
| `m` | choose the sections (the **⋯** menu) |
| `?` | open this guide for the view |
| `Esc` | focus the editor |

Mouse: click moves the cursor; a click on a header or `▸` / `▾` folds it;
a click on an action row (Connect registry…, the HELP rows) runs it;
double-click on a container opens its logs, on a context uses it; right
click opens the menu of the row; the wheel scrolls.

Right-click a row for the same actions as its keys; items that ask first end with `…`.
Closing a logs tab stops only `logs -f`, never the container.

## Containers

On a CONTAINERS row, `s`, `x`, and `r` start, stop, and restart it; `p`
pauses or resumes it. `l` follows logs, `e` opens a shell, `t` attaches,
`S` follows stats, `i` opens a read-only JSON inspect page, `o` opens its
first published port, `y` copies its full ID, and `D` asks before removal.
Removing a running container shows one card with the exact stop and remove
commands and a **Stop and remove** button; the engine is never forced to
remove it. On a compose project group, `s`, `x`, and `r` run compose up,
stop, and restart. `D` asks before compose down and names its command.
Attach uses `--sig-proxy=false`; press `Ctrl+P Ctrl+Q` in its terminal tab
to detach without stopping the container.

Logs, shell, attach, stats and pulls run in terminal tabs, so you see
progress and can type.

## Images, networks, and volumes

The IMAGES section groups tags by repository and keeps dangling images in a
separate `<none> dangling` group. A repository count includes the number of
images and, when applicable, how many containers use them. On a tag, `u`
pulls, `P` asks before pushing, `t` asks for a new tag, `Enter` asks for a
container name, ports, and environment entries, then shows the full run
command before opening a terminal tab. `h` opens image history, `i` opens
read-only image details, and `D` removes an image only when no container uses
it. `z` on the IMAGES heading asks before pruning dangling images; it never
removes tagged images.

NETWORKS and VOLUMES show their driver and scope. `Space` expands a row and
checks which containers use it. `n` asks for a name and creates a network or
volume. `D` shows the exact remove command and users before removing; a volume
in use is kept, and built-in networks cannot be removed. `z` on either heading
asks before pruning. Volume pruning lists only anonymous dangling volumes;
named volumes are kept. On VOLUMES, `d` reads disk usage once and fills in the
reported sizes. Disk usage is not queried in the background.

## Pruning

Pruning always asks first. Every prune card names what goes (counts from the
last list), the exact command, and has one red `Prune` button. `Esc` or
`Cancel` runs nothing. If a session context is set, the card names it; the
command runs there.

| Where | Command |
|---|---|
| CONTAINERS heading (`z` or right-click): stopped containers | `docker container prune -f` |
| IMAGES heading: dangling images | `docker image prune -f` |
| IMAGES heading: **Prune incl. unused images…** (a separate item) | `docker image prune -a -f` |
| NETWORKS heading: unused networks | `docker network prune -f` |
| VOLUMES heading: the card lists the anonymous dangling volumes first | `docker volume prune -f` |
| `⋯` menu: **Prune system…** | `docker system prune -f` |

System prune never adds the flag for all unused images or for volumes. Those
are only on the IMAGES and VOLUMES headings. The palette has the same entries
(**Containers: Prune Stopped Containers…**, **Prune Dangling Images…**,
**Prune Networks…**, **Prune Volumes…**, **Prune System…**).

When it works, a toast shows what the engine reported: `Pruned · reclaimed
1.2GB`, and the affected sections list again. When it fails, the toast shows
the engine's own error. Every prune has a two minute limit.

The AI cannot prune: there is no tool, and every prune palette command is
closed to it.

## Registries

REGISTRIES lists the registry hosts from your engine's own login file:
`$DOCKER_CONFIG/config.json` (else `~/.docker/config.json`); for Podman
`$REGISTRY_AUTH_FILE` or `$XDG_RUNTIME_DIR/containers/auth.json`. termocode
only reads the host names and the helper names (`credsStore`,
`credHelpers`). It never reads the stored logins and never writes this file.

```
▾ REGISTRIES ────────────── 3
◉ docker.io
◉ ghcr.io
    acme/shop:latest    212MB
◦ quay.io
+ Connect registry…
```

`◉` means signed in. `◦` means the file names a helper for this host, but
there is no login yet. The two lines at the bottom show the host, the user
(when you signed in from termocode in this run; the file does not keep it)
and the helper. A broken file shows a short note; **Connect registry…**
still works.

| Key | Action |
|---|---|
| `Enter` / `Space` on `◉` | show or hide the local images tagged for this host |
| `Enter` on `◦` | connect to this host |
| `n`, or `Enter` on **Connect registry…** | connect to a registry |
| `u` | pull: on a host, the prompt starts with `ghcr.io/`; on an image, its full name |
| `D` | sign out (asks first) |

**Connect registry…** (also **Containers: Connect Registry…** in the
palette) asks four things: a host from a short list (`docker.io`, `ghcr.io`,
`registry.gitlab.com`, or **Other host…**), the host (you can edit it), the
user name, and the token or password. The token prompt shows only `•`. You
can type a reference instead of the value: `${env:NAME}`, `${dotenv:NAME}`
(from this workspace's `.env`) or `${cmd:<a command that prints it>}`. termocode reads it
just before the call.

Then it runs `docker login <host> --username <user> --password-stdin`. The
token goes to the engine on stdin only. It is never in the command line,
the environment, a log, a toast, the session file or any file of
termocode's. The engine's own credential store keeps it. If the login
fails, the toast shows the engine's message with the token removed. A login
has a one minute limit. `Esc` on any step runs nothing and forgets what you
typed.

**Sign out** (`D`, the right-click menu, or **Containers: Sign Out of
Registry…**) shows a card with the exact `docker logout <host>` command.
`Cancel` or `Esc` runs nothing. Local images stay.

Login, logout and pulls use the session context, like every other call
(see below).

termocode does not keep registry tokens itself yet. A "remember in
termocode" option needs the locked, encrypted secret store, which is not
built yet.

The AI cannot sign in or out: there is no tool for it, and both palette
commands are closed to it.

## Contexts and engine choice

CONTEXTS lists your Docker contexts: `◉ default`, `◦ prod`. The endpoint
(`ssh://deploy@prod`) is in the two lines at the bottom. The section shows
only when a second context exists; with just `default` it stays out of the
way. Podman has no CONTEXTS section.

| Key | Action |
|---|---|
| `Enter` / `l` / double-click | use the context **in this session only** |
| `D` | make it the default (asks first) |
| `y` | copy the endpoint |

Using a context sets `DOCKER_CONTEXT` on every engine call of this termocode
session, lists every section again, and restarts the event watcher. The engine
line (`docker 27 · prod`) shows it. Nothing outside termocode changes: not
your shell, not other terminals, not Docker's default. Tabs you open from the
view (logs, shell, pull, compose) get the same `DOCKER_CONTEXT`. Closing
termocode forgets the choice. The PROJECT section keeps using your own
setting.

**Make default…** (right-click menu, `D`, or **Containers: Make Context
Default…**) shows a card with the exact `docker context use <name>` command.
This one does change the default for every terminal and app. Cancel or `Esc`
runs nothing. **Containers: Switch Context…** is the palette version of
`Enter`.

If a remote context fails (SSH key, TLS, timeout), CONTAINERS shows
`context prod: ssh: Connection timed out` and `R retry · Enter on CONTEXTS to
switch back`. CONTEXTS stays available, so `Enter` on `default` gets you
back. Every engine call has a time limit; the view never waits forever.

Set `containers_engine` in `config.json` to `auto` (the default), `docker`,
or `podman`, then press `R`. `auto` tries Docker first, then Podman. An
explicit choice uses only that engine.

The AI cannot switch or change contexts: there is no tool for it, and both
palette commands are closed to it.

## The HELP section

HELP has three rows and one line of information. `Enter`, `l` or a click
runs a row.

| Row | What it does |
|---|---|
| **Open guide** | opens this page in the built-in guide (also **Containers: Open Guide** in the palette) |
| **Keys** | opens this page at [Keys](#keys); `?` in the view does the same kind of thing for what has focus |
| **Report a problem** | opens the project's issue page in your browser |
| `docker 27.3.1`, `context prod` | the engine with its full version, and the active context (lines you cannot run) |

Over SSH there is no browser on the machine that runs termocode, so
**Report a problem** copies the address and shows it in a toast. Open it
on your own computer.

When no engine is found, the line says `No container engine`. While the
engine does not answer, a `not running` line is added.

## Cards and prompts

Every action that removes or changes something outside the view asks
first, with a card:

- The card names what goes and shows the **exact command**. Nothing is
  hidden behind a button.
- The destructive button is red (`Prune`, `Remove`, `Stop and remove`).
  `Esc` or **Cancel** runs nothing.
- If a session context is set, the card names it; the command runs there.
- Prompts (a new tag, a network name, a registry user) take `Esc` at any
  step and forget what you typed. A token or password prompt shows only
  `•`.

## Over SSH

The engine runs on the machine where termocode runs, so ports are that
machine's ports. `o` copies the URL instead of opening a browser, and the
toast shows the forward command for your own computer, for example
`ssh -L 5432:localhost:5432 <host>`. termocode opens no tunnels. **Report a
problem** copies the address, as above.

A Docker context with an `ssh://` endpoint is different: the engine runs on
another machine and termocode reaches it through the engine's own SSH
helper. See [Contexts and engine choice](#contexts-and-engine-choice).

## Podman

Set `containers_engine` to `podman`, or leave `auto` and have only Podman
installed. Podman is used through the same commands. It has no CONTEXTS section, and the session context
is not set for it. Registry logins are read from `$REGISTRY_AUTH_FILE` or
`$XDG_RUNTIME_DIR/containers/auth.json`. Compose projects are grouped by
Podman's own labels.

## Security

- Removing and pruning always ask, with the exact command. The engine is
  never forced.
- Tokens go to the engine on stdin only. They are never in a command line,
  the environment, a log, a toast or a file of termocode's.
- termocode reads the engine's login file for host and helper names only.
  It never reads stored logins and never writes the file.
- Members of the `docker` group are root-equal on that machine. termocode
  never runs `sudo`; it shows the fix for you to run yourself.
- Inspect pages and AI inspect output keep env **names** and mask every
  value.
- The AI has no way to remove, prune, push, tag, run an image, sign in or
  out, or change contexts.

## The AI and host containers

The AI also sees the whole host. It lists every container with its
published ports (`containers_list`), so "why is port 8081 busy?" gets the
answer `port 8081 → other-nginx-1` without starting or removing anything.
It can inspect a container, image, volume or network, read a container's
logs and stats, and list images, volumes, networks, contexts and
registries (hosts and users only). These run at once.

It can start, stop, restart, pause or unpause one container
(`containers_control`), pull an image in a visible `pull:` tab
(`images_pull`), and run `compose up -d` for a project on the host
(`compose_up`). Each one asks first, with **Allow**, **Always allow
containers** and **Deny**. "Always allow containers" lasts until you quit
termocode and is never saved. A container that is busy, or already in
the state asked for, is refused before the card.

Every id, name and image reference is checked first (no `-` at the
start, no spaces or shell characters) and goes to the engine as a plain
argument. Each result says the engine and the context; the AI uses the
context you picked for this session.

The AI cannot remove, prune, push, tag, run an image, sign in or out,
switch or change a context, or read disk usage.

Inspect output keeps env **names** and masks every value, masks auth
fields and labels that look secret, and is cut at
`containers_max_inspect_kb` (64) KB. Container logs are cut at
`services_max_log_kb`.

### Secrets

The AI sees env **names**, not values. Passwords from `.env` and the
compose file are masked as `[REDACTED]` in everything the AI reads (logs,
`read_file`, command output); in a URL only the password is masked, so
`postgres://app:[REDACTED]@localhost:5432/app` still shows the host.
`services_logs` returns at most `services_max_log_kb` (16) KB.

## Errors

| The view says | Do |
|---|---|
| Docker not found | Install Docker, then `R`. |
| Docker Compose v2 is needed | Update Docker (the `docker compose` plugin). |
| Docker is not running | Start Docker, then `R`. The rows stay, with `?`. |
| No access to the Docker socket | The view shows `sudo usermod -aG docker $USER` (`y` copies it). Members of the `docker` group are root-equal on that machine: think first, or use [rootless Docker](https://docs.docker.com/engine/security/rootless/). termocode never runs `sudo`. |
| compose.yaml: … | The first error line; `o` opens the file. |

## Termux (Android)

No container engine: PROJECT shows local commands and HELP stays. Nothing asks you to install an engine.
