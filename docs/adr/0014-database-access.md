# 0014 · Database access through drivers and sidecars

**Status:** Accepted  
**Date:** 2026-10-19

## Context

Database access needs to work on desktop Linux, macOS, Windows, Android and over SSH. SQLite should use the user's installed command line tool so the application does not embed a second engine or require cgo. Passwords must stay out of project configuration, and database work must not consume resources while the view is hidden.

## Decision

Use pure Go PostgreSQL and MySQL drivers through `database/sql`. Use the user's `sqlite3` command as a sidecar, with JSON output and a minimum supported version of 3.33. Use read-only enforcement provided by the server or SQLite's read-only mode. Database results and schema information are owned by `internal/dbclient`; the user interface opens sessions only when requested and closes them when the view is hidden.

The first visible phase includes the Databases sidebar, connection configuration, connection testing, and lazy schema browsing. Passwords are stored through `internal/secrets`; project configuration can contain references but not literal passwords. Network drivers, SQLite sidecars, and future tunnel support are selected to keep the Android build free of cgo.

## Consequences

SQLite requires `sqlite3` 3.33 or newer to browse. The Databases view can show an install hint when the command is missing or too old. No query console or result page is part of the initial browsing phase. The drivers and command-line output handling need focused tests as their supported versions change.

## Alternatives considered

- Embedding a cgo or pure-Go SQLite engine would increase the binary and create a second engine with different behavior.
- Starting an external local tunnel process would require a listening port and additional process management.
- Running a background connection monitor would violate the zero-idle-work requirement.
