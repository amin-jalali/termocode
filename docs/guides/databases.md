# Browse databases

The **Databases** view lists configured connections and SQLite files found near the workspace root. It can connect to PostgreSQL, MySQL or MariaDB, and SQLite. Connections are opened only when you expand them; schema details load as you move down the tree.

## Open the view

Choose **Databases** in the activity bar or run **Databases: Focus** from the command palette. Use `j` and `k` to move, `Enter` to connect or expand a row, `h` to fold a branch, and `R` to reload the list. Use `/` to filter names and `Esc` to return focus to the editor.

Tables show their columns, primary keys, indexes, and foreign keys when expanded. A primary-key column has a `[PK]` marker; selecting it shows “primary key” in the footer. Views, routines, and sequences appear in their own groups when the server exposes them.

## Add and edit connections

Press `a` and choose PostgreSQL, MySQL / MariaDB, SQLite, or a connection URL. The server prompts start with useful defaults. Network connections ask for the host, port, user, and database, followed by a masked password prompt. The app tests the connection before offering a choice to save it to the project or to your user configuration. You can also edit a connection file directly with `e` on a row. Project configuration lives in `.termocode/databases.json`; user connections live in `~/.config/termocode/db/connections.json`.

Passwords are stored through the secret store. A password pasted in a URL is removed from the saved URL and stored as a secret. Project files contain references such as `${secret:db.local}` or `${dotenv:DATABASE_URL}`, never literal passwords. Press `p` on a connection row to set its password. SQLite paths in project configuration must stay inside the workspace.

SQLite uses the installed `sqlite3` command. It needs version 3.33 or newer. If it is missing, the view displays an install command for the current platform.

## Suggestions

SQLite files ending in `.db`, `.sqlite`, or `.sqlite3` are suggested from the workspace root and nearby folders. A connection URL in `.env` can also be surfaced as a suggestion; the value itself is not shown, and the saved connection uses a `${dotenv:NAME}` reference. Press `a` to add a suggestion or `x` to hide it.

## Disconnect

Press `x` on a connection and confirm its name to close all sessions opened for that connection. Expanding another database reuses its session on later visits. Sessions also close when you leave the Databases view, so hidden views do not keep database processes or activity running.

## SQL consoles

Press `n` on a connection to open its console. Consoles live in your user configuration under `db/consoles/<workspace>/<connection>.sql` (mode 0600) and start with a `-- @conn <id>` line. A project `.sql` file can use the same line; without it, the first run asks which connection to use and remembers the answer until you quit.

Press `Ctrl+Alt+R` (or `Ctrl+Enter` in terminals that send it) to run the statement under the cursor. In visual mode the selection runs as typed. **Databases: Run File** runs every statement in order and stops at the first error. A small note at the end of each statement shows the key, the connection and the last result. `Esc` cancels a running query.

A read runs at once. Any other statement asks first, and the card shows the exact SQL (use Up/Down to read a long one). The title says what is special: `DELETE` or `UPDATE` without `WHERE`, `DROP`, `TRUNCATE`, `ALTER`, a production connection, or a read-only connection. There is no "always allow". In a file run, each write gets its own card.

## SQL results

The result opens as the **SQL Result** page, in a split on the right when the editor is at least 120 columns wide. The first rows show the SQL, the connection, the row count and the time. `Tab` switches between Grid, JSON and Messages. Messages shows the full error, the server's notices and the time.

| Key | Action |
| --- | --- |
| `]` / `[` | next / previous page |
| `o` | sort by a column (choose it again to reverse) |
| `f` | show only rows that contain a text |
| `v` or `Enter` | open the cell under the cursor (JSON is pretty-printed); `q` goes back |
| `y` | copy the cell |
| `c` / `C` | copy the loaded rows as CSV / JSON |
| `r` | run again (writes ask again) |
| `gd` | go to the statement |
| `h` | SQL history |
| `Esc` | cancel a running query |
| `q` | close the page |

A result loads at most `db_max_rows` rows (default 10,000); the page says when the limit was reached, so add a `LIMIT` or a `WHERE`. Only one page (`db_page_rows`, default 100) is written to the page at a time. A statement stops after `db_timeout` seconds (default 30). Set these in your user `config.json`.

## SQL history

Press `h` on the result page, `H` in the Databases view, or use **Databases: History…**. The newest run is at the top; `Enter` runs it again (writes ask again). **Clear history…** asks before it deletes. The history is `db/history.jsonl` in your user configuration (mode 0600): passwords are removed, each entry keeps at most 4 KB of SQL, and `db_history_max` (default 500) limits the count.

Nothing polls while you wait: an idle console or result page uses no CPU, and hiding the Databases view cancels a running query and closes its sessions.
