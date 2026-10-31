# Set up AI

AI in termocode is optional. Until you set it up, every AI command only shows
a short "configure" message. Nothing is sent anywhere.

You can use:

| Provider | Needs |
|---|---|
| Anthropic | an API key, or your Claude Pro / Max plan (through Claude Code) |
| OpenAI | an API key, or your ChatGPT Plus / Pro plan (through Codex) |
| OpenRouter | an API key |
| Ollama (local) | Ollama running on your machine |
| LM Studio (local) | LM Studio's server running |
| OpenAI-compatible | a base URL (ending in `/v1`) and maybe a key |
| Claude (OAuth sign-in) | a Claude account — opt-in, see the warning below |

## Option A: add an AI in the app

1. Press `F1` and run **AI: Add AI…**. A card opens.
2. Pick an AI: **Anthropic**, **OpenAI**, **Ollama**, **OpenRouter**,
   **LM Studio** or **Other server**. Use `↑` / `↓` and `Enter`, or click it.
3. Connect it. The card asks only what this AI needs:

    | AI | What you do |
    |---|---|
    | Anthropic, OpenAI | Choose **Use an API key**, paste the key, press **Connect** |
    | OpenRouter | Paste the key, press **Connect** |
    | Ollama, LM Studio | Nothing. termocode looks for the server on this machine at once |
    | Other server | Type the server URL (OpenAI-style, ending in `/v1`) and a key if the server needs one, press **Connect** |

4. termocode checks the AI by loading its model list. The card then says
   *Anthropic is ready* and shows the models. Press **Done**.

The key is hidden while you type. termocode keeps it in memory, checks it,
and saves it only when the AI accepts it. When something goes wrong, the
card says it in one sentence and you stay on the same step:

| The card says | What to do |
|---|---|
| *That key was not accepted.* | The key stays in the field. Fix it and press **Connect** again |
| *Could not reach Anthropic. Check your connection.* | Check the network, then **Connect** again |
| *Ollama is not running on this machine.* | Install it (ollama.com, lmstudio.ai), start it, then press **Try again** |
| *Take the user or password out of the URL.* | A server URL never holds a password. Put the key in the key field |

The details of an error go to `errors.log` (keys removed), never to the card.

`Esc` or **Cancel** closes the card at any step. Nothing is saved before the
AI answers, and an answer that comes after you closed the card is dropped.

### Sign in with your plan

**Sign in with Claude Pro / Max** (Anthropic) and **Sign in with ChatGPT
Plus / Pro** (OpenAI) use your plan through the vendor's own CLI: Claude
Code or Codex. termocode never sees your password. The CLI signs you in in
your browser and keeps the login; termocode only reads the link it prints
and asks the CLI whether you are signed in.

1. Pick **Anthropic** or **OpenAI**, then the sign-in.
2. When the CLI is not on this machine yet, the card says so and lists the
   three steps. Press **Install and sign in** (one click).
3. The card shows each step, with a ✓ when it is done:
    - **Installing Codex** with a timer. termocode installs the CLI with
      `npm` into its own tools folder (`~/.local/share/termocode/tools`),
      not into your system.
    - **Sign in with your browser**. Your browser opens. Finish there; the
      card waits. When the browser did not open, press **Copy the link**
      and open it yourself. When the CLI asks for a code, a field appears:
      paste the code from the browser and press `Enter`.
    - **Loading your models**.
4. The card says *OpenAI is ready* and shows the models. Press **Done**.

When the CLI is already installed, the install step is skipped. When it is
already signed in, the card goes straight to the models.

Over SSH (no display on the machine), Codex uses a device sign-in: the card
shows a link and a short code. Open the link in a browser on any device
and type the code.

`Esc` or **Cancel** stops the work at any step: the install and the CLI's
sign-in end, and nothing is saved.

| The card says | What to do |
|---|---|
| *Codex needs Node.js* | Install Node.js (nodejs.org or your package manager), then press **Try again**. Or press **Use an API key** |
| *No connection.* | Press **Try again** when you are online |
| *Sign-in took too long.* | The sign-in waits 5 minutes. Press **Try again** |
| *Sign-in did not finish.* | The CLI ended without a login. Press **Try again** or **Use an API key** |

A connected sign-in is saved in `providers.json` with the method `cli` and
the CLI's own model list. Removing it from termocode does not sign you out
of the CLI.

An AI connects one way. When Anthropic already uses an API key and you pick
the sign-in (or the other way round), the card asks first. **Keep** comes
first and has the focus; **Replace** switches.

