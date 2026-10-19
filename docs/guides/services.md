# Containers and project services

The **Containers** view has your project's services on top (the
**PROJECT** section) and, below, what runs on this machine: **CONTAINERS**,
**IMAGES**, **REGISTRIES**, **NETWORKS**, **VOLUMES**, **CONTEXTS** and
**HELP**. Container and project actions run through the detected engine.

- **Space** (or a click) folds and opens a section. A section loads when
  you open it, and only while the view is on screen.
- **m**, or the **⋯** on the title row, picks which sections show. The
  choice is the same in every workspace (`containers_sections` in
  `config.json`); folded sections are kept per workspace. The menu also
  has **Refresh all**.
- **R** refreshes everything.
- CONTAINERS groups containers by compose project (this folder's project
  first, marked `·`, then `other` for the rest) and shows the short service
  name, for example `postgres-1`. Its first line names the engine and the
  active context, for example `docker 27 · default`.
- No Docker or Podman? CONTAINERS says so; the project services still work.

The PROJECT section lists services your project needs while you work: the
database, the cache, the mail catcher, your own dev server. Start one with a
key, read its logs in a terminal tab, and let the AI chat check them.

termocode does not install anything. It drives what the project already
declares:

| File | Backend | What termocode runs |
|---|---|---|
| `compose.yaml`, `compose.yml`, `docker-compose.yml`, `docker-compose.yaml` (first found; `COMPOSE_FILE` wins) | Docker Compose v2 | `docker compose …` |
| `.termocode/services.json` | local commands (and compose file / profiles) | your shell line in a terminal tab |
| `Procfile`, `Procfile.dev` | local commands | `name: command` in a terminal tab |

Open the view with the **☷** icon in the activity bar, or **Containers: Focus**
in the palette. The existing `services-*` palette commands remain available
for the PROJECT section. Nothing runs before you open it.

## The view

```
CONTAINERS · TERMOCODE
compose · termocode       3/4 up

▸ Start all  ▪ Stop all        ↻

▾ PROJECT ───────────────────── 4
◉ postgres     :5432    2h 14m
◉ redis        :6379    2h 14m
◌ api          :8080       12s
◦ mailhog      :8025

▾ LOCAL ─────────────────────── 1
◉ web          :5173     1h 2m
```

Each row: state, name, published port, uptime. The two lines at the bottom
explain the row under the cursor (image, ports, Docker's status text).

| Mark | Meaning |
|---|---|
| ◉ green | running (healthy, or the port answers) |
| ◉ amber | running, but its port does not answer |
| ◉ red / `!` | unhealthy (its Docker healthcheck fails) |
| ◌ | starting, pulling or stopping |
| ↻ | restarting |
| ◦ | stopped or never created |
| ✘ | exited with an error (`exit 1` on the right) |
| ? | Docker does not answer |

## Keys

| Key | Action |
|---|---|
| `j` `k` `g` `G` | move |
| `Enter` / `l` | logs: open (or show) the `logs: <name>` terminal tab; on PROJECT, all services |
| `s` | start (`docker compose up -d <name>` in the **services: up** tab, so pulls show) |
| `x` | stop (`docker compose stop`, containers and volumes stay) |
| `r` | restart |
| `a` / `A` | start all / stop all in PROJECT |
| `t` | attach to a container (CONTAINERS rows) |
| `s` / `x` / `r` | start / stop / restart the selected container; on a project group, start / stop / restart that project |
| `p` | pause or unpause the selected container |
| `e` / `D` | shell / remove the selected container (removal asks first) |
| `R` | refresh now (the project and every open host section) |
| `m` | choose the sections (the **⋯** menu) |
| `e` | shell in the container (`docker compose exec <name> sh`) |
| `o` | open the file the service comes from (at its line) |
| `y` | copy the address (`localhost:5432`) |
| `Space` | fold / open the section or group under the cursor |

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

Right-click a row for the same actions, plus **Recreate** and **Down
(remove containers)**, which asks first. Double-click opens the logs.
Closing a logs tab stops only `logs -f`, never the service.

The status bar shows `☷ 3/4` (running / declared) once you opened the view;
click it to come back. `· ✘ 1` means one service is unhealthy or exited.

## Local commands: `.termocode/services.json`

**Services: Configure** (or `o` in an empty view) writes a commented
template:

```jsonc
{
  "compose": { "file": "compose.yaml", "profiles": ["dev"] },
  "services": [
    { "name": "api", "command": "go run ./cmd/api", "cwd": ".", "env": { "PORT": "8080" },
      "port": 8080, "health": { "http": "http://localhost:8080/healthz" } },
    { "name": "redis", "command": "redis-server --port 6379", "port": 6379 }
  ]
}
```

- `name`: `[a-z0-9_-]+`, unique. `command`: a shell line, like `tasks.json`.
- `cwd`: inside the project. `env` values may use `${workspaceFolder}`.
- `port`: shown in the list and probed (TCP) when `health` is not set.
- `health`: `{"tcp": 6379}` or `{"http": "http://…"}` (2xx / 3xx is OK).

Each local service runs in its own terminal tab: its tab is its log. `x`
sends SIGTERM. The first start of a command line in a project asks you
first (project files can run commands); termocode remembers your OK.

## Status without polling

termocode reads `docker compose ps` when the view opens, on `R`, and after
your actions. While the view is on screen one `docker events` process tells
it when something changed. When you hide the view (another activity item,
`Ctrl+B`, zen mode) that process stops: a hidden view runs nothing, and the
chip may be old (it turns grey).

## Quitting

termocode never stops a service it did not start. If you started services
in this session, quitting asks: **Keep running and quit**, **Stop and
quit**, or **Cancel**. Set `services_on_quit` in `config.json` to `keep`
or `stop` to skip the question. Local services always stop with the editor.

## The AI chat

The AI can look at your services: `services_list` (names, images, ports,
env key **names**), `services_status`, `services_logs`. It can also start,
stop and restart them; these ask first, with **Allow**, **Always allow
services** (every project) and **Always in project** (this project only,
kept in termocode's own session file, never in the repo). **AI: Services
Trust…** forgets the project answer.

It never gets `down`, `--force-recreate`, `exec` or volume commands.

Ask it "my app can't connect to the database — why?": it reads the state
and the logs before it answers.

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

## Over SSH

Docker runs on the server, so the ports are server ports. `y` copies
`localhost:5432`, and the toast shows the forward command for your own
computer: `ssh -L 5432:localhost:5432 <host>`. termocode opens no tunnels.

## Termux (Android)

No Docker: the compose part of PROJECT does not show, and nothing asks you to
install it. Local commands work. Android does not let apps listen on ports
below 1024; the exit toast says so.
