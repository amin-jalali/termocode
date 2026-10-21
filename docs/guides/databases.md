# Browse databases

The **Databases** view manages connections to PostgreSQL, MySQL or MariaDB, SQLite, and Redis or Valkey. You can browse the schema, run SQL in a console, read and edit rows, export and import data, and let the AI help with safe rules.

A connection opens only when you expand it. Schema details load as you move down the tree.

## Open the view

Choose **Databases** in the activity bar or run **Databases: Focus** from the command palette. The action `DatabasesFocus` has no default key; you can bind one in `keymap.json`.

Use `j` and `k` to move, `Enter` to connect or expand a row, `h` to fold a branch, `R` to reload the list, and `/` to filter by name. `Esc` returns focus to the editor. `?` shows a short key list.

Tables show their columns, primary keys, indexes and foreign keys when expanded. A primary-key column has a `[PK]` marker. Views, routines and sequences appear in their own groups when the server has them.

## Connections and the JSON files

Connections live in two JSON files. Both allow comments.

| File | Use it for |
| --- | --- |
| `.termocode/databases.json` | Connections you share with the project. Never put a password in it. |
| `~/.config/termocode/db/connections.json` | Your own connections. |

If both files use the same `id`, the project entry wins. Press `e` on a connection to open its file. Press `e` on an empty spot to open the user file. A new file starts from a template with comments.

```jsonc
{
  "connections": [
    {
      "id": "local",
      "driver": "postgres",
      "host": "localhost",
      "port": 5432,
      "user": "app",
      "database": "app",
      "password": "${secret:db.local}",
      "environment": "development"
    }
  ]
}
```