What is saved where:

| What | Where |
|---|---|
| The key | Your secret store (see below), as you typed it: a `${env:…}` reference stays a reference |
| The list of connected AIs (no keys) | `~/.config/termocode/ai/providers.json` |
| The model list | `~/.cache/termocode/ai/models.json` (only a cache) |

The card never writes `config.json`. Profile steps use a connected AI with
`provider` and `model` (see [Step fields](ai-profiles.md#step-fields)).

### The chat's provider and model

The chat uses one provider and model of its own. To set them:

1. Press `F1` and run **AI: Configure Provider…**.
2. Pick a provider.
3. Paste your API key and press `Enter`. The key is hidden while you type.
4. Pick a model, or choose **Custom model id…**.
5. A toast says *AI ready*. The status bar shows `✦` and the model name.

Where the key goes depends on the `secret_store` setting. The key prompt
says it: the OS keyring, the encrypted file `secrets.age`, or the plain file
`~/.config/termocode/ai/credentials.json` (mode `0600`, not encrypted). Other
AI settings go in `config.json`.

### Which store is used

termocode keeps secrets in one of three places. The `secret_store` setting
(`config.json`) picks it. The default is `auto`.

| Store | When | What you do |
| --- | --- | --- |
| OS keyring | `auto` uses it when one answers: a desktop with gnome-keyring or the macOS Keychain | Nothing. No prompt from termocode. |
| Encrypted file `secrets.age` | `auto` uses it when there is no keyring: over SSH, without D-Bus, on Termux | A passphrase, once while termocode runs. |
| Plain file `ai/credentials.json` | Only if you set `secret_store` to `plain`, or an old plain file is still there | Nothing. The file is not encrypted. |

`auto` never switches you to the plain file. Set `keyring` or `age` to force
one store; if you force `keyring` and none answers, you get an error, not a
fallback. termocode remembers its choice in `secrets.json`, so it does not
test the keyring on every start. The encrypted-file passphrase has at least 8
characters. Under 12 it still works, with a warning. You can not read it back
if you forget it: set your keys again.

`secret_lock_minutes` (in `config.json`, default `0` = never) locks the
encrypted file after that many minutes without a secret use; a toast says
*Secrets locked after N min idle*, and the next need asks the passphrase
again. Each use starts the count again. It is read when you unlock. The
keyring and the plain file have no passphrase, so it does nothing there.
**AI: Lock Secrets** locks by hand any time. Both settings are for you
only; the AI can not change them.

**AI: Change Secrets Passphrase…** (also on the store screen) asks the
current passphrase, then the new one twice. termocode writes the new file
next to the old one, checks that the new passphrase opens it, and only then
replaces `secrets.age`. If anything fails, the old file and the old
passphrase stay. Another termocode window that has the file open asks for
the new passphrase on its next save.

Over SSH, see [Secrets on a server](remote-ssh.md#secrets-on-a-server).

### Type a reference instead of a secret

Every secret prompt (the AI key, an MCP secret or token, a database
password, an SSH password or key passphrase, a registry token) accepts a
reference instead of the value. The hint under the prompt says so:
`value, or ${env:NAME} / ${cmd:…} / ${dotenv:NAME}`.

| Type | termocode reads |
| --- | --- |
| `${env:NAME}` | the environment variable of termocode |
| `${dotenv:NAME}` | `NAME` in the `.env` of this workspace (never a parent folder) |
| `${cmd:pass show termocode/anthropic}` | the first output line of the command |

termocode saves the text you typed, never the value it points at, and reads
it each time it is needed. A reference that is not valid (`${env:}`,
`${foo:x}`, a missing `}`) is refused in the prompt with a message, and
nothing is saved. The first time a command runs in a session, a toast shows
`Running: <the command>` (never its output), so a changed command in your
own files is noticed. A command runs only from your own configuration, never
from a file in a project: `.termocode/databases.json` may name
`${secret:NAME}` and `${dotenv:NAME}`, `.termocode/mcp.json` may name
`${secret:NAME}`, but a `${cmd:…}` in either is refused with a warning and
is not run.

### See and forget saved secrets

Run **AI: Secret Store…** from `F1`. The screen shows the backend, whether
it is locked, and one row per saved name with `value` or its reference
(such as `${env:OPENAI_API_KEY}`). It never shows a secret value.

- `Enter` on a name asks to forget it (a locked encrypted file asks for its
  passphrase first).
- **Lock now** / **Unlock…** locks the encrypted file or asks for its
  passphrase.
- **Change passphrase…** (encrypted file only) is **AI: Change Secrets
  Passphrase…**.
- **Switch backend…** moves every secret to another store: the OS keyring
  (offered only when one answers; the card says why not), the encrypted
  file (a new passphrase twice, or the passphrase of an existing
  `secrets.age`) or the plain file (after a second card that says it is
  not encrypted). termocode copies every name (references stay
  references), reads each one back, saves the new store in `secrets.json`
  and in `secret_store`, and only then removes the old copies. If a step
  fails, the old store stays in use and nothing is removed.
- `Esc` closes the screen.

With the keyring or the encrypted file, **Open Config: AI Credentials**
opens this screen, because there is no file to edit. The AI can not open it.

### Move secrets out of the plain file

Older versions kept every secret in the plain file
`~/.config/termocode/ai/credentials.json`. If you still have it, termocode
asks once at start: **Move your secrets to a safer store?** The card counts
what it found (for example `2 AI keys · Claude sign-in · 1 MCP secret`). It
never shows a value.

- **OS keyring** (offered when one answers): no passphrase.
- **Encrypted file**: you type a new passphrase twice. The secrets go to
  `secrets.age`.
- **Keep plain**: sets `secret_store: plain`. The card does not come back.
- `Esc`: ask later. After three times the card stops, and the Welcome page
  shows one line: `F1 → AI: Move Secrets to a Safer Store…`.

The card says why the keyring is not offered, for example "no D-Bus
session" over SSH.

What happens:

1. termocode copies every secret to the new store.
2. It reads each one back and compares it.
3. Then it asks on a card: **Keep for now** (first, the default) or
   **Delete the plain file**. Only on Delete it checks the copy once more,
   overwrites `credentials.json` with zeros, deletes it and writes the empty
   marker `ai/credentials.json.migrated`.

A toast says *Secrets moved · plain file kept*. Until you delete it, the
plain file stays readable on disk. If a step fails, `credentials.json`
stays as it was and termocode keeps using it. If you keep the file (or
termocode stops in the middle), the next start asks again once the new store
is open (the encrypted file after you unlock it). Esc and Enter keep the
file. **AI: Move Secrets to a Safer Store…** offers the delete again any
time.

While the marker exists, termocode does not ask again. Run
**AI: Move Secrets to a Safer Store…** from `F1` any time the plain file is
in use. The AI can not run it.

!!! note "Older termocode versions"
    An older termocode does not know the new store. After the move it finds
    no `credentials.json` and shows AI as not configured.

To change the model later, run **AI: Pick Model…**. To turn AI off, run
**AI: Configure Provider…** and pick **None**.

## Option B: use environment variables

Environment variables win over the files. This is handy on servers and in CI.

```sh
export ANTHROPIC_API_KEY=sk-ant-...      # picks Anthropic
# or
export OPENAI_API_KEY=sk-...             # picks OpenAI
# or
export OPENROUTER_API_KEY=sk-or-...
```

If no provider is set, an Anthropic key is used first, then an OpenAI key.

You can also set everything by hand:

| Variable | Meaning |
|---|---|
| `TERMOCODE_AI_PROVIDER` | `anthropic`, `openai`, `openrouter`, `ollama`, `lmstudio`, `openai-compatible`, or `none` |
| `TERMOCODE_AI_MODEL` | model id |
| `TERMOCODE_AI_BASE_URL` | API base URL |
| `TERMOCODE_AI_API_KEY` | API key for the chosen provider |
| `OLLAMA_HOST` | Ollama address, e.g. `192.168.1.5:11434` |

## Option C: local models with Ollama

No key, no cloud. Your code stays on your machine.

1. Install [Ollama](https://ollama.com/) and pull a coding model:

    ```sh
    ollama pull qwen2.5-coder:7b
    ```

2. In termocode, run **AI: Configure Provider…** and pick **Ollama (local)**.
3. Pick the model.

termocode talks to `http://localhost:11434/v1` by default. LM Studio works
the same way on `http://localhost:1234/v1`.

!!! tip "Small models for ghost text"
    Inline completion runs on every pause in typing. A small model such as
    `qwen2.5-coder:1.5b` feels faster.

## Claude sign-in (opt-in)

**AI: Sign in with Claude (OAuth, opt-in)…** uses your Claude subscription
instead of an API key.

!!! warning
    This uses the Claude Code OAuth client. Anthropic's terms may not allow
    that for third-party apps. **An Anthropic API key is the supported option.**
    termocode asks you to confirm before it starts.

The steps: termocode opens the sign-in page in your browser (and copies the
link). You sign in, copy the code, and paste it back. Run
**AI: Sign out of Claude** to remove the tokens.

## Default models

| Provider | Chat model | Ghost-text model |
|---|---|---|
| Anthropic / Claude | `claude-opus-5-5` | `claude-haiku-4-5` |
| OpenAI | `gpt-4.1-mini` | `gpt-4.1-mini` |
| OpenRouter | `anthropic/claude-sonnet-5-5` | same as chat |
| Ollama | `qwen2.5-coder:7b` | same as chat |

## Prompt cache (Claude)

With an Anthropic key or a Claude sign-in, termocode marks the tools, the
system prompt and the chat so far for Claude's prompt cache. The next step
of the same chat reads that part from the cache, at a small part of the
input price. The turn's usage line shows these tokens as "cached".

Set `ai_prompt_cache` in `config.json`:

| Value | Meaning |
|---|---|
| `5m` (default) | The cache lasts 5 minutes after its last use. |
| `1h` | The cache lasts one hour. Writing it costs more; use it when you often pause for more than 5 minutes. |
| `off` | Nothing is cached. |

## Keys at a glance

| Key / command | Action |
|---|---|
| **AI: Add AI…** | Connect an AI: a sign-in with your plan, a key, a local server or a server URL |
| **AI: Configure Provider…** | Pick the chat's provider and enter a key |
| **AI: Pick Model…** | Change the model |
| **AI: Secret Store…** | List and forget saved secrets, lock or unlock, change the passphrase, switch backend |
| **AI: Change Secrets Passphrase…** | Re-encrypt `secrets.age` with a new passphrase |
| **AI: Move Secrets to a Safer Store…** | Move `credentials.json` to the OS keyring or the encrypted file |
| **Open Config: AI Credentials** | Open `credentials.json` (the secret store screen for the keyring or encrypted file) |
| Click the `✦` badge in the status bar | Configure (when AI is off) |

Next: [Use AI](ai-use.md).

## Troubleshooting

**"Wrong passphrase" three times**
:   termocode stays locked and the AI shows as not configured. Run
    **AI: Unlock Secrets** from `F1` to try again. If you forgot the
    passphrase, there is no way to read `secrets.age`. Delete it, then set
    your keys again.

**"Secret file damaged"**
:   `secrets.age` can not be read (cut off or changed). The card names the
    file. Restore it from a backup, or press **Start a new encrypted file**:
    termocode keeps the old file as `secrets.age.broken-<date>` (it never
    deletes it), asks a new passphrase and starts an empty file. Set your
    keys again; references stay.

**"OS keyring is not available"**
:   Only with `secret_store: keyring`: no keyring answers here, for example
    over SSH or with the desktop locked. Log in on the desktop, or set
    `secret_store` to `auto` or `age`. Keys that lived in the keyring stay
    there; set them again for the new store.

**"Keyring not reachable here"**
:   The keyring worked when termocode chose it, but does not answer now,
    for example over SSH to the same machine. The toast comes once per
    run. On the desktop, open **AI: Secret Store…** → **Switch backend…**
    → **Encrypted file** to copy the keys there.

**A reference does not work**
:   The AI shows the reference text, never a value. Check the variable
    (`${env:NAME}`), the `.env` of the workspace (`${dotenv:NAME}`), or run
    the command in your own shell (`${cmd:…}`).

**Every AI command says "configure"**
:   No provider is set, or the key is empty. Run **AI: Configure Provider…**.

**I added an AI, but the chat does not use it**
:   **AI: Add AI…** connects an AI for profile steps; the chat keeps its own
    provider and model. Pick them with **AI: Configure Provider…** (see
    [The chat's provider and model](#the-chats-provider-and-model)).

**A step says "Claude Code is not installed" or "is not logged in"**
:   Run **AI: Add AI…**, pick the AI and its sign-in (or press **Fix** on
    the step in the profile form). The card installs the CLI and signs you
    in. In Itermoc, a signed-out AI has a **Sign in** pill that opens the
    same card at the sign-in.

**`401` or "invalid API key"**
:   Check the key. An environment variable wins over the saved key — run
    `env | grep API_KEY` to check for an old one.

**Ollama: "connection refused"**
:   Start Ollama (`ollama serve`). If it runs on another machine, set
    `OLLAMA_HOST`.

**Ollama: "model not found"**
:   Pull it first: `ollama pull <model>`. The model id must match exactly.

**Where are errors?**
:   Run **Help: Show Error Log**. API keys are removed from AI errors before
    they are logged.
