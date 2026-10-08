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

The key is saved in `~/.config/termocode/ai/credentials.json` with file mode
`0600` (only you can read it). Other AI settings go in `config.json`.

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
| **Open Config: AI Credentials** | Open `credentials.json` |
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
