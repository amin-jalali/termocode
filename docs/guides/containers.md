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
- CONTEXTS shows only when there is a second Docker context (or, for
  Podman, a connection to choose).
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
| `/` | filter the rows of every section by name (see [Filter](#filter)) |
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
| `b` | on an image: show its remote tags; in REGISTRIES: browse the tags of a repository |
| `s` | in REGISTRIES: search Docker Hub |
| `P` / `h` | push an image (asks first) / image history |
| `n` | create a network or volume; connect a registry |
| `d` | read volume disk usage (on VOLUMES) |
| `z` | prune what the section heading offers (asks first) |
| `D` | remove, sign out or make default (asks first) |
| `R` | refresh all |
| `m` | choose the sections (the **⋯** menu) |
| `?` | open this guide for the view |
| `Esc` | clear the filter; without a filter, focus the editor |

Mouse: click moves the cursor; a click on a header or `▸` / `▾` folds it;
a click on an action row (Connect registry…, the HELP rows) runs it;
double-click on a container opens its logs, on a context uses it; right
click opens the menu of the row; the wheel scrolls.

Right-click a row for the same actions as its keys; items that ask first end with `…`.
Closing a logs tab stops only `logs -f`, never the container.

## Filter

Press `/` and type. Every section keeps only the rows whose name contains
the text (upper and lower case are the same): PROJECT services,
containers, image repositories and tags, registries, networks, volumes,
contexts and remote tags.

```
CONTAINERS · /pos▏
▾ PROJECT ─────────────────── 1/4
  ◉ postgres            :5432   2h
▾ CONTAINERS ──────────────── 1/8
▾ termocode · ················ 2/3
    ◉ postgres-1        :5432   2h
▾ IMAGES ──────────────────── 2/4
▾ postgres ······················ 2
      16                   421MB
      15-alpine            239MB
▸ NETWORKS ────────────────── 0/3
```

- The text shows in the title row. Each header shows `matches/all`.
- A match keeps its group (compose project, image repository, registry
  host); a group that matches keeps all its rows. Folded groups are
  searched too. A folded section stays folded, but its header still counts
  the matches.
- When nothing matches, one line says so: `No rows match "…" · Esc clears`.
- `Enter` keeps the filter and gives the keys back (`j`, `k`, `s`, …).
  `Esc` clears it. `Backspace` deletes a letter; `Ctrl+U` clears the text.
- `Tab`, the arrows and the mouse work on the filtered rows, also while you
  type.
- The filter only looks at what the view already has. Typing never runs an
  engine command. Closing termocode forgets the filter.
- **Containers: Filter Rows…** in the palette starts it too.

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

### Remote tags

`b` on an image repository (or one of its tags) in IMAGES lists the tags
the registry has, right under the repository:

```
▾ postgres ······················ 2
      16                   421MB
      15-alpine            239MB
    ▾ remote tags                84
        17
        17-alpine
        …
```

`b` in REGISTRIES (or **Containers: Browse Remote Tags…**) asks for a
repository, like `nginx`, `bitnami/redis` or `ghcr.io/acme/api`, and lists
its tags in REGISTRIES. On a host row the prompt starts with that host.

| Key | Action |
|---|---|
| `Space` / `Enter` on **remote tags** | fold or open the list |
| `r` | ask the registry again |
| `Enter` / `u` on a tag, or double-click | pull it: the pull prompt starts with the full name (`nginx:1.27`) |
| `y` | copy the name |
| `x` | remove a browsed repository from REGISTRIES |

How termocode asks the registry:

- It uses the Registry HTTP API v2 (`/v2/<name>/tags/list`) and the
  registry's own token step, **without your login**. It never reads the
  engine's credential store, a credential helper or the login values in
  `config.json`. So only public repositories list their tags. A private
  one shows `needs sign-in; pull by name still works`: `u` pulls through
  the engine, which uses your login.
- HTTPS only. A registry on this machine (`localhost:5000`, `127.0.0.1`,
  `[::1]`) may answer in plain HTTP: termocode tries HTTPS first and uses
  HTTP only when that local registry answers in plain HTTP. A redirect to
  another scheme, or a next page on another host, is refused.
- It lists at most 500 tags (`first 500 tags · pull by name for others`),
  has a 5 second connect limit and a 20 second limit for the whole list,
  and reads at most 1 MB per answer. Only names that are valid tags are
  shown.
- The registry's token lives only inside one request. It is never shown,
  logged or saved.
- Hiding the view stops a request that is still running.

### Search Docker Hub

`s` in REGISTRIES (or **Containers: Search Docker Hub…**) asks for a word
and runs `docker search --format '{{json .}}' --limit 25 --no-trunc
<word>` (Podman: `podman search`). termocode sends no request of its own.
The results open in a picker with the stars and the description. Pick one
for a card: **Pull…** opens the pull prompt with its name, **Browse tags**
lists its remote tags in REGISTRIES. The word may use letters, digits and
`. _ - / :`.

termocode does not keep registry tokens itself yet. A "remember in
termocode" option needs the locked, encrypted secret store, which is not
built yet.

The AI cannot sign in or out: there is no tool for it, and both palette
commands are closed to it.

## Contexts and engine choice

CONTEXTS lists your Docker contexts: `◉ default`, `◦ prod`. The endpoint
(`ssh://deploy@prod`) is in the two lines at the bottom. The section shows
only when a second context exists; with just `default` it stays out of the
way. With Podman, CONTEXTS lists the Podman connections (see
[Podman connections](#podman-connections)).

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
installed. Podman is used through the same commands. Registry logins are
read from `$REGISTRY_AUTH_FILE` or `$XDG_RUNTIME_DIR/containers/auth.json`.
Compose projects are grouped by Podman's own labels.

### Podman connections

With Podman, CONTEXTS lists the connections from `podman system connection
ls --format json`:

```
▾ CONTEXTS ─────────────────── 2
◉ local
◦ podman-machine-default
◦ build-box
```

- On Linux the first row is **local**: Podman on this machine, with no
  connection. On macOS and Windows Podman always uses a connection, so
  there is no **local** row.
- `Enter` uses a connection **in this session only**: every Podman call
  of this termocode session gets `CONTAINER_CONNECTION=<name>` (Podman's
  own setting for "use this connection"), the sections list again and the
  event watcher restarts. Tabs you open from the view get it too. `Enter`
  on **local** goes back.
- `D` (**Make default…**) shows a card with the exact `podman system
  connection default <name>` command. This changes Podman's default for
  every terminal. Cancel or `Esc` runs nothing.
- Listing the connections and making one the default never use the
  session's connection.
- If a remote connection fails, CONTAINERS shows `connection build-box:
  …`; CONTEXTS stays, so you can switch back.
- The section shows only when there is something to choose: two or more
  rows.

The AI cannot switch connections or make one the default.

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
- Remote tags are read without your login (anonymous registry tokens
  only), over HTTPS (plain HTTP only for a registry on this machine), with
  time and size limits. The registry token is never shown or saved.
- Docker Hub search runs the engine's own `search` command.
- The AI has no way to remove, prune, push, tag, run an image, sign in or
  out, change contexts or connections, search or browse tags.

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
