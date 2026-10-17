# ADR 0012 — HTTP client engine in Go; `.http` grammar shared with mobocode

Status: **Accepted** (2026-10-17)

## Context
Developers test APIs from the editor: VS Code REST Client and the
JetBrains HTTP Client made `.http` files the common format. termocode had
a sample Lua extension (`extensions/rest-client`) that sent requests with
`curl`: curl is not on Termux by default nor in the hermetic test PATH, it
had a fixed 30 s timeout, no cancel, no environments, no history and
nothing the AI agent could use safely. mobocode (the phone app) already
parses `.http` files (`lib/core/rest/http_file_parser.dart`).

## Decision
- The engine is the Go package `internal/httpclient` over `net/http`: no
  child process, so it works in Termux, over SSH and in CI. Timeouts,
  cancel, redirects, cookie jar, proxy env, body cap and timing are all
  in-process; tests use `httptest` only.
- The parser and serializer are byte-equal with mobocode's Dart code (its
  tests are ported as Go table tests; shared fixtures in
  `internal/httpclient/testdata`). termocode accepts a harmless superset
  (`//` comments, `@var = value`, a bare URL as GET, `# @directives`) and
  writes only the common subset.
- The request is the `.http` file in nvim (lens by a decoration
  provider); the answer is an nvim scratch page (like Find Results), so
  big bodies get search, folds and yank for free.
- AI access is through typed `http_*` tools gated per host and method,
  with a dial-time IP check against DNS rebinding and redaction of every
  secret before the model, the audit log or history sees it.
- The curl-based sample extension is retired.

## Consequences
- No curl dependency; one code path for the user, the AI and tests.
- mobocode must learn the superset (HC-16) before files with `@var` lines
  read the same there; until then the docs advise env files.
- HTTP/2, proxies and TLS follow Go's `net/http` behaviour, not curl's.
