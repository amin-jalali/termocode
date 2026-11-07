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
| `?` | open a card with the keys of the row under the cursor; it stays open until `Esc`, and **Open guide** brings you to this table |
| `Space` | fold / open the section or group under the cursor |
| `Enter` / `l` | on a container: logs tab; on a context: use it; on an image tag: run it (asks); on a HELP row: run it |
| `s` / `x` / `r` | start / stop / restart the selected container; on a project group, start / stop / restart that project |
| `a` / `A` | start all / stop all of the project's services (the **Start all** and **Stop all** buttons); off while the engine is down |
| `p` | pause or unpause the selected container |
| `K` | kill the selected container (asks first: Cancel, Stop gently with TERM, or Kill) |
| `N` | rename the selected container |
| `w` | change the selected container: restart policy, CPU and memory limits, connect to or disconnect from a network (each asks first) |
| `O` | sort the containers (name, image, state, created, size) inside each project; turn the SIZE column on or off |
| `e` | shell in the selected container (terminal tab) |
| `E` | exec with options: shell or command, user, start folder |
| `W` | open a folder of the running container as the workspace |
| `t` | attach to a container; on an image, tag it |
| `S` | follow live stats |
| `T` | debug a running container with a separate toolbox container (see [Debug toolbox](#debug-toolbox)) |
| `i` | inspect: a read-only JSON page in the editor |
| `L` | logs with options: tail, since, timestamps; save to a file; search |
| `I` | details: status, exit code, restarts, health log, processes, mounts, labels, env names (no values) |
| `F` | browse the files of a running container: view read-only, download, upload (asks first) |
| `o` | open the first published port in the browser |
| `y` | copy the ID, name, host or endpoint of the row |
| `u` | pull an image |
| `b` | on an image: show its remote tags; in REGISTRIES: browse the tags of a repository |
| `s` | in REGISTRIES: search Docker Hub |
| `P` / `h` | push an image (asks first) / image history |
| `c` / `C` | on an image: scan it for vulnerabilities / make its SBOM (needs docker scout, trivy, grype or syft) |
| `n` | create a network or volume; connect a registry |
| `d` | read volume disk usage (on VOLUMES) |
| `z` | prune what the section heading offers (asks first) |
| `D` | remove, sign out or make default (asks first) |
| `n` / `i` / `x` | on CONTEXTS: create a context (name, host, TLS files; asks first) / inspect it / remove it (asks first) |
| `R` | refresh all |
| `m` | choose the sections (the **⋯** menu) |
| `!` | your custom commands for the row (see [Custom commands](#custom-commands)) |
| `Esc` | clear the filter; without a filter, focus the editor |

Mouse: click moves the cursor; a click on a header or `▸` / `▾` folds it;
a click on an action row (Connect registry…, the HELP rows) runs it;
double-click on a container opens its logs, on a context uses it; right
click opens the menu of the row; the wheel scrolls.

Right-click a row for the same actions as its keys; items that ask first end with `…`.
On a container the menu also holds Kill…, Rename…, Restart policy…, CPU and
memory limits…, Connect to network… and Disconnect from network…. Limits are
typed as `CPUs memory swap`, like `1.5 512m 1g`; `-` keeps a value. Every
change but the rename shows the exact command on a card with Cancel first.
Closing a logs tab stops only `logs -f`, never the container.

## Sort and size

`O` (or **Containers: Sort Containers…**) picks how containers are ordered:
project order (the default), name, image, state (running first), created
(newest first) or size (largest first). Containers stay in their compose
project; the order applies inside each group, and the CONTAINERS line
shows `by <key>`. The last line of the list shows or hides the SIZE
column. Sizes come from `ps --size`, which is slower on big hosts, so the
column is off until you ask; sorting by size turns it on.

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

### Exec with options

`E` on a running container (or **Containers: Exec with Options…**) asks
three short questions; each may stay empty. **Shell or command**: empty
keeps the bash-or-sh default, one word is a shell (`zsh`, `ash`), more
words run as a command without a shell (`ps aux`). **User**: a name or
uid (`root`, `1000:1000`). **Folder**: an absolute path (`/app`). The exec
opens in a terminal tab. Values that start with `-` are refused.

### Logs with options

`L` on a container (or **Containers: Logs with Options…**) opens a picker:
follow the last 200 or 1000 lines, all lines, the last 10 minutes or hour,
since a duration or date you type, or the last N lines you type.
**Timestamps** switches the engine timestamps on or off for the next
follow. **Save logs to a file** writes the last 10000 lines to a new file
in your Downloads folder. **Search the logs…** opens the matching lines of
the last 10000 in a read-only page (any case).

### Container details

`I` on a container (or **Containers: Show Details**) opens a read-only
page with its status, exit code and restart count, the health check
state and its last results, the processes (`top`, only while it runs),
mounts, labels and the names of its env variables. Env values are never
read into the page; use `i` (inspect) when you need the raw JSON.

### Container files

`F` on a running container (or **Containers: Browse Files**) lists its
folders in a picker, starting at `/`. Enter opens a folder; `..` goes up.
A file opens a card: **View** shows it read-only in the editor (the first
1 MB of text; a binary file is not shown), **Download** saves a copy in your
Downloads folder and never replaces a file there (`name (1)`, `name (2)`…).
**Upload a file here…** asks for a local path, then a card with **Cancel**
first; a file that is already in the container is replaced only after a
second **Replace** card. Nothing is written in the container without a card.

Logs, shell, attach, stats and pulls run in terminal tabs, so you see
progress and can type.

### Open a folder in a container

`W` on a running container asks which folder to open, filled with the
container's working folder. Enter restarts termocode on that folder; the
editor stays on your machine and nothing is installed in the container.
The same works from the palette (**Workspace: Open Folder in
Container…**) and from the command line:
`termocode --container api-1 [--context prod-ssh] /app`.

The folder then shows in the recent workspaces as `▣ api-1 · prod-ssh /app`.
The status bar shows a `▣ api-1` pill. When the container stops, the pill
says `stopped` and a card offers **Wait** (keep the open files),
**Start**, or **Disconnect** (open your home folder instead).

In a container workspace, Source Control runs git inside the container:
stage, unstage, discard, commit, push, pull, fetch, checkout, and new
branches all work there (git must be in the image). Staging one hunk is
not there yet; use the terminal for it. **Replace in Workspace** reads
and writes the files through the container too.

### Reopen in a dev container

**Workspace: Reopen in Dev Container…** reads the project's
`.devcontainer/devcontainer.json` (or `.devcontainer.json`). A card says
which container it makes and lists every command of the file and where
it runs (`initializeCommand` on this computer, the others in the new
container). **Cancel** is first; nothing runs before **Create and Open**.
termocode builds the image (for `build`), starts the container with the
project folder mounted, runs the commands, then opens the folder in it.
`containerEnv` goes in a private env file, never on the command line;
forwarded ports listen on 127.0.0.1 only.

The container is named `termocode-dc-<id>` and found again next time: an
existing one is only started, the commands do not run again.

Files with `dockerComposeFile` or `features` use the devcontainer CLI
(`npm install -g @devcontainers/cli`); termocode says so when it is not
installed. The card then also lists every feature (each one runs its own
install script in the container), and one `devcontainer up` makes the
container and runs the commands. termocode marks that container with the
`termocode.devcontainer` label, so next time it is only started. A
changed command or feature asks again.

When you open a local folder with a devcontainer.json, one quiet toast
offers to reopen it in a dev container. A click shows **Not now** (first),
**Never for this project** and **Reopen in Dev Container**, which goes on
to the card above. Nothing is made from the toast itself.

The right-click menu of a container also has **Duplicate…** and
**Recreate (pull, same settings)…**. Both read the container's settings
(`inspect`: image, name, ports, volumes, network, restart policy, user,
workdir, labels, entrypoint, command and env) and show a card with
**Cancel** first and the exact command. Duplicate starts a copy named
`<name>-copy`. Recreate pulls the image again, removes the container
(`rm -f`; volumes stay) and starts a new one with the same settings. Env
values are never in the command: it names them (`-e NAME`) and the
terminal tab's environment carries the values.

## Compose extras

The palette has more compose commands for this folder's project:

- **Containers: Show Resolved Compose Config** opens `compose config` (the
  project with variables, extends and profiles applied) in a read-only page.
- **Containers: Run One-Off Command in Service…** asks for
  `service command args…` and runs `compose run --rm` in a terminal tab.
- **Containers: Watch Project (Rebuild on Change)** runs `compose watch` in
  a tab (the `develop.watch` rules of the compose file).
- **Containers: Choose Compose Env File…** picks `.env`, `.env.*` or
  `*.env` for `--env-file`, for this session only.
- **Containers: Deploy Stack from Git…** asks for a repository URL
  (`https://`, `ssh://` or `user@host:path`; add `#branch` or `#tag`). A
  card with **Cancel** first shows the exact commands: a shallow
  `git clone` into `~/.local/share/termocode/stacks/<name>` (or a
  fast-forward `git pull` when it is already there), then
  `compose up -d` in it. **Deploy** runs them in a terminal tab. The compose
  file of that repository decides what runs, so deploy only repositories you
  trust. The AI cannot start it.

## Live CPU and memory

While the Containers view is on screen and the CONTAINERS section is open,
termocode asks the engine for stats of the running containers every 2
seconds (one `stats --no-stream` call for all of them, in the context the
view uses). Nothing runs when the view is hidden, the terminal window is
in the background, or no container is running. Stopped containers have no
stats.

- The selected running container shows four lines under its row: a CPU
  graph (percent of one core), a memory graph (used, scaled to the limit),
  and the network and disk totals. Each graph is the last 60 samples,
  about two minutes. Unicode block characters draw it; with `icons`
  set to `ascii` it uses `_.:-=+*#`.
- With a wider sidebar the row shows the values too: `12% · 210 MB` from
  44 columns, `CPU 12% · MEM 210 MB` from 62 (drag the splitter).
- If the engine does not answer or does not support stats, the section
  shows one quiet line (`stats unavailable: …`) and tries again every 10
  seconds. The graphs keep what they had.

## Images, networks, and volumes

The IMAGES section groups tags by repository and keeps dangling images in a
separate `<none> dangling` group. A repository count includes the number of
images and, when applicable, how many containers use them. On a tag, `u`
pulls, `P` asks before pushing, `t` asks for a new tag, `Enter` asks for a
container name, ports, and environment entries, then shows the full run
command before opening a terminal tab. **More options…** on that card takes
`volume=SRC:DST`, `network=`, `restart=`, `user=`, `workdir=`, `hostname=`,
`platform=`, `label=KEY=value`, `entrypoint=`, `detach`, `no-rm`, then `--`
and the command, and shows the new command (env values passed by name only). `h` opens image history, `i` opens
read-only image details, and `D` removes an image only when no container uses
it. `z` on the IMAGES heading asks before pruning dangling images; it never
removes tagged images.

`c` on an image scans it for known vulnerabilities with the scanner you
have installed (trivy, then grype, then docker scout). The findings open as
a read-only page, most severe first, and a toast gives the count per
severity. `C` makes a CycloneDX SBOM (the list of software in the image)
with syft, trivy or docker scout and opens it as read-only JSON. Both only
read the image. With no scanner installed, a toast says what to install.
The first scan can take minutes while the scanner downloads its database.

NETWORKS and VOLUMES show their driver and scope. `Space` expands a row and
checks which containers use it. `n` asks for a name and creates a network or
volume. `D` shows the exact remove command and users before removing; a volume
in use is kept, and built-in networks cannot be removed. `z` on either heading
asks before pruning. Volume pruning lists only unnamed volumes (their data
is deleted); volumes with a name are kept. On VOLUMES, `d` reads disk usage once and fills in the
reported sizes. Disk usage is not queried in the background.

## Dockerfiles and compose files

In a Dockerfile every `FROM` line shows a **▸ build** lens at the right
edge (**▸ build to <stage>** on a named stage that is not the last one).
In a compose file every service shows **▸ up <service>**. Click the lens,
or run **Containers: Run Dockerfile Stage or Compose Service at Cursor**
with the cursor anywhere in that block. A card shows the full command
first (**Cancel** is the default); **Build** or **Start** runs it in a
terminal tab.

**Containers: Add Docker Files…** writes a starter `Dockerfile`,
`compose.yaml` and `.dockerignore`. The template that fits the folder
(Go, Node.js, Python, static site, or a plain Alpine shell) is listed
first. The card lists the files it creates and the files that already
exist; existing files are never overwritten.

Your own templates: **Containers: Open Docker Templates Folder** opens
`docker-templates/` in the config folder. Each sub-folder there is one
template; its files are copied as they are.

**Containers: Add Compose Service…** adds a service (PostgreSQL, MySQL,
Redis, MongoDB, RabbitMQ, nginx) to the end of the `services:` block of
the workspace compose file, or creates `compose.yaml`. The card shows the
service first; a name that is already there is refused.

`tasks.json` also takes `"type": "docker-build"`, `"docker-run"` and
`"docker-compose"` tasks, and **Run Task…** offers docker build and
compose up / down when the folder has a Dockerfile or compose file.

## Custom commands

Add your own row commands in `container-commands.json` in the config
folder (**Containers: Edit Custom Commands** opens it, with an example the
first time). Each entry has a `name`, a `view` (`containers`, `images`,
`volumes`, `networks`, `compose`, `pods`, `deployments`, `services`,
`jobs`), an `argv` list and an optional `key`:

```json
{ "commands": [
  { "name": "Top processes", "view": "containers", "key": "w",
    "argv": ["docker", "top", "{{id}}"] }
] }
```

Placeholders: `{{id}}`, `{{name}}`, `{{image}}`, `{{context}}`,
`{{namespace}}`. `!` on a row (or **Custom Commands…** in the row menu, or
**Containers: Run Custom Command on Row…**) lists the commands of that
view; the `key` runs one straight from the view. A key the view already
uses is refused. Every command shows a card with the command as it will
run (**Cancel** first). There is no shell: each `argv` entry is one
argument, and a row value that would start with `-` is refused.

## Several contexts at once

**Containers: Show All Contexts Together** adds an **ALL CONTEXTS**
section above HELP. It asks every docker context (or podman connection)
at the same time and lists their containers, grouped by context. A
context that does not answer shows one line with its error; the others
still show. The rows only show: `Enter` uses that context in this
session (your default stays as it is), then the usual keys work on its
rows in CONTAINERS. `y` copies the id, `r` loads the section again. It
never loads on a timer. Run the command again to hide the section.

## Debug toolbox

Some images have no shell or tools (distroless). `T` on a running
container (or **Debug with Toolbox…** in its menu, or **Containers: Debug
Selected with Toolbox…**) asks for a toolbox image (`busybox:stable` by
default), then shows the command on a card (**Cancel** first). The
toolbox is a separate, throw-away container that shares the target's
processes and network (`--pid container:…`, `--network container:…`), so
`ps`, `netstat` and `wget` see the target. The target is never changed:
nothing is copied into it and nothing runs inside it. The toolbox is
removed when its shell ends.

## Pruning

Pruning always asks first. Every prune card names what goes (counts from the
last list), the exact command (last, in a quiet colour), and has one red
button named like the title: `Remove`, or `Clean up` for the whole system.
`Esc` or `Cancel` runs nothing. If a session context is set, the card names it; the
command runs there.

| Where | Command |
|---|---|
| CONTAINERS heading (`z` or right-click): stopped containers | `docker container prune -f` |
| IMAGES heading: dangling images | `docker image prune -f` |
| IMAGES heading: **Prune incl. unused images…** (a separate item) | `docker image prune -a -f` |
| NETWORKS heading: unused networks | `docker network prune -f` |
| VOLUMES heading: the card lists the unnamed volumes first | `docker volume prune -f` |
| `⋯` menu: **Prune system…** (the card says **Clean up unused items?**) | `docker system prune -f` |

System prune never adds the flag for all unused images or for volumes. Those
are only on the IMAGES and VOLUMES headings. The palette has the same entries
(**Containers: Prune Stopped Containers…**, **Prune Dangling Images…**,
**Prune Networks…**, **Prune Volumes…**, **Prune System…**).

When it works, a toast shows what the engine reported: `Pruned · reclaimed
1.2GB`, and the affected sections list again. When it fails, the toast shows
the engine's own error. Every prune has a two minute limit.

The AI cannot prune: there is no tool, and every prune palette command is
closed to it.

## System page

**Containers: Show System Page** opens a read-only page about the engine:

- the engine: version, OS, kernel, CPUs, memory, storage driver, runtime
  and cgroup, with the container and image counts and the engine's warnings;
- the disk summary of `docker system df` (images, containers, volumes,
  build cache, and what can be reclaimed);
- the recent events (newest first, up to 50) seen while the view was open;
- the action log: every container action of this session (start, stop,
  remove, prune, tag …) with its time and whether it worked.

The page only reads. The AI may open it.

## Builds

- **Containers: Show Builds (History, Builders, Cache)** opens a read-only
  page: the recent builds (newest first, with time, length, cached steps and
  the error of a failed build), the buildx builders (`*` marks the current
  one, with driver, status and platforms) and the build cache usage.
- **Containers: Show Build Detail…** lists the recent builds; pick one to see
  `docker buildx history inspect` in a terminal tab.
- **Containers: Prune Build Cache…** first reads the cache size, then shows a
  card: `Cancel` (first), `Prune unused`, and `Prune all` (the next builds
  start cold). The card names the command. The AI cannot prune.
- **Containers: Check Dockerfile (Build Checks)** runs `docker build --check .`
  in the workspace folder in a terminal tab. No image is built.

Build history and builders need docker buildx; with podman the page says so.

## Archive: save, load, import, commit, export

Select an image or a container row, then use the palette. A file path may be
relative: it is read from the workspace folder.

| Palette command | What it does |
|---|---|
| **Save Selected Image to File…** | `docker save -o <file> <image>`. If the file exists, a card asks first (`Cancel` is first). |
| **Load Images from File…** | `docker load -i <file>`, after a card: an image with the same tag is replaced. |
| **Import Image from File System Tar…** | Type the tar file, then the new image name (optional): `docker import`. |
| **Commit Selected Container to Image…** | Type the new image name, then a message (optional): `docker commit`. |
| **Export Selected Container to File…** | `docker export -o <file> <container>`; a card asks if the file exists. |
| **Show Selected Container Changes (Diff)** | A read-only page of the files the container added (`+`), changed (`~`) and deleted (`-`). |
| **Show Selected Image Layer Sizes** | A read-only page of the layers that add bytes, largest first, with their share of the image. |
| **Show Containers Using Selected Image** | A read-only page of the containers made from the image. |

A save, load, import or export may take up to 30 minutes. A toast says when it
is done; the IMAGES section lists again after a load, import or commit. The AI
may open the three read-only pages; it cannot save, load, import, commit or
export.

## Volume files, backup, restore and clone

Select a VOLUMES row, then use the palette. These jobs run a small helper
container (`busybox:stable`, no network); the engine pulls it the first time.

| Palette command | What it does |
|---|---|
| **Browse Selected Volume Files…** | Type a folder inside the volume (`/` is the top). A read-only page lists its folders and files. The volume is mounted read-only. |
| **Back Up Selected Volume…** | Type a folder; the backup is written there as `<volume>-<date>-<time>.tar.gz`. |
| **Restore Selected Volume from Backup…** | Type the `.tar.gz` file. A card asks first (`Cancel` is first): files with the same name are overwritten. |
| **Clone Selected Volume…** | Type the new volume name. A card asks first; if that volume exists, the card says so. |
| **Create Volume with Options…** | `name driver=local opt.type=tmpfs opt.device=tmpfs label.team=web` |
| **Create Network with Options…** | `name driver=bridge subnet=10.5.0.0/16 gateway=10.5.0.1 ip-range=10.5.1.0/24 internal attachable ipv6 label.app=web` |

A relative path is read from the workspace folder. With a remote context, the
backup folder is a folder on the engine's machine. A backup, restore or clone
may take up to 30 minutes; a toast says when it is done. The AI cannot run
any of these commands.

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
just before the call. A reference that is not valid is refused in the
prompt and nothing runs; the first run of a `${cmd:…}` shows a
`Running: …` toast. See
[References](ai-setup.md#type-a-reference-instead-of-a-secret).

Then it runs `docker login <host> --username <user> --password-stdin`. The
token goes to the engine on stdin only. It is never in the command line,
the environment, a log, a toast, the session file or any file of
termocode's. The engine's own credential store keeps it. If the login
fails, the toast shows the engine's message with the token removed. A login
has a one minute limit. `Esc` on any step runs nothing and forgets what you
typed.

**Remember in termocode.** When your secret store is the OS keyring or an
unlocked encrypted file (never the plain file), a card after the user name
asks *Remember the token in termocode?*. After a good sign-in the token is
saved as `registry.<host>` (for example `registry.ghcr.io`). The next time
you connect to that host, a card offers *Use saved token* (a locked
encrypted file asks for its passphrase first). The token still goes to the
engine on stdin only. See **AI: Secret Store…** to list or forget it.

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

### Private tags, catalog and deleting a tag

When a repository needs a login and termocode has a token saved for that
host (**Connect registry…** with **Remember**, stored as
`registry.<host>`), a card asks first: **Cancel** or **Use saved token**.
After **Use saved token** termocode sends that token, for this session
only, to the token server the registry names (HTTPS, as HTTP Basic) and
lists the private tags. It is never shown or logged, and the AI never gets
it. Without a saved token the row keeps `needs sign-in; pull by name still
works`.

**Containers: Browse Registry Catalog…** (or **Browse catalog…** in a
registry row's menu) asks for a host and lists its repositories
(`/v2/_catalog`, at most 500) in a picker. Pick one to list its tags.
Docker Hub has no catalog: use search.

`D` on a remote tag (or **Delete tag from registry…** in its menu) reads
the tag's manifest digest with your saved token and shows a card with
**Cancel** first. The registry deletes by digest, so every tag on that
manifest goes too. Local images stay. Many registries turn deletes off;
then the card's action says so.

### Image updates

**Containers: Check Running Images for Updates** asks, for each tag a
running container uses (at most 20, digest-pinned images skipped), the
registry's digest of that tag and compares it with the local image's repo
digest. It runs only when you ask. Images with a newer version are named in
a notice, and their containers show `update available` in the footer.
Nothing is pulled or restarted: `u` on the image pulls it, and
**Recreate (pull, same settings)…** in the container menu restarts with it.
Local-only and private images (without an opted-in saved token) are
counted as not checked.

### Cloud registries

**Connect registry…** also lists **AWS ECR…**, **Azure Container
Registry…** and **Google Artifact Registry…**. These sign in with their own
CLI, so the card shows the command to run in a terminal (`aws ecr
get-login-password … | docker login …`, `az acr login --name …`,
`gcloud auth configure-docker …`) with **Copy command**. termocode never
runs a cloud CLI. Typing such a host (`*.dkr.ecr.*.amazonaws.com`,
`*.azurecr.io`, `*-docker.pkg.dev`, `gcr.io`) shows the same card with
your host filled in, and **Use a token instead…** goes on to the normal
sign-in. For `ghcr.io` the user prompt says which GitHub token to use.

### Search Docker Hub

`s` in REGISTRIES (or **Containers: Search Docker Hub…**) asks for a word
and runs `docker search --format '{{json .}}' --limit 25 --no-trunc
<word>` (Podman: `podman search`). termocode sends no request of its own.
The results open in a picker with the stars and the description. Pick one
for a card: **Pull…** opens the pull prompt with its name, **Browse tags**
lists its remote tags in REGISTRIES. The word may use letters, digits and
`. _ - / :`.

The AI cannot sign in or out: there is no tool for it, and both palette
commands are closed to it.

## Contexts and engine choice

CONTEXTS lists your Docker contexts: `◉ default`, `◦ prod`. The endpoint
(`ssh://deploy@prod`) is in the two lines at the bottom. The section shows
only when a second context exists; with just `default` it stays out of the
way. With Podman, CONTEXTS lists the Podman connections (see
[Podman connections](#podman-connections)).

`n` on CONTEXTS (or **Containers: Create Context…**, which works also while
only `default` exists) asks for a name and a host: `ssh://user@host`,
`tcp://host:2376` or `unix:///path`. A `tcp://` host also asks for the TLS
files (CA, cert and key paths, or none); with Podman an `ssh://` host asks
for the identity file. A card shows the exact `docker context create`
command, Cancel first. Only the paths go into the context: termocode does
not read the files. A new context is not used until you press `Enter` on it.
`i` opens `docker context inspect` as read-only JSON. `x` removes a context
after a card; `default`, the machine default and the context this session
uses cannot be removed.

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
| **Keys** | opens this page at [Keys](#keys); `?` in the view opens a card with the keys of what has focus |
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
containers** and **Deny**. **Deny** comes first and has the focus, so a stray `Enter` allows nothing. "Always allow containers" lasts until you quit
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
| No container engine | Install Docker or Podman, then `R`. |
| Docker Compose v2 is needed | Update Docker (the `docker compose` plugin). |
| Docker is not running (or Podman) | The view shows the start command (`y` copies it; termocode never runs it): `sudo systemctl start docker` on Linux, `podman machine start` for Podman on macOS and Windows, `systemctl --user start podman.socket` for Podman on Linux. On macOS and Windows Docker runs in Docker Desktop: open it. Then `R`. The rows stay, with `?`. |
| Change engine settings | **Containers: Open Engine Config** opens `daemon.json` (Docker: `/etc/docker/daemon.json` on Linux, `~/.docker/daemon.json` with Docker Desktop) or `~/.config/containers/containers.conf` (Podman). Restart the engine after you save it. |
| No access to the Docker socket | The view shows `sudo usermod -aG docker $USER` (`y` copies it). Members of the `docker` group are root-equal on that machine: think first, or use [rootless Docker](https://docs.docker.com/engine/security/rootless/). termocode never runs `sudo`. |
| compose.yaml: … | The first error line; `o` opens the file. |

## Termux (Android)

No container engine: PROJECT shows local commands and HELP stays. Nothing asks you to install an engine.
