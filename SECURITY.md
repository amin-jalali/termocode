# Security

## Report a problem

Please do not open a public issue for a security problem. Report it
privately on GitHub: **Security → Report a vulnerability**
(<https://github.com/amin-jalali/termocode/security/advisories/new>).

Say what you found, how to reproduce it, and what an attacker could do.
You will get an answer within a few days. Fixed problems are named in the
release notes, with credit if you want it.

## Supported versions

Only the newest release gets security fixes.

## What termocode keeps and where

- **AI keys** are stored in `~/.config/termocode/ai/credentials.json`
  with mode 0600 (or read from environment variables). They are sent only
  to the provider you chose, and they are removed from error messages and
  logs.
- **Sessions** (for SSH reconnects) listen on a unix socket only, never on
  the network. The socket folder is private to your user (mode 0700), and
  the session checks that every client runs as your user.
- **Unsaved files** are backed up under `~/.local/state/termocode/backup/`
  so they survive a crash.
- Text from AI models, tools and extensions is cleaned before it reaches
  the screen: terminal escape sequences and bidi override characters are
  removed.
