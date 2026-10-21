# Set up AI

AI in termocode is optional. Until you set it up, every AI command only shows
a short "configure" message. Nothing is sent anywhere.

You can use:

| Provider | Needs |
|---|---|
| Anthropic | an API key |
| OpenAI | an API key |
| OpenRouter | an API key |
| Ollama (local) | Ollama running on your machine |
| LM Studio (local) | LM Studio's server running |
| OpenAI-compatible | a base URL (ending in `/v1`) and maybe a key |
| Claude (OAuth sign-in) | a Claude account — opt-in, see the warning below |

## Option A: set it up in the app

1. Press `F1` and run **AI: Configure Provider…**.
2. Pick a provider.
3. Paste your API key and press `Enter`. The key is hidden while you type.
4. Pick a model, or choose **Custom model id…**.
5. A toast says *AI ready*. The status bar shows `✦` and the model name.

Where the key goes depends on the `secret_store` setting. The key prompt
says it: the OS keyring, the encrypted file `secrets.age`, or the plain file
`~/.config/termocode/ai/credentials.json` (mode `0600`, not encrypted). Other
AI settings go in `config.json`.

### See and forget saved secrets

Run **AI: Secret Store…** from `F1`. The screen shows the backend, whether
it is locked, and one row per saved name with `value` or its reference
(such as `${env:OPENAI_API_KEY}`). It never shows a secret value.

- `Enter` on a name asks to forget it (a locked encrypted file asks for its
  passphrase first).
- **Lock now** / **Unlock…** locks the encrypted file or asks for its
  passphrase.
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
3. Only then it overwrites `credentials.json` with zeros, deletes it and
   writes the empty marker `ai/credentials.json.migrated`.

A toast says *Secrets moved · credentials.json removed*. If a step fails,
`credentials.json` stays as it was and termocode keeps using it. If termocode
stops in the middle, the next start checks the new store and finishes the
delete without asking (the encrypted file after you unlock it).

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

## Keys at a glance

| Key / command | Action |
|---|---|
| **AI: Configure Provider…** | Pick a provider and enter a key |
| **AI: Pick Model…** | Change the model |
| **AI: Secret Store…** | List and forget saved secrets, lock or unlock |
| **AI: Move Secrets to a Safer Store…** | Move `credentials.json` to the OS keyring or the encrypted file |
| **Open Config: AI Credentials** | Open `credentials.json` (the secret store screen for the keyring or encrypted file) |
| Click the `✦` badge in the status bar | Configure (when AI is off) |

Next: [Use AI](ai-use.md).

## Troubleshooting

**Every AI command says "configure"**
:   No provider is set, or the key is empty. Run **AI: Configure Provider…**.

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
