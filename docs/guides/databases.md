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

Press `x` on a connection and confirm its name to close all sessions opened for that connection. Expanding another database reuses its session on later visits. Sessions also close when you leave the Databases view, so hidden views do not keep database processes or activity running. A session with an open manual transaction stays open until you commit or roll back (see [Transactions](#transactions)); `x` on its connection first asks **Commit**, **Roll back** or **Cancel**.

## SQL consoles

Press `n` on a connection to open its console. Consoles live in your user configuration under `db/consoles/<workspace>/<connection>.sql` (mode 0600) and start with a `-- @conn <id>` line. A project `.sql` file can use the same line; without it, the first run asks which connection to use and remembers the answer until you quit.

Press `Ctrl+Alt+R` (or `Ctrl+Enter` in terminals that send it) to run the statement under the cursor. In visual mode the selection runs as typed. **Databases: Run File** runs every statement in order. It stops at the first error and asks: **Continue** runs the remaining statements, **Stop** ends the run. A small note at the end of each statement shows the key, the connection and the last result. `Esc` cancels a running query.

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
| `S` | export the rows to a file (CSV, TSV, JSON, SQL INSERTs or Markdown) |
| `r` | run again (writes ask again) |
| `1` … `9` | show result tab 1 … 9 |
| `p` | pin the result tab (again: unpin) |
| `gd` | go to the statement |
| `h` | SQL history |
| `Esc` | cancel a running query |
| `q` | close the page |

A result loads at most `db_max_rows` rows (default 10,000); the page says when the limit was reached, so add a `LIMIT` or a `WHERE`. Only one page (`db_page_rows`, default 100) is written to the page at a time. A statement stops after `db_timeout` seconds (default 30). Set these in your user `config.json`.

## Table data, describe and DDL

Move to a table or view in the tree and press:

| Key | Action |
| --- | --- |
| `o` | open the table's data (also **Databases: Open Table Data**) |
| `d` | describe: columns, types, null, defaults, keys, indexes, foreign keys, triggers |
| `D` | the DDL in a read-only SQL buffer (`q` goes back) |
| `s` | write `SELECT * FROM <table> LIMIT 100;` at the end of the connection's console and run it there |
| `#` | count the rows |
| `i` | import a CSV file into the table (not for views) |
| `y` | copy the quoted table name |

The data page asks the server for one page at a time (`db_page_rows`, default 100) in the table's key order, so a big table stays cheap. `]` and `[` fetch the next and previous page. `o` on the page sorts by a column on the server (again: reverse). `f` asks for a `WHERE` condition, for example `plan = 'pro' AND id > 100`; an empty text shows all rows. A condition is one plain read: a `;`, a comment (`--`, `/* */`, and `#` on MySQL), a dollar quote, unbalanced brackets or a word such as `DELETE` or `INTO` is refused, and nothing is sent. Names of tables and columns are always quoted for the connection's database. The row count is never automatic: `#` counts the rows the current `WHERE` keeps and shows `of N` in the summary.

`t` flips the page to the Transposed view: one row as `column | value` lines, which suits narrow windows and wide tables. `]` and `[` step through the rows (and the pages), `v` opens the value under the cursor, `t` goes back to the grid.

PostgreSQL has no `CREATE TABLE` text to ask for, so its DDL is rebuilt from the catalogs (columns, primary key, indexes, foreign keys) and the buffer says so; it is a reconstruction, not a dump. MySQL / MariaDB and SQLite show the server's own statement.

## Result tabs

Every result has a tab. A new run replaces the tabs that are not pinned; a pinned tab (`p`) and a tab with pending changes stay. When a run has several statements, each statement that returns rows (or fails) gets its own tab. Up to nine tabs are kept. The first row of the page lists them when there is more than one: `1p [2*] 3` means tab 1 is pinned, tab 2 is on the page and has pending changes. Press `1` … `9` to switch.

## Export and import

**Export.** `S` (Shift+s) on a result page (or **Databases: Export Result…**) opens a picker: CSV, TSV, JSON, SQL INSERTs, Markdown table. Then type a file name; the default is `<table>-<date>.<ext>` in the workspace, and a full path works too. If the file exists, a card asks before it is replaced. The rows go into a temporary file in the same folder, and the file you chose appears only when every row is written, so a cancel or an error leaves no half file and keeps the old one.

When the result is only part of the rows (the 10,000-row limit, or one page of a table), a card asks: **Export the loaded rows** or **Export all**. Export all runs the query again, without the row limit, in a read-only session and writes the rows as the server sends them (a page of 1,000 at a time, never all in memory). A progress card shows the count; `Esc` cancels and removes the temporary file. Only a plain `SELECT` can run again, so a write is never repeated; Export all reads committed data, not the changes of your open transaction.

CSV and TSV protect spreadsheets from formula injection: a cell that starts with `=`, `+`, `-`, `@`, a tab or a carriage return gets a leading single quote (`'=SUM(A1)`). A real number such as `-5` keeps its sign. NULL is an empty cell in CSV and TSV, `null` in JSON and `NULL` in Markdown and INSERT files. INSERT files quote names and values for the connection's database (a quote is doubled; MySQL also doubles a backslash) and write binary values in the database's own form. The **Copy** keys `c` / `C` do not add the quote.

**Import.** `i` on a table (or **Databases: Import CSV into Table…**) lists the `.csv` files of the workspace (or asks for a path). A preview card shows what was detected: the encoding (UTF-8, UTF-16 or ISO-8859-1), the delimiter (comma, semicolon, tab or pipe), whether the first row is a header, the row count, the column mapping (file column to table column, matched by name, or by position without a header) and the first rows. **Map columns** changes one table column at a time, and **Header** switches the header guess. Empty cells become NULL.

**Import** reads the file one row at a time and inserts it with bound values in INSERTs of up to 1,000 rows, all in one transaction: if any row fails, everything is rolled back, nothing is written, and a card names the row and line. A progress card shows `N / total rows`; `Esc` cancels and rolls back. A read-only connection never imports, a production connection gets the strong card, and in manual transaction mode the rows join your open transaction (Commit saves them). The AI cannot import.

## Edit rows

You can change the rows of a table's data page, and of a console result that is a plain `SELECT` from one table (columns named as they are in the table, no `DISTINCT`, `JOIN`, `GROUP BY` or `UNION`). The table needs a primary key or a unique key, and the result must contain its columns: each change finds its row by the full key. Otherwise the page says **read-only** and why (for example `read-only: no key`); `e` then shows the reason. Views, AI results, table descriptions and read-only connections cannot be edited.

| Key | Action |
| --- | --- |
| `e` | edit the cell under the cursor in a one-line prompt (`\N` means NULL) |
| `E` | edit the value in a buffer (for long or multi-line values); `:w` keeps it, `q` leaves without a change |
| `+` | add a new row; its cells show `<default>` until you set them with `e` |
| `-` | mark the row for deletion (again: keep it); on a new row: remove it |
| `u` | undo the change of the cell under the cursor, else of the row; on a row without changes it asks to undo all changes |
| `w` | submit the changes |

Changes stay on the page until you submit them. The left margin marks each row: `*` changed, `+` new, `-` to be deleted, and the page says how many changes wait. Anything that loads the rows again (another data page, a sort or a filter on a data page, `r`) asks **Keep** or **Discard** first; switching tabs or closing the page keeps the changes.

`w` shows a card with the exact SQL, one statement per row, with the values written out. **Copy SQL** copies it. **Submit** runs the statements as one transaction: values are sent as bound parameters (never pasted into the SQL), names are quoted for the connection's database, and an `UPDATE` also checks that the old value is still there. Each statement must change exactly one row; if one changes no row (someone else changed or deleted it) or fails, everything is rolled back, your changes stay on the page, and a card offers to reload the rows. After a submit the page shows the new values at once and then reloads them from the server; **Messages** lists the statements that ran. A production connection gets the stronger card with **Cancel** first. There is no "always allow", and the AI cannot submit your changes.

## Transactions

Each connection starts in auto-commit mode: every statement is saved at once. Press `t` on a connection (or use **Databases: Transaction Mode…**) and choose **Manual** to group your work: the first statement begins a transaction, the next ones join it, and nothing is saved until you commit.

- `K` on the result page or **Databases: Commit** saves the changes; `Z` or **Databases: Rollback** drops them.
- The status bar and the result page show `manual · 2 uncommitted` while changes wait. Typed `BEGIN`, `COMMIT` or `ROLLBACK` statements are not sent in manual mode; use `K` and `Z`.
- Submitted row changes join the open transaction; data pages read inside it, so they show your own changes.
- Quitting, disconnecting, or switching back to auto-commit with changes waiting asks **Commit**, **Roll back** or **Cancel**.

## Completion

In a SQL buffer, `Ctrl+X Ctrl+O` (and the pop-up that opens while you type) offers keywords, tables, views, routines, sequences and schemas from the tree you have already opened. After `alias.` or `table.` it offers that table's columns; the alias comes from the statement at the cursor (`FROM users u`, `JOIN orders AS o`). The first time a table's columns are needed they are read once and the pop-up opens again. Completion reads a cached copy: it never asks the server while you type.

## SQL history

Press `h` on the result page, `H` in the Databases view, or use **Databases: History…**. The newest run is at the top; `Enter` runs it again (writes ask again). **Clear history…** asks before it deletes. The history is `db/history.jsonl` in your user configuration (mode 0600): passwords are removed, each entry keeps at most 4 KB of SQL, and `db_history_max` (default 500) limits the count.

Nothing polls while you wait: an idle console or result page uses no CPU. Hiding the Databases view (another view, a hidden sidebar or zen mode) cancels a running query and closes its sessions, except a session with an open manual transaction. The command palette and other dialogs do not.

## AI

The AI chat has database tools: `db_connections`, `db_schema`, `db_ddl`, `db_query`, `db_explain` and `db_history`. The system prompt mentions them only when the workspace has connections.

- **Reads run at once.** Schema, DDL, plans and a read (`SELECT`, `SHOW`, `EXPLAIN` without `ANALYZE` …) run in a separate read-only session: `BEGIN READ ONLY` with a statement timeout on PostgreSQL, `START TRANSACTION READ ONLY` on MySQL / MariaDB, and `sqlite3 -readonly` for SQLite. The server or the file enforces it, not a text check. SQLite needs `sqlite3` 3.37 or newer for this.
- **Every other statement asks.** The card shows the connection and the exact SQL (Up/Down scrolls a long one). **Always allow** for `db:<conn>/write` lets later writes on that connection run without asking; you can turn it off again in **AI: Agent Auto-Approve…**.
- **Production connections** (`"environment": "production"`) always ask before a write, with no "Always allow". **Read-only connections** (`"readOnly": true`) refuse AI writes.
- **Own sessions.** Each AI call opens its own session and closes it after. The AI never runs on your session and never joins your transaction. It cannot add or change connections or set passwords.
- **What you see.** Each `db_query` and `db_explain` run opens the SQL Result page marked **by AI** and adds an entry to the SQL history. Every call is in the AI audit log.
- **What the model sees.** Passwords never reach it. Server text is cleaned, cells are cut at 80 characters, and one answer is at most `db_ai_max_kb` KB (default 32); `max_rows` (default 100, max 1000) limits rows.
