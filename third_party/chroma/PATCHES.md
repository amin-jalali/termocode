# Chroma v2.23.1, patched for termocode

This is github.com/alecthomas/chroma/v2 v2.23.1 (MIT, see COPYING),
used through a `replace` in termocode's go.mod. Only what termocode
builds is here: the root package and `lexers` with its embedded lexer
files. Tests, test data, `formatters`, `styles`, `quick` and `cmd` are
left out.

Changed files are marked with "termocode patch".
