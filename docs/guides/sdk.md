# Build on the agent runtime (SDK)

The agents in termocode's terminal tabs are a runtime your own programs
can drive. A program lists the tabs and their agent states, sends a
prompt to an agent, waits until it is done, watches state changes, and
writes notes on its worktree. Use it to chain agents, to fan one task out
to many tabs, or to build a small bot on top of termocode.

There are three ways in, all on the same control socket:

| Way | For |
|---|---|
| The Go package `github.com/amin-jalali/termocode/sdk` | Go programs |
| `termocode agent list / prompt / wait / report` | shell scripts, CI ([Give your agent the IDE's tools](agents.md)) |
| One JSON line per request on the socket | any other language |

## Where the socket is

Inside a termocode terminal, `TERMOCODE_SOCKET` holds the socket path,
`TERMOCODE_TAB_ID` the tab's id and `TERMOCODE_ENV` is `1`. Outside, the
socket of a workspace is found from its folder. Only processes of your own
user can open it (the folder is `0700`, the socket `0600`).

## Go

```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/amin-jalali/termocode/sdk"
)

func main() {
	c, err := sdk.FromEnv() // TERMOCODE_SOCKET, else the current folder's workspace
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	tab, err := c.FindTab(ctx, func(t sdk.Tab) bool { return t.Agent == "claude" })
	if err != nil {
		log.Fatal(err) // sdk.ErrNoTab: start one with Agent: Start Claude Code
	}
	st, err := c.Run(ctx, tab.ID, "fix the failing tests") // prompt, then wait
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("agent is", st) // done, or waiting when it asks you something
}
```

| Call | What it does |
|---|---|
| `Tabs(ctx)` | the tabs: id, name, agent, state, `Hooked` (the agent reports itself), `Active` |
| `FindTab(ctx, match)` | the first tab `match` accepts, else `sdk.ErrNoTab` |
| `Prompt(ctx, tab, text)` | types the text and presses Enter |
| `Type(ctx, tab, text)` | types the text, no Enter |
| `Wait(ctx, tab, state)` | waits for `working`, `waiting` or `done`; the context deadline is the timeout (10 minutes without one) |
| `Run(ctx, tab, text)` | `Prompt`, a short wait to see the agent start, then `Wait` for `done` |
| `Watch(ctx, every)` | a channel of state changes, and a `Gone` event when a tab closes |
| `Report(ctx, tab, agent, state)` | an agent hook says its own state (`tab ""` is this program's tab) |
| `Worktrees(ctx, dir)` | the worktrees of the repository `dir` is in |
| `WorktreeNote / WorktreeStatus / WorktreeLink` | a note, a status (`working`, `blocked`, `done`) or an issue/PR link on the worktree |

Each call opens its own connection, so one `Client` is safe to share
between goroutines. Canceling the context ends the call.

## You stay in charge

The SDK has no more power than the `termocode agent` commands:

- termocode asks **Let a script type into a terminal tab?** before a
  prompt is typed into a tab. **Deny** is first, so a stray `Enter` sends
  nothing; **Allow for this session** lets later prompts in until
  termocode quits. `Prompt` returns an error when you deny.
- `WorktreeLink` also asks you first.
- The SDK never sees secrets, the AI chat or your keys. It only types into
  tabs and reads their states.

## Any other language

Send one JSON object per line and read one JSON line back. The ops are
`list`, `prompt`, `wait`, `report`, `worktree-list`, `worktree-note`,
`worktree-status` and `worktree-link`.

```python
import json, os, socket

def call(req):
    s = socket.socket(socket.AF_UNIX)
    s.connect(os.environ["TERMOCODE_SOCKET"])
    s.sendall((json.dumps(req) + "\n").encode())
    rep = json.loads(s.makefile().readline())
    s.close()
    if not rep.get("ok"):
        raise RuntimeError(rep.get("error"))
    return rep

print(call({"op": "list"})["tabs"])
call({"op": "prompt", "tab": "t1", "text": "write the release notes"})
print(call({"op": "wait", "tab": "t1", "until": "done", "timeout_s": 600})["state"])
```

| Field | Used by |
|---|---|
| `op` | every request |
| `tab` | `prompt`, `wait`, `report` |
| `text` | `prompt`, `worktree-note`, `worktree-link` |
| `enter` | `prompt`: `false` types without Enter (default `true`) |
| `until`, `timeout_s` | `wait` |
| `agent`, `state` | `report`; `state` also for `worktree-status` |
| `dir` | the worktree ops: the folder the worktree is found from |

A reply has `ok`, and `error` when it failed. `list` adds `tabs`, `wait`
adds `state`, `worktree-list` adds `worktrees`.