| Key | Meaning |
| --- | --- |
| `id` | Required. Lower-case letters, digits, `_` and `-`. |
| `name` | Label in the tree. |
| `driver` | `postgres`, `mysql` (also `mariadb`), `sqlite` or `redis`. |
| `host`, `port`, `user`, `database` | Network connections. The port defaults to 5432, 3306 or 6379. |
| `path` | SQLite file. In a project file it must stay inside the workspace. |
| `db` | Redis database index. |
| `url` | A connection URL instead of the single fields. |
| `params` | Extra driver parameters. |
| `password` | A reference, never the text (see [Passwords](#passwords-and-the-secret-store)). |
| `environment` | `development` (default), or `production` for the stricter cards. |
| `readOnly` | `true` makes the AI refuse writes. Your own writes ask with a warning. |
| `ssh`, `ssl`, `tls` | See [SSH tunnels](#ssh-tunnels) and [SSL and TLS](#ssl-and-tls). |

A row with a problem shows the reason, for example `literal password in project config`, `duplicate connection id` or `sqlite path is outside the workspace`. Fix the file and press `R`.

## Suggestions

Termocode suggests connections it finds. A suggestion row has no password and does nothing until you add it.

- SQLite files ending in `.db`, `.sqlite` or `.sqlite3`, up to two folders below the workspace root.
- `DATABASE_URL`, `REDIS_URL` or `MYSQL_URL` in the workspace `.env`. The value is not shown. The saved connection uses a `${dotenv:NAME}` reference.
- PostgreSQL, MySQL, MariaDB, Redis and Valkey containers and compose services. The Databases view reads the container list itself when it opens and when you press `R`. You do not need to open Services first.
- A server on this computer that is not in Docker. When the view opens and on `R`, Termocode tries one quick connection to `127.0.0.1` on ports 5432, 3306 and 6379. A port that answers shows as **PostgreSQL on localhost:5432**, **MySQL on localhost:3306** or **Redis on localhost:6379**. Ports that a saved connection or a container already uses are skipped. No other computer is ever tried.

Press `a` on a suggestion (or `Enter`) to add it. Press `x` to hide it. A hidden suggestion stays hidden.

With no connections yet, the view lists the suggestions and offers **a add connection** and **Add remote… (SSH)**. **Add remote…** asks for the server type, then the usual connection steps, then the SSH tunnel steps, then runs the test.

## Mouse

- Click a row to select it. Double-click a connection to connect or expand it, a folder to fold or unfold it, and a table or view to open its data.
- On a suggestion, click to select and double-click to add it.
- Click **a add**, **R refresh** or **/ filter** in the header. In the empty view, click **a add connection** or **Add remote…**.
- Right-click a row for its menu.

## Add a connection

Press `a` and choose PostgreSQL, MySQL / MariaDB, SQLite, Redis / Valkey, or **From a URL**.

- **A server type** asks for the host, port, user and database. Then it asks for a masked password.
- **From a URL** asks you to paste one, for example `postgres://app@localhost:5432/app`. Schemes `postgres`, `postgresql`, `mysql`, `mariadb`, `redis`, `rediss`, `sqlite` and `file` work. A password in the URL is taken out and stored as a secret. If the URL has none, you are asked for it.

Termocode tests the connection first. If the test works, choose **Save to project** (no password in the file) or **Save for me only**. If it fails, a list offers **Set password…**, **Use an SSH tunnel…**, **SSL settings…**, **Test again** or **Cancel**.

Run **Databases: Test Connection** or press `T` on a row to test a saved connection again. The test goes through its tunnel and SSL settings and does not touch an open session.

## Passwords and the secret store

A password is never written to a project file. The file holds a reference:

| Reference | Source |
| --- | --- |
| `${secret:db.local}` | The secret store. This is what the app writes. |
| `${dotenv:NAME}` | A key in the workspace `.env`. |
| `${env:NAME}` | An environment variable of termocode. |
| `${cmd:line}` | The first output line of a command you wrote. |

Press `p` on a connection to set its password. It goes to the secret store as `db.<id>`. The SSH password and key passphrase are stored as `db.<id>.ssh` and `db.<id>.sshkey`.

The secret store uses your OS keyring when one is available. Otherwise it uses an encrypted file, or a plain file with mode 0600 (`ai/credentials.json`). See [Security notes](#security-notes).

A project file with a literal password is refused: the row shows a problem. A user file with a literal password gives a warning, and the app offers to **Move password** to the secret store before it connects.

SQLite needs no password.

## SSH tunnels

A connection can go through an SSH server (a bastion), and through jump hosts before it. Run **Databases: Add SSH Tunnel…** on a row, or choose **Use an SSH tunnel…** after a failed test while adding a connection. The prompts ask for the SSH server, port, user and how to log in: `ssh-agent`, a key file, or a password. The tunnel runs inside termocode and opens no local port. The database host is resolved by the SSH server, so names that only exist behind the bastion work.

```jsonc
"ssh": { "host": "bastion.example.com", "user": "deploy", "auth": "agent", "jump": ["ops@gw.example.com"] },
"ssl": { "mode": "verify-full", "ca": "~/.certs/staging-ca.pem" }
```

- `ssh.host` may be an alias from `~/.ssh/config`. termocode reads `HostName`, `Port`, `User`, `IdentityFile` and `ProxyJump` for it; values in the connection win. `ProxyCommand` is not supported (it would run a shell command): the row says so.
- Jump hosts: `ProxyJump gw1,user@gw2:2222` in `~/.ssh/config`, or `"jump": ["user@gw1.example.com", "gw2:2222"]` in the `ssh` object (it wins over `ProxyJump`). Each jump host may be an alias too; its own `ProxyJump` comes before it. termocode connects to the first jump host, then to each next one through the one before, then to the SSH server. `ProxyJump none` means no jump host. A chain may have at most 8 jump hosts, and a chain that loops is refused.
- Every jump host gets the same host key check as the SSH server: an unknown key shows the trust card with that jump host's name and fingerprint, and a changed key is refused. Jump hosts log in with `ssh-agent` or their key files (`IdentityFile`, else `~/.ssh/id_ed25519`, `id_ecdsa`, `id_rsa`). A jump host that asks for a password, or a key with a passphrase that is not in your agent, is refused with the jump host's name; use `ssh-add`. The jump list is edited in the connection file; the prompts set only the SSH server.
- `ssh.auth` is `agent` (keys in your agent), `key` (with `keyFile`) or `password`. The key passphrase and the SSH password are kept in the secret store as `db.<id>.sshkey` and `db.<id>.ssh`, never in a file. A key file that other users can read is refused; run `chmod 600` on it.
- Host keys are checked against `~/.ssh/known_hosts` (or `knownHosts`). An unknown server shows a card with its `SHA256:` fingerprint: **Trust and save to known_hosts** adds it, **Cancel** stops. A server whose key changed is refused with no way around it; check with your admin.
- The tunnel opens with the connection and closes when you disconnect, leave the view, refresh or quit; every jump host closes with it. There is no keepalive: a dropped tunnel shows `SSH tunnel closed · R reconnect` on the next query. When a jump host goes away, the whole chain closes and the message names it.

## SSL and TLS

**Databases: SSL Settings…** sets `ssl.mode`: `disable`, `require` (encrypted, server not checked; with a `ca` it is checked like `verify-ca`), `verify-ca` (the certificate must come from the CA) or `verify-full` (CA and host name; used when an `ssl` object has no `mode`). A CA file is optional (the system's CAs are used without one); `cert` and `key` add a client certificate, and the key file must be readable only by you. Without an `ssl` object termocode does not change the driver default. When a mode needs TLS there is no plain-text retry. `~` in a path is your home; a relative path is in the workspace.

Rows show `ssh` and `ssl` tags after the driver. Test a setting with `T` (see [Add a connection](#add-a-connection)).

For Redis, `"tls": true` or a `rediss://` URL turns TLS on (see [Redis and Valkey](#redis-and-valkey)).

## From Containers and Services

A PostgreSQL, MySQL, MariaDB, Redis or Valkey container or compose service has **Open in Databases** in its right-click menu (in the Containers view) and in the palette (**Containers: Open in Databases**). It opens this view and asks for the password with the host, the published port, the user and the database already filled in. The user and database names come from the compose file (`POSTGRES_USER`, `POSTGRES_DB`, `MYSQL_USER`, `MYSQL_DATABASE`, `MARIADB_USER`, `MARIADB_DATABASE`); passwords are never read from it. The password goes to the secret store, not to a file. If a saved connection already points at the same local port, it is selected instead.

When the container or service behind a saved connection is stopped, the footer says so (`postgres stopped · S start`). Press `S` on the connection, or choose **Start service** in its menu, to start it the same way the Containers view does.

## Disconnect

Press `x` on a connection and choose **Disconnect** to close all its sessions. Sessions also close when you leave the Databases view, so a hidden view keeps no database process. A session with an open manual transaction stays open until you commit or roll back (see [Transactions](#transactions)); `x` then asks **Commit**, **Roll back** or **Cancel**.

## The SQL console

Press `n` on a connection to open its console. Consoles live in your user configuration under `db/consoles/<workspace>/<connection>.sql` (mode 0600) and start with a `-- @conn <id>` line. A project `.sql` file can use the same line; without it, the first run asks which connection to use and remembers the answer until you quit.

Press `Ctrl+Alt+R` (or `Ctrl+Enter` in terminals that send it) to run the statement under the cursor. In visual mode the selection runs as typed. **Databases: Run File** runs every statement in order. It stops at the first error and asks: **Continue** runs the remaining statements, **Stop** ends the run. A small note at the end of each statement shows the key, the connection and the last result. `Esc` cancels a running query.

A read runs at once. Any other statement asks first, and the card shows the exact SQL (use Up/Down to read a long one). The title says what is special: `DELETE` or `UPDATE` without `WHERE`, `DROP`, `TRUNCATE`, `ALTER`, a production connection, or a read-only connection. There is no "always allow". In a file run, each write gets its own card.

## Results

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

## Result tabs

Every result has a tab. A new run replaces the tabs that are not pinned; a pinned tab (`p`) and a tab with pending changes stay. When a run has several statements, each statement that returns rows (or fails) gets its own tab. Up to nine tabs are kept. The first row of the page lists them when there is more than one: `1p [2*] 3` means tab 1 is pinned, tab 2 is on the page and has pending changes. Press `1` … `9` to switch.

## Table views

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

## Completion

In a SQL buffer, `Ctrl+X Ctrl+O` (and the pop-up that opens while you type) offers keywords, tables, views, routines, sequences and schemas from the tree you have already opened. After `alias.` or `table.` it offers that table's columns; the alias comes from the statement at the cursor (`FROM users u`, `JOIN orders AS o`). The first time a table's columns are needed they are read once and the pop-up opens again. Completion reads a cached copy: it never asks the server while you type.

## Edit rows

You can change the rows of a table's data page, and of a console result that is a plain `SELECT` from one table (columns named as they are in the table, no `DISTINCT`, `JOIN`, `GROUP BY` or `UNION`). The table needs a primary key or a unique key, and the result must contain its columns: each change finds its row by the full key. A SQLite table with neither is edited by its hidden `rowid` on the data page (not in a console result); a table that has columns named `rowid`, `_rowid_` and `oid` stays read-only. Otherwise the page says **read-only** and why (for example `read-only: no key`); `e` then shows the reason. Views, AI results, table descriptions and read-only connections cannot be edited.

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

## Export and import

**Export.** `S` (Shift+s) on a result page (or **Databases: Export Result…**) opens a picker: CSV, TSV, JSON, SQL INSERTs, Markdown table. Then type a file name; the default is `<table>-<date>.<ext>` in the workspace, and a full path works too. If the file exists, a card asks before it is replaced. The rows go into a temporary file in the same folder, and the file you chose appears only when every row is written, so a cancel or an error leaves no half file and keeps the old one.

When the result is only part of the rows (the 10,000-row limit, or one page of a table), a card asks: **Export the loaded rows** or **Export all**. Export all runs the query again, without the row limit, in a read-only session and writes the rows as the server sends them (a page of 1,000 at a time, never all in memory). A progress card shows the count; `Esc` cancels and removes the temporary file. Only a plain `SELECT` can run again, so a write is never repeated; Export all reads committed data, not the changes of your open transaction.

CSV and TSV protect spreadsheets from formula injection: a cell that starts with `=`, `+`, `-`, `@`, a tab or a carriage return gets a leading single quote (`'=SUM(A1)`). A real number such as `-5` keeps its sign. NULL is an empty cell in CSV and TSV, `null` in JSON and `NULL` in Markdown and INSERT files. INSERT files quote names and values for the connection's database (a quote is doubled; MySQL also doubles a backslash) and write binary values in the database's own form. The **Copy** keys `c` / `C` do not add the quote.

**Import.** `i` on a table (or **Databases: Import CSV into Table…**) lists the `.csv` files of the workspace (or asks for a path). A preview card shows what was detected: the encoding (UTF-8, UTF-16 or ISO-8859-1), the delimiter (comma, semicolon, tab or pipe), whether the first row is a header, the row count, the column mapping (file column to table column, matched by name, or by position without a header) and the first rows. **Map columns** changes one table column at a time, and **Header** switches the header guess. Empty cells become NULL.

**Import** reads the file one row at a time and inserts it with bound values in INSERTs of up to 1,000 rows, all in one transaction: if any row fails, everything is rolled back, nothing is written, and a card names the row and line. A progress card shows `N / total rows`; `Esc` cancels and rolls back. A read-only connection never imports, a production connection gets the strong card, and in manual transaction mode the rows join your open transaction (Commit saves them). The AI cannot import.

## EXPLAIN and ANALYZE

**Explain.** In a SQL console press `\x` (leader, then `x`) on a statement, or use **Databases: Explain Statement**. On a result page `x` explains the statement that made the result. The plan opens on the result page as the **Plan** view: one line per step, indented, with the cost and row estimates. The step that takes most of the cost (at least 40 % of the whole plan, without its children) is marked with `!` and `<- costly` and coloured. Details such as `Filter:` and `Hash Cond:` sit under their step. Folds follow the indent: `za` folds a step, `zR` opens all. `y` copies the plan, `Tab` shows the server's raw answer, `r` plans again.

A plain plan never runs the statement. It uses `EXPLAIN (FORMAT JSON)` on PostgreSQL, `EXPLAIN FORMAT=JSON` on MySQL / MariaDB (the table form on a server without JSON), and `EXPLAIN QUERY PLAN` on SQLite, always in the read-only session.

**Explain Analyze.** `\X` in a console, `X` on a result page, or **Databases: Explain Analyze Statement**. This RUNS the statement and shows the real time and rows, so a card comes first and says so. A read runs in the read-only session. Any other statement runs inside a transaction that is always rolled back, and the card says so; if the rollback fails, the page shows an error. On a production connection the card is the strong one. A read-only connection analyzes reads only. DDL (`CREATE`, `DROP` …) is not offered, because it cannot be rolled back; with a manual transaction open a write is not analyzed either. SQLite has no `ANALYZE`: it shows the plan and never runs the statement.

## Compare schemas

**Databases: Compare Schemas…** compares two schemas of connected databases. Connect both first (`Enter` on each connection). Then pick two schemas:

1. The **source**: the model, the schema that is right (for example your local database).
2. The **target**: the schema to change (for example staging).

Both must be the same database type: PostgreSQL with PostgreSQL, MySQL / MariaDB with MySQL / MariaDB, SQLite with SQLite. The target list marks the other types, and picking one only shows a message. Two schemas of the same connection work too.

termocode reads both schemas in read-only sessions and writes each one as clean SQL: objects sorted by name, names quoted for the database, no schema names. The diff card shows one file per kind: `tables.sql` (columns, primary keys, unique keys, foreign keys and, on PostgreSQL, MySQL 8.0.16+ and MariaDB, checks), `views.sql`, `routines.sql` (not for SQLite) and `indexes.sql`. The left side is the target, the right side is the source, so the green lines are what the target is missing. A column order that differs is not a difference. `Tab` moves to the next file, `n` / `p` to the next change, `Esc` closes the card.

**Copy migration script** (`y`) copies a script that makes the target match the source. **Open script** (`o`) opens it in a read-only SQL buffer. termocode never runs this script: you read it, change it if needed, and run it yourself. The script does the basic steps: create and drop tables, add and drop columns, drop and create indexes and views. Other differences (a changed column type, a changed key, a routine) are lines that start with `-- REVIEW:`, for you to do by hand. The top of the script says which schema it changes and lists every destructive statement (`DROP TABLE`, `DROP COLUMN`, `DROP INDEX`, `DROP VIEW`); each one also has a `-- DESTRUCTIVE` line above it, because it deletes objects or data.

Very large schemas are compared up to 2,000 tables; the card and the script say so when a schema is bigger.

## SQL history

Press `h` on the result page, `H` in the Databases view, or use **Databases: History…**. The newest run is at the top; `Enter` runs it again (writes ask again). **Clear history…** asks before it deletes. The history is `db/history.jsonl` in your user configuration (mode 0600): passwords are removed, each entry keeps at most 4 KB of SQL, and `db_history_max` (default 500) limits the count.

Nothing polls while you wait: an idle console or result page uses no CPU. Hiding the Databases view (another view, a hidden sidebar or zen mode) cancels a running query and closes its sessions, except a session with an open manual transaction. The command palette and other dialogs do not.

## Redis and Valkey

A Redis or Valkey connection (`"driver": "redis"`) takes a host, a port (6379 by default), an optional ACL `user`, a database index `db`, and a password from the secret store. Use `"tls": true` or a `rediss://` URL for TLS; `"ssl": {"ca": "…", "cert": "…", "key": "…"}` adds a CA and a client certificate. Containers and compose services built from `redis` or `valkey` images, and `REDIS_URL` in `.env`, are suggested.

Expanding a connection lists its databases (`db0 (1234 keys)`). Expanding a database lists its keys with `SCAN`, grouped into namespaces by `db_redis_separator` (`:` by default): `session:ab12` sits under `session:`. The app never sends `KEYS`. One row loads at most `db_redis_max_keys` keys (10000); past that the row says so, and opening a namespace scans only its keys. Each key shows its type: `"` string, `{` hash, `[` list, `(` set, `⇅` sorted set, `≋` stream (ASCII: `s h l S z x`).

`Enter` on a key opens its value page: the type, length, TTL and memory, then one page of the value (`]` and `[` move between pages; `db_page_rows` items per page). A whole value is never loaded: strings show their first 64 KB, other types load page by page. Values are shown safely: bytes that are not text and control characters appear as `\xHH`.

On the value page, `e` edits the value or the field under the cursor, `+` adds a field, item, member or entry, `-` deletes the one under the cursor, `t` sets the TTL (seconds or `30m`, `2h`, `1d`; empty removes it) and `D` deletes the key. Every change shows a card with the exact command, such as `HSET session:ab12 plan pro`, before it runs. On a production connection the card says so; on a read-only connection it asks **Run anyway?**. In the tree, `t`, `D` and `y` (copy the key) work on a key row. `D` on a namespace row deletes every key under it: the keys are found with `SCAN`, a card shows how many and a few of their names, and they are removed in batches with `UNLINK` (`DEL` on servers older than 4.0). If a namespace has more keys than one scan loads, run it again for the rest.

Press `n` on a Redis connection to open its console, a `.redis` file that starts with `# @conn <id>`. Write one command per line and press the send key on a line (or select several lines). Read commands run at once; other commands show a card first. Commands that can wipe data, stop the server or run code (`FLUSHALL`, `FLUSHDB`, `CONFIG SET`, `SHUTDOWN`, `DEBUG`, `MODULE`, `EVAL`, `FCALL`, `MIGRATE`, `REPLICAOF`, `ACL SETUSER`, …) show a strong warning card. `MONITOR`, `SUBSCRIBE`, `SELECT` and `MULTI` are not sent. `KEYS pattern` runs as `SCAN` with a note. An `AUTH` line is masked in the card, the page and the history. Console runs go to the same history as SQL.

## AI tools and their rules

The AI chat has database tools: `db_connections`, `db_schema`, `db_ddl`, `db_query`, `db_explain` and `db_history`. The system prompt mentions them only when the workspace has connections.

- **Reads run at once.** Schema, DDL, plans and a read (`SELECT`, `SHOW`, `EXPLAIN` without `ANALYZE` …) run in a separate read-only session: `BEGIN READ ONLY` with a statement timeout on PostgreSQL, `START TRANSACTION READ ONLY` on MySQL / MariaDB, and `sqlite3 -readonly` for SQLite. The server or the file enforces it, not a text check. SQLite needs `sqlite3` 3.37 or newer for this.
- **Every other statement asks.** The card shows the connection and the exact SQL (Up/Down scrolls a long one). **Always allow** for `db:<conn>/write` lets later writes on that connection run without asking; you can turn it off again in **AI: Agent Auto-Approve…**.
- **Production connections** (`"environment": "production"`) always ask before a write, with no "Always allow". **Read-only connections** (`"readOnly": true`) refuse AI writes.
- **Own sessions.** Each AI call opens its own session and closes it after. The AI never runs on your session and never joins your transaction. It cannot add or change connections or set passwords.
- **What you see.** Each `db_query` and `db_explain` run opens the SQL Result page marked **by AI** and adds an entry to the SQL history. Every call is in the AI audit log.
- **What the model sees.** Passwords never reach it. Server text is cleaned, cells are cut at 80 characters, and one answer is at most `db_ai_max_kb` KB (default 32); `max_rows` (default 100, max 1000) limits rows.
- **Redis.** `redis_keys` lists keys with `SCAN` (type and TTL), `redis_get` reads one key a page at a time, and `redis_command` runs one command. Read commands run at once in a read-only session that refuses anything else; `CONFIG`, `ACL` and `CLIENT` always ask because they can show secrets. Any write asks with the exact command (`db:<conn>/write`; production always asks; read-only connections refuse). Dangerous commands (`FLUSHALL`, `CONFIG SET`, scripts, `MIGRATE`, …) and `MONITOR` / `SUBSCRIBE` are refused without a card.

## Security notes

- Project files never hold passwords. A literal password in a project file is refused.
- The secret store uses the OS keyring when one is available. Without a keyring it may be a plain file with mode 0600; anyone who can read your account can read that file. Use `${env:NAME}` or `${cmd:line}` if you want the password to stay out of it.
- Passwords are removed from the SQL history, error text and what the AI sees.
- Host keys are checked against `known_hosts`. A changed key is refused with no way around it.
- When you edit rows or import a file, values are sent as bound parameters. Names are quoted for the database.
- A write always asks first. There is no "always allow" for your own writes. A production connection gets a stronger card.
- Reads for the AI run in a read-only session that the server or the SQLite file enforces.
- CSV and TSV exports protect spreadsheets from formula injection (see [Export and import](#export-and-import)).
- Hiding the view closes its sessions, so an idle view keeps no database process.

## Troubleshooting

**`sqlite3` is not installed.** The view shows an install command for your system, for example `sudo apt install sqlite3` or `brew install sqlite`. Press `y` on that row to copy it. Then press `R`.

**`sqlite3` is too old.** The message says `version 3.33 or newer is needed`. The AI tools need 3.37 or newer. Update `sqlite3` and press `R`.

**Connection refused.** The server is not running, or the host or port is wrong. If the server is a container or compose service, the footer says `stopped · S start`. Press `S` on the connection. Otherwise check the host and port with `e`, then press `T`. If the server is only reachable from a bastion, add an SSH tunnel.

**Unknown host key.** A card shows the server's `SHA256:` fingerprint. Compare it with the one your admin gave you. Choose **Trust and save to known_hosts** only if it matches. If the key changed, do not connect. Ask your admin.

**The server asks for SSL.** The error says so and shows the `ssl` setting to add. Run **Databases: SSL Settings…**.

**`SSH tunnel closed · R reconnect`.** The tunnel dropped. Press `R`.

**Wrong password.** Press `p` on the connection, enter it again, then press `T`.
