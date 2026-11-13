# Exasol Go driver v1.2.0, patched for termocode

This is github.com/exasol/exasol-driver-go v1.2.0 (MIT, see LICENSE),
used through a `replace` in termocode's go.mod and e2e/go.mod. Only the
non-test Go files, go.mod, go.sum and LICENSE are kept; `examples/` is
left out.

Changed files are marked with "termocode patch".

Lazy patterns (startup):

- `pkg/connection/local_parquet_import.go`: `regularIdentifier` and
  `illegalPathCharacters` are compiled on first use (`sync.OnceValue`),
  not in package init. The identifier pattern's Unicode classes took
  ~3 ms and 5 MB in the init of every termocode process, needed or not
  (they are used only by the Parquet import), and pushed
  startup_first_frame over its 30 ms budget (perf budgets job).

Drop this copy once upstream compiles these patterns lazily.
