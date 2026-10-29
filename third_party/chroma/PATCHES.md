# Chroma v2.23.1, patched for termocode

This is github.com/alecthomas/chroma/v2 v2.23.1 (MIT, see COPYING),
used through a `replace` in termocode's go.mod. Only what termocode
builds is here: the root package and `lexers` with its embedded lexer
files. Tests, test data, `formatters`, `styles`, `quick` and `cmd` are
left out.

Changed files are marked with "termocode patch".

- `lexers`: the global registry is filled on first use, not at package
  init (`lexers.go`; `dns.go`, `mysql.go` and `zed.go` set their
  analysers through `onFill`). Reading the config of all 268 embedded
  lexers took ~9 ms and 2.5 MB at the start of every termocode process,
  also the session client and server that never highlight anything.
  `GlobalLexerRegistry` is a func here (a var upstream). Lexers keep
  upstream's order: the XML ones first, then the Go ones.
  Test: `lexers/lazy_test.go`.
