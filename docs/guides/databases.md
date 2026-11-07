# Browse databases

The **Databases** view manages connections to PostgreSQL, MySQL or MariaDB, SQLite, and Redis or Valkey. You can browse the schema, run SQL in a console, read and edit rows, export and import data, and let the AI help with safe rules.

A connection opens only when you expand it. Schema details load as you move down the tree.

## Open the view

Choose **Databases** in the activity bar or run **Databases: Focus** from the command palette. The action `DatabasesFocus` has no default key; you can bind one in `keymap.json`.

Use `j` and `k` to move, `Enter` to connect or expand a row, `h` to fold a branch, `R` to reload the list, and `/` to filter by name. The filter works as you type: the tree narrows with every key, `Enter` keeps it, `Esc` clears it. It is the same filter as in Containers. Without a filter, `Esc` returns focus to the editor. `?` opens a card with all the keys of the view; **Open guide** on the card brings you here.

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
| `options` | Session options, see below. |

**Connection options** (`"options": {…}`), checked when the file loads:

| Key | Meaning |
| --- | --- |
| `schema` | Default schema: the PostgreSQL `search_path` (a comma list) or the MySQL database to `USE`. |
| `timeZone` | Session time zone, like `UTC` or `Europe/Berlin`. |
| `isolation` | `read uncommitted`, `read committed`, `repeatable read` or `serializable`. |
| `initSQL` | Statements every new session runs after the options above. |
| `txMode` | `manual` starts the connection in manual commit (the transaction mode picker still switches it). |
| `idleMinutes` | Closes connections that were idle this long; a new one opens on the next query. |
| `socket` | Unix socket path (PostgreSQL: the socket directory). |
| `databases`, `schemas` | Globs: the tree shows only these. |
| `objects` | Globs that show tables, views and routines; `!glob` hides them. |
| `readOnlyTables` | `schema.table` or `table` globs that the grid refuses to edit. |

Every pooled connection runs the schema, time zone, isolation and `initSQL` statements first; a failing one fails the connection with its reason.

**Schema loading.** `db_introspect_level` sets how much a table row loads (`auto` loads less in big schemas). `r` in the tree reloads only the object under the cursor; `R` reloads everything. Each load is kept in `db/schema-cache.json` (names and types, never data); when a load differs from the cached one the row says `changed since …`. **Databases: Show Schema Cache Info** tells when a connection was last loaded. `db_complete_catalogs` adds the system catalogs to SQL completion.

A row with a problem shows the reason, for example `literal password in project config`, `duplicate connection id` or `sqlite path is outside the workspace`. Fix the file and press `R`.

## Suggestions

Termocode suggests connections it finds. A suggestion row has no password and does nothing until you add it.

- SQLite files ending in `.db`, `.sqlite` or `.sqlite3`, up to two folders below the workspace root.
- `DATABASE_URL`, `REDIS_URL` or `MYSQL_URL` in the workspace `.env`. The value is not shown. The saved connection uses a `${dotenv:NAME}` reference.
- PostgreSQL, MySQL, MariaDB, Redis, Valkey, KeyDB and Dragonfly containers and compose services. The Databases view reads the container list itself when it opens and when you press `R`. You do not need to open Services first.
- A server on this computer that is not in Docker. When the view opens and on `R`, Termocode tries one quick connection to `127.0.0.1` on ports 5432, 3306 and 6379. A port that answers shows as **PostgreSQL on localhost:5432**, **MySQL on localhost:3306** or **Redis on localhost:6379**. Ports that a saved connection or a container already uses are skipped. No other computer is ever tried.

Press `a` on a suggestion (or `Enter`) to add it. Press `x` to hide it. A hidden suggestion stays hidden.

With no connections yet, the view lists the suggestions and offers **a add** and **Add remote… (SSH)**. **Add remote…** asks for the server type, then the usual connection steps, then the SSH tunnel steps, then runs the test.

## Mouse

- Click a row to select it. Double-click a connection to connect or expand it, a folder to fold or unfold it, and a table or view to open its data.
- On a suggestion, click to select and double-click to add it.
- Click **a add**, **R refresh** or **/ filter** in the header. In the empty view, click **a add** or **Add remote…**.
- Right-click a row for its menu.

## Add a connection

Press `a` and choose PostgreSQL, MySQL / MariaDB, SQLite, Redis / Valkey, DuckDB, Trino, Presto, or **From a URL**.

The MySQL family has presets on the MySQL driver: MariaDB, TiDB (port 4000), PlanetScale / Vitess and Amazon Aurora (MySQL) (both with TLS), and SingleStore. A preset fills the port, user and SSL mode, shows a short note on what differs from MySQL, and is saved as `"preset"` in the connection; the tree shows its name next to the host.

- **A server type** asks for the host, port, user and database. Then it asks for a masked password.
- **DuckDB** asks for a database file, like SQLite. It runs the `duckdb` command line client (install it and put it on `PATH`); each run stands alone, so there are no transactions and `:memory:` keeps nothing between runs. Read-only runs use `-readonly`. The tree shows the schemas of the file.
- **Trino** and **Presto** use the coordinator's REST protocol (port 8080). The database step asks for the **catalog** (for example `hive`); the tree shows its schemas. A password is sent only over TLS. `USE` changes the catalog and schema for the next runs, transactions use `START TRANSACTION`, and read-only runs use `START TRANSACTION READ ONLY`.
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

The password prompts (here, for an SSH password and for a key passphrase) accept a reference instead of a value: `${env:NAME}`, `${dotenv:NAME}` or `${cmd:…}`. The text you type is saved, not the value, and a bad reference is refused in the prompt. A project file may use `${secret:…}` and `${dotenv:…}` but not `${cmd:…}`: the row shows a problem and the command is not run. See [References](ai-setup.md#type-a-reference-instead-of-a-secret).

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

## Command line client

Press `$` on a connection to open its own client in a terminal tab: `psql`, `mysql`, `sqlite3` or `redis-cli` (it must be on your PATH). The host, port, user and database come from the connection, and a read-only connection opens the client read-only too. The password and SSL settings go in the tab's environment only, never on the command line, so they do not show in the process list. A connection through an SSH tunnel cannot use this: the tunnel lives inside termocode.

## Disconnect

Press `x` on a connection and choose **Disconnect** to close all its sessions. Sessions also close when you leave the Databases view, so a hidden view keeps no database process. A session with an open manual transaction stays open until you commit or roll back (see [Transactions](#transactions)); `x` then asks **Cancel**, **Commit** or **Roll back** (**Cancel** has the focus).

## The SQL console

Press `n` on a connection to open its console. Consoles live in your user configuration under `db/consoles/<workspace>/<connection>.sql` (mode 0600) and start with a `-- @conn <id>` line. **Databases: Another Console** opens one more console for the same connection (up to 9; the tab says *SQL console 2*, *3* …). A project `.sql` file can use the same line; without it, the first run asks which connection to use and remembers the answer until you quit.

Press `Ctrl+Alt+R` (or `Ctrl+Enter` in terminals that send it) to run the statement under the cursor. In visual mode the selection runs as typed. **Databases: Run File** runs every statement in order. It stops at the first error and asks: **Continue** runs the remaining statements, **Stop** ends the run. A small note at the end of each statement shows the key, the connection and the last result. `Esc` cancels a running query; while it runs the note says `running… · Esc cancels`.

**Execution modes.** `db_exec_mode` sets what the run key sends: `largest` (the whole statement, the default), `smallest` (the innermost subquery around the caret), `from-caret` (the statement and every one after it) or `ask` (a list to choose from, innermost first). **Databases: Choose Execution Mode…** saves it. **Databases: Run From Caret to End of File**, **Run Smallest Statement (Subquery) at Caret** and **Run… (Choose Statement or Subquery)** do one run in that mode. `db_selection_split` set to `false` sends a selection to the server as one text instead of splitting it.

**Result page.** `N` goes to row N (it opens that page and puts the cursor on the row). `L` loads all rows when the result was cut at `db_max_rows` (it runs the statement again, up to 100000 rows). **Databases: Set Result Page Size…** changes the rows per page of the shown result at once. `db_result_new_tab` keeps every result in its own tab; `db_auto_transpose` opens a one-row result flipped (one line per column).

A read runs at once. Any other statement asks first, and the card shows the exact SQL (use Up/Down to read a long one). The title says what is special: `DELETE` or `UPDATE` without `WHERE`, `DROP`, `TRUNCATE`, `ALTER`, a production connection, or a read-only connection. There is no "always allow". In a file run, each write gets its own card.

## Results

The result opens as the **SQL Result** page, in a split on the right when the editor is at least 120 columns wide. The first rows show the SQL, the connection, the row count and the time. `Tab` switches between Grid, JSON and Messages. Messages shows the full error, the server's notices and the time.

| Key | Action |
| --- | --- |
| `]` / `[` | next / previous page |
| `o` | sort by a column (choose it again to reverse) |
| `f` | show only rows that contain a text |
| `v` or `Enter` | open the cell under the cursor; `q` goes back. JSON and XML are pretty-printed, long text wraps, a binary value is a hex dump (the first 16 KB), a PNG, JPEG, GIF or WebP image says its kind and size, and a geometry value (WKB or EWKB hex, as PostGIS prints it) is shown as WKT and GeoJSON text |
| `=` | count, sum, avg, min and max of the column under the cursor on this page; select rows with `V` first to sum up only those. Text columns show count, min and max |
| `y` | copy the cell |
| `Y` | copy the column, one value per line |
| `c` / `C` | copy the loaded rows as CSV / JSON |
| `S` | export the rows to a file (CSV, TSV, JSON, SQL INSERTs, Markdown or HTML) |
| `M` | heatmap on / off: every column with only numbers gets a colour per cell, from cool (lowest) to hot (highest) on the page. Also **Databases: Toggle Result Heatmap** |
| `B` | chart: pick a number column, then a bar chart (one bar per row, labelled by the first text column) or a line chart (braille dots, the rows in order) opens in a read-only buffer; `q` goes back. Up to 500 loaded rows. Also **Databases: Chart Result…** |
| `r` | run again (writes ask again) |
| `1` … `9` | show result tab 1 … 9 |
| `p` | pin the result tab (again: unpin) |
| `gd` | go to the statement |
| `h` | SQL history |
| `Esc` | cancel a running query |
| `q` | close the page |

### Columns

| Key | Action |
| --- | --- |
| `H` | hide the column under the cursor |
| `O` | pick columns to show or hide |
| `<` / `>` | move the column left / right |
| `F` | freeze the columns up to this one on the left (again: unfreeze) |
| `{` / `}` | scroll the columns after the frozen ones |
| `(` / `)` | make the column narrower / wider |
| drag | drag a column's right edge in the two header lines to resize it |
| `g=` | reset the column layout |

The layout of a table is saved per connection and table, so it comes back the next time you open the table. A column the table gains shows at the end. A query result keeps its layout until you quit.

A result loads at most `db_max_rows` rows (default 10,000); the page says when the limit was reached, so add a `LIMIT` or a `WHERE`. Only one page (`db_page_rows`, default 100) is written to the page at a time. A statement stops after `db_timeout` seconds (default 30). Set these in your user `config.json`.

## Result tabs

Every result has a tab. A new run replaces the tabs that are not pinned; a pinned tab and a tab with pending changes stay. When a run has several statements, each statement that returns rows (or fails) gets its own tab. Up to nine tabs are kept. The first row of the page lists them when there is more than one: `1 pinned [2 edited] 3` means tab 1 is pinned, tab 2 is on the page and has pending changes. Press `1` … `9` to switch.

## Table views

Move to a table or view in the tree and press:

| Key | Action |
| --- | --- |
| `o` | open the table's data (also **Databases: Open Table Data**) |
| `d` | describe: columns, types, null, defaults, keys, indexes, foreign keys, triggers |
| `D` | the DDL in a read-only SQL buffer (`q` goes back) |
| `s` | write `SELECT * FROM <table> LIMIT 100;` at the end of the connection's console and run it there |
| `c` | generate code from the table's columns and copy it: SELECT, INSERT, UPDATE, DELETE, CREATE or DROP (names quoted, placeholders for values; nothing runs), the column names, qualified column names, a Go struct, a TypeScript interface or a Python dataclass. In the list Enter copies and `→` inserts the text at the editor cursor. On a routine (PostgreSQL, MySQL), `c` offers **Call with arguments…**: it asks for each argument (blank for NULL) and makes `SELECT * FROM f(…)` or `CALL p(…)`, and DROP with the argument types |
| `#` | count the rows |
| `i` | import a CSV file into the table (not for views) |
| `y` | copy the quoted table name |

The data page asks the server for one page at a time (`db_page_rows`, default 100) in the table's key order, so a big table stays cheap. `]` and `[` fetch the next and previous page. `o` on the page sorts by a column on the server (again: reverse). `f` asks for a `WHERE` condition, for example `plan = 'pro' AND id > 100`; an empty text shows all rows. A condition is one plain read: a `;`, a comment (`--`, `/* */`, and `#` on MySQL), a dollar quote, unbalanced brackets or a word such as `DELETE` or `INTO` is refused, and nothing is sent. Names of tables and columns are always quoted for the connection's database. `*` on a cell shows only the rows whose column holds the same value, added to the filter already set. `b` opens the filter builder next to the raw text: **Add a condition** asks for a column, an operator (`=`, `<>`, `<`, `<=`, `>`, `>=`, `LIKE`, `NOT LIKE`, `IN`, `IS NULL`, `IS NOT NULL`) and a value, and adds it with `AND` (or with `OR`: **Add an alternative condition**); **Sort by a column** asks for the column and the direction; **Edit the WHERE text** is `f`; **Clear the filter** shows all rows. The builder writes the same `WHERE` text you could type (names quoted, values as text literals), and it is checked the same way. The row count is never automatic: `#` counts the rows the current `WHERE` keeps and shows `of N` in the summary.

`t` flips the page to the Transposed view: one row as `column | value` lines, which suits narrow windows and wide tables. `]` and `[` step through the rows (and the pages), `v` opens the value under the cursor, `t` goes back to the grid.

**Foreign keys.** On a table's data page, `gf` on a foreign key cell opens the row it points to (the other table's data page, filtered to that key). `gb` goes back to the page the jump started from; the trail keeps the last 64 jumps. `gr` lists the tables whose foreign keys point at the row under the cursor; pick one to open its rows. `gl` on a foreign key cell lists the values of the referenced column (the first 500, sorted); the one you pick becomes a pending change, like `e`, so `w` shows the SQL and `u` undoes it. A NULL key points to no row, and a new row must be saved before you can follow it.

PostgreSQL has no `CREATE TABLE` text to ask for, so its DDL is rebuilt from the catalogs (columns, primary key, indexes, foreign keys) and the buffer says so; it is a reconstruction, not a dump. MySQL / MariaDB and SQLite show the server's own statement.
**Dependencies.** **Databases: Show Dependencies** on a table, view or routine opens a buffer with what it uses and what uses it: foreign keys link tables, and views and routines link by the names in their SQL text (not in comments or strings). **Databases: Show ER Diagram (Text)** draws tables as text boxes with their PK and FK columns and one line per foreign key: on a table, that table and its direct neighbours; on a schema, the whole schema. Both read the schema on the read-only session.
**Table designer.** **Databases: Create Table…** (cursor on a schema or a table in it) and **Databases: Alter Table…** (cursor on a table) open a form: one part per line, the syntax is in the `--` notes at the top. A new table starts with `table <name>`, then `column <name> <type> [not null] [default <expr>] [pk]`, `index`, `unique`, `fk … references …` and `check` lines. A change is a list of `add column`, `drop column`, `rename column <old> to <new>`, `add index`, `drop index`, `add unique`, `add fk`, `add check` and `drop constraint` lines. Submit builds the DDL; a mistake brings the form back with the reason. The preview card shows the exact script, with **Cancel** first, **Edit** back to the form and **Apply**. It says how many statements drop something, and a production connection gets a stronger title. Apply runs the script on the connection's session; refresh the tree (`r`) to see the change. A read-only connection is refused. Types, defaults and checks are SQL text you type, so a `;` in them is refused. SQLite cannot add or drop a constraint on an existing table.
**Find Object.** **Databases: Find Object…** searches every connected database at once. It reads the schemas in the background on the read-only session (up to 50 schemas per connection) and lists their tables, views, columns and routines; type to filter. Enter moves the tree cursor to the object (a column goes to its table). When the object's schema is not expanded in the tree yet, Enter shows the table's description instead.
**From SQL.** In a console or a project `.sql` file, put the cursor on a table or view name (`users`, `public.users`, `"Order Items"`). **Databases: Go to Definition** opens its DDL; the name must be loaded in the tree, so expand its schema first. **Databases: Find Usages** lists every line of the project's `.sql` files that names it (whole word, any case); Enter opens the line.

**From SQL.** In a console or a project `.sql` file, put the cursor on a table or view name (`users`, `public.users`, `"Order Items"`). **Databases: Go to Definition** opens its DDL; the name must be loaded in the tree, so expand its schema first. On `alias.column` (`u.email`) it opens the table's DDL with the cursor on the column; on a function or procedure name it opens its stored source. **Databases: Find Usages** lists every line of the project's `.sql` files that names it (whole word, any case); Enter opens the line. **Databases: Rename in SQL…** asks for a new name, then a card (Cancel first) says how many places change in this buffer and in the other project `.sql` files; Rename replaces the whole name (plain or quoted) and leaves strings and comments alone. Only the SQL text changes, never the database; files open with unsaved changes are left alone.

**Quick documentation.** **Databases: Quick Documentation** shows a card about the table, column or routine under the cursor of a SQL buffer: its columns with types, the primary key, keys, indexes and foreign keys. In the Databases tree, press `K` (or right-click → Quick documentation) on a table, view, column, routine or schema. It reads only what the tree has loaded, so no query runs. **Databases: Show INSERT Column Hints** writes the target column before each value of `INSERT INTO t (a, b) VALUES (…)`; the hints clear on your next edit.

**Virtual objects.** **Databases: Edit Virtual Objects…** opens `.termocode/virtual/<connection>.jsonc` (a commented template the first time). It holds foreign keys the database does not declare (`foreignKeys`, or `nameRules` such as the suffix `_id`), computed columns (`columns`: a read-only expression) and saved SELECTs (`views`). After you save, Refresh (`R`): virtual keys and columns show under the table and in Quick Documentation, completion offers the virtual column names, Select 100 rows adds the computed columns, and a **Virtual Views** group under the connection runs a saved SELECT with Enter. Nothing is written to the database; a broken entry is skipped with a warning.

**Edit object source.** Press `E` on a view, routine or trigger (or **Databases: Edit Object Source**): its stored CREATE statement opens in a buffer, read on a read-only session. Edit and save, then run **Databases: Submit Object Source…** from that buffer. termocode reads the stored source again and shows a card with the diff and the exact script; Cancel comes first. The card warns when someone else changed the object since you opened it, and when the engine cannot replace it in one transaction (MySQL drops the old routine first). The source must stay one CREATE statement for the same object. **Databases: Pending Source Changes…** lists the saved sources you have not submitted; Enter opens one.

**Object actions.** Press `A` on a tree row (or right-click → Object actions…, or **Databases: Object Actions…**). On a table: copy it as a quick backup (the name defaults to `<table>_backup_<date>`), truncate it (SQLite: delete all rows), drop it, or (PostgreSQL) drop it with cascade. Before a drop, termocode lists the foreign keys and views that would also be removed or broken. On a view, routine or sequence: drop it. On a column: make it the primary key. On a schema or database: create a new one or drop it; on a connection: create a database. Every action ends on a card with the exact SQL; Cancel comes first, and Copy SQL copies it instead. A read-only connection gets no actions.
**Other objects.** `D` (or **Databases: Show DDL**) also works on a routine, a trigger and a sequence: it reads the stored DDL on the read-only session and opens it in the same kind of buffer. PostgreSQL sequences are written as `CREATE SEQUENCE` from `pg_sequences`; MySQL / MariaDB and SQLite have no sequences. A table that has partitions lists them as `Partition · name` rows under it. A PostgreSQL or MySQL / MariaDB connection also shows a **Roles** group with the server's users and roles, when the login may read them.

## Completion

In a SQL buffer, `Ctrl+X Ctrl+O` (and the pop-up that opens while you type) offers keywords, tables, views, routines, sequences and schemas from the tree you have already opened. After `alias.` or `table.` it offers that table's columns; the alias comes from the statement at the cursor (`FROM users u`, `JOIN orders AS o`). The first time a table's columns are needed they are read once and the pop-up opens again. Completion reads a cached copy: it never asks the server while you type.

Once a console's connection is connected, the whole schema is loaded once in the background (every schema of the database, the columns of up to 400 tables with their types, and the foreign keys), so you do not need to open the tree first. Columns of a `WITH name AS (…)` CTE or a `(SELECT …) alias` subquery come from its select list. After `JOIN orders o ON` the pop-up offers whole conditions from the foreign keys, such as `o.user_id = u.id`. Each column shows its type family (`column num`, `column text`, `column time`); after `u.created = o.` the columns of the same type come first.

## Inspections and quick fixes

While you type, a SQL console or file is checked against the schema already loaded: syntax errors are underlined as errors, unknown tables and `alias.column` names as warnings, with a hint such as *Unknown column u.emial. Did you mean email?* Put the cursor on the underlined name and press `<leader>.` (or run **Databases: Apply SQL Quick Fix**) to replace it with the suggested name; a quoted name stays quoted, and `u` undoes it. Nothing is asked of the server.

## Edit rows

You can change the rows of a table's data page, and of a console result that is a plain `SELECT` from one table (columns named as they are in the table, no `DISTINCT`, `JOIN`, `GROUP BY` or `UNION`). The table needs a primary key or a unique key, and the result must contain its columns: each change finds its row by the full key. A SQLite table with neither is edited by its hidden `rowid` on the data page (not in a console result); a table that has columns named `rowid`, `_rowid_` and `oid` stays read-only. Otherwise the page says **read-only** and why (for example `read-only: no key`); `e` then shows the reason. Views, AI results, table descriptions and read-only connections cannot be edited.

| Key | Action |
| --- | --- |
| `e` | edit the cell under the cursor in a one-line prompt (`\N` means NULL) |
| `E` | edit the value in a buffer (for long or multi-line values); `:w` keeps it, `q` leaves without a change |
| `+` | add a new row; its cells show `<default>` until you set them with `e` |
| `-` | mark the row for deletion (again: keep it); on a new row: remove it |
| `V` then `e` | set one value in the column under the cursor for every selected row (one prompt) |
| `V` then `+` | add a copy of each selected row; key columns get their default and binary values are left out |
| `V` then `-` | mark every selected row for deletion (again: keep them); new rows in the selection are removed |
| `P` | paste rows from the clipboard: tab-separated values go from the cursor cell to the right and down, extra lines become new rows (`\N` is NULL); values past the last column, in a binary column or on a deleted row are skipped |
| `D` | set the cell back to its column's `DEFAULT` (the SQL says `= DEFAULT`; on a new row the cell is left out). SQLite cannot do this in an `UPDATE`, so it says so |
| `U` | put a new random UUID (version 4) in the cell |
| `u` | undo the change of the cell under the cursor, else of the row; on a row without changes it asks to undo all changes |
| `w` | submit the changes |

A typed value is checked against the column type before it is kept: whole numbers for integer columns, numbers for `numeric` and `float`, `true` / `false` for booleans, a UUID for `uuid`, valid JSON for `json` and `jsonb`, and `YYYY-MM-DD` for `date`. A value that does not fit is not kept, and a message says what fits. Other types (and arrays) are checked by the server when you submit.

Changes stay on the page until you submit them. The left margin marks each row: `*` changed, `+` new, `-` to be deleted, and the page says how many changes wait. Anything that loads the rows again (another data page, a sort or a filter on a data page, `r`) asks **Keep** or **Discard** first; switching tabs or closing the page keeps the changes.

`w` shows a card with the exact SQL, one statement per row, with the values written out. **Copy SQL** copies it. **Submit** runs the statements as one transaction: values are sent as bound parameters (never pasted into the SQL), names are quoted for the connection's database, and an `UPDATE` also checks that the old value is still there. Each statement must change exactly one row; if one changes no row (someone else changed or deleted it) or fails, everything is rolled back, your changes stay on the page, and a card offers to reload the rows. After a submit the page shows the new values at once and then reloads them from the server; **Messages** lists the statements that ran. A production connection gets the stronger card with **Cancel** first. There is no "always allow", and the AI cannot submit your changes.

## Transactions

Each connection starts in auto-commit mode: every statement is saved at once. Press `t` on a connection (or use **Databases: Transaction Mode…**) and choose **Manual** to group your work: the first statement begins a transaction, the next ones join it, and nothing is saved until you commit.

- `K` on the result page or **Databases: Commit** saves the changes; `Z` or **Databases: Roll Back** drops them.
- The status bar and the result page show `manual commit · 2 not committed` while changes wait. Typed `BEGIN`, `COMMIT` or `ROLLBACK` statements are not sent in manual mode; use `K` and `Z`.
- **Savepoints.** With a transaction open, **Databases: Set Savepoint…** asks for a name (letters, digits and `_`) and sends `SAVEPOINT name`. **Databases: Roll Back to Savepoint…** lists them (newest first); a card asks first (Cancel is the default), then `ROLLBACK TO SAVEPOINT name` drops every change after it. The transaction stays open, and the later savepoints are forgotten. **Databases: Sessions…** lists the open sessions and closes one.
- Submitted row changes join the open transaction; data pages read inside it, so they show your own changes.
- Quitting, disconnecting, or switching back to auto-commit with changes waiting asks **Cancel**, **Commit** or **Roll back**.

## Export and import

**Export.** `S` (Shift+s) on a result page (or **Databases: Export Result…**) opens a picker: CSV, TSV, JSON, SQL INSERTs, Markdown table, HTML table (one page you can open in a browser; every cell is escaped), Parquet (for data tools; every column is stored as text, NULL stays NULL). Then type a file name; the default is `<table>-<date>.<ext>` in the workspace, and a full path works too. If the file exists, a card asks before it is replaced. The rows go into a temporary file in the same folder, and the file you chose appears only when every row is written, so a cancel or an error leaves no half file and keeps the old one.

When the result is only part of the rows (the 10,000-row limit, or one page of a table), a card asks: **Export the loaded rows** or **Export all**. Export all runs the query again, without the row limit, in a read-only session and writes the rows as the server sends them (a page of 1,000 at a time, never all in memory). A progress card shows the count; `Esc` cancels and removes the temporary file. Only a plain `SELECT` can run again, so a write is never repeated; Export all reads committed data, not the changes of your open transaction.

CSV and TSV protect spreadsheets from formula injection: a cell that starts with `=`, `+`, `-`, `@`, a tab or a carriage return gets a leading single quote (`'=SUM(A1)`). A real number such as `-5` keeps its sign. NULL is an empty cell in CSV and TSV, `null` in JSON and `NULL` in Markdown and INSERT files. INSERT files quote names and values for the connection's database (a quote is doubled; MySQL also doubles a backslash) and write binary values in the database's own form. The **Copy** keys `c` / `C` do not add the quote.

**Import.** `i` on a table (or **Databases: Import CSV into Table…**) lists the `.csv` files of the workspace (or asks for a path). A preview card shows what was detected: the encoding (UTF-8, UTF-16 or ISO-8859-1), the delimiter (comma, semicolon, tab or pipe), whether the first row is a header, the row count, the column mapping (file column to table column, matched by name, or by position without a header) and the first rows. **Map columns** changes one table column at a time, and **Header** switches the header guess. Empty cells become NULL.

**Open a CSV/TSV as a table.** **Databases: Open CSV/TSV as Table…** opens the active `.csv`/`.tsv` buffer, or lists the workspace files (or asks for a path). The file is copied into a new SQLite database in termocode's cache folder (the file itself is never changed) and shows as a connection named `<file> (CSV)` for this session only: it is not saved to a config file. `SELECT *` runs in its console, so the grid can sort, hide columns, transpose and filter, and you can write any SQL against the table. Opening the same file again loads it fresh.

**Import** reads the file one row at a time and inserts it with bound values in INSERTs of up to 1,000 rows, all in one transaction: if any row fails, everything is rolled back, nothing is written, and a card names the row and line. A progress card shows `N / total rows`; `Esc` cancels and rolls back. A read-only connection never imports, a production connection gets the strong card, and in manual transaction mode the rows join your open transaction (Commit saves them). The AI cannot import.

**Bad rows: skip.** The card's **Bad rows** button switches from **stop** (the default above) to **skip**: a row that cannot be read, has the wrong number of fields or fails its INSERT is left out (it is rolled back alone), and the other rows are saved. The done toast says how many rows were skipped and names an error log (row, line and the server's error for each) in the termocode config folder.

**Import as a new table.** **Databases: Import CSV as New Table…** on a schema, a database or a table in the tree asks for a CSV file and shows the `CREATE TABLE` it would run (Cancel first): the table is named after the file (lower case, `_` for other characters) and every column is `TEXT`, named like the header (a repeated name gets `_2`). **Create and import** makes the table, then the usual import card opens for it. Change the column types later with `ALTER TABLE`.

**Run a SQL dump.** **Databases: Run SQL Dump File…** on a connection asks for a `.sql` file (up to 64 MB), splits it into statements and shows one card (Cancel first): the file, the statement count, how many delete objects or rows (`DROP`, `TRUNCATE`, `DELETE`) and a PRODUCTION line on a production connection. **Run** sends the statements one by one in the connection's session, with no card for each. A progress card shows `N / total statements`; `Esc` or **Cancel** stops it. The first failing statement stops the run and is named with its error; what ran before it stays, unless the file has its own `BEGIN` / `COMMIT`. A read-only connection never runs a dump. Press `R` in the tree afterwards to see new or dropped objects.

**Formats.** The export picker (`S`) has CSV, TSV, JSON, SQL INSERTs, Markdown, HTML, JSON lines, YAML, XML and SQL UPDATEs. SQL UPDATEs need a key: the table's primary key in a data view, or the key of the result; without one the export says so and writes nothing. Columns you hid in the grid are left out of the file.

**CSV settings** (in `settings.json`): `db_csv_separator` (one character, `\t` for a tab; default `,`), `db_csv_header` (`false`: no row of column names) and `db_csv_null` (the text of a NULL cell; default empty).

**Masking.** `db_export_mask` is a list of column names (any case; `*` and `?` match), for example `["*password*", "email"]`. Exports, **Copy Result As…** and **Export Tables to Folder…** write `••••` for their values; NULL stays NULL. The grid still shows the real values.

**Copy as.** `A` on a result page (or **Databases: Copy Result As…**) copies the loaded rows in any of the export formats.

**Export tables to a folder.** **Databases: Export Tables to Folder…** on a schema, a database or a table in the tree asks for a folder (made when missing), then shows a card (**Cancel** first). Every table is written as `<table>.csv` and the tables' DDL as `schema.sql`, in a read-only session. A file that is already there is kept and named in the final message. A card shows the progress; `Esc` or **Cancel** stops it.
**SQL files.** **Databases: Go to Statement…** lists the statements of the `.sql` file in the editor, with their line and kind (read, write, DDL); Enter jumps there. The statements are split in the file's dialect: the driver of its connection, else a `-- @dialect postgres` (or `mysql`, `sqlite`) line, else the dialect the text shows (`::` or `RETURNING` for PostgreSQL, backticks or `AUTO_INCREMENT` for MySQL, `PRAGMA` for SQLite); the picker title says which. **Databases: Switch File Connection…** picks the connection the file runs on (or detaches it). The choice is saved in `.termocode/sql-connections.json` with paths relative to the workspace, so it is kept after a restart; a `-- @conn id` line in the file still wins.

## EXPLAIN and ANALYZE

**Explain.** In a SQL console press `\x` (leader, then `x`) on a statement, or use **Databases: Explain Statement**. On a result page `x` explains the statement that made the result. The plan opens on the result page as the **Plan** view: one line per step, indented, with the cost and row estimates. The step that takes most of the cost (at least 40 % of the whole plan, without its children) is marked with `!` and `<- costly` and coloured. Details such as `Filter:` and `Hash Cond:` sit under their step. Folds follow the indent: `za` folds a step, `zR` opens all. `y` copies the plan, `C` copies the server's own answer (the native JSON on PostgreSQL and MySQL), `Tab` shows that raw answer, `r` plans again. `b` switches to the bar view: one bar per step for its own share of the whole cost (or of the real time after an analyze), so the slow step stands out; `b` again goes back to the tree.

A plain plan never runs the statement. It uses `EXPLAIN (FORMAT JSON)` on PostgreSQL, `EXPLAIN FORMAT=JSON` on MySQL / MariaDB (the table form on a server without JSON), and `EXPLAIN QUERY PLAN` on SQLite, always in the read-only session.

**Explain Analyze.** `\X` in a console, `X` on a result page, or **Databases: Explain Analyze Statement…**. This RUNS the statement and shows the real time and rows, so a card comes first and says so. A read runs in the read-only session. Any other statement runs inside a transaction that is always rolled back, and the card says so; if the rollback fails, the page shows an error. On a production connection the card is the strong one. A read-only connection analyzes reads only. DDL (`CREATE`, `DROP` …) is not offered, because it cannot be rolled back; with a manual transaction open a write is not analyzed either. SQLite has no `ANALYZE`: it shows the plan and never runs the statement.

## Compare schemas

**Databases: Compare Schemas…** compares two schemas of connected databases. Connect both first (`Enter` on each connection). Then pick two schemas:

1. The **source**: the one to copy, the schema that is right (for example your local database).
2. The **target**: the schema to change (for example staging).

Both must be the same database type: PostgreSQL with PostgreSQL, MySQL / MariaDB with MySQL / MariaDB, SQLite with SQLite. The target list marks the other types, and picking one only shows a message. Two schemas of the same connection work too.

termocode reads both schemas in read-only sessions and writes each one as clean SQL: objects sorted by name, names quoted for the database, no schema names. The diff card shows one file per kind: `tables.sql` (columns, primary keys, unique keys, foreign keys and, on PostgreSQL, MySQL 8.0.16+ and MariaDB, checks), `views.sql`, `routines.sql` (not for SQLite) and `indexes.sql`. The left side is the target, the right side is the source, so the green lines are what the target is missing. A column order that differs is not a difference. `Tab` moves to the next file, `n` / `p` to the next change, `Esc` closes the card.

**Copy migration script** (`y`) copies a script that makes the target match the source. **Open script** (`o`) opens it in a read-only SQL buffer. termocode never runs this script: you read it, change it if needed, and run it yourself. The script does the basic steps: create and drop tables, add and drop columns, drop and create indexes and views. Other differences (a changed column type, a changed key, a routine) are lines that start with `-- REVIEW:`, for you to do by hand. The top of the script says which schema it changes and lists every destructive statement (`DROP TABLE`, `DROP COLUMN`, `DROP INDEX`, `DROP VIEW`); each one also has a `-- DESTRUCTIVE` line above it, because it deletes objects or data.

Very large schemas are compared up to 2,000 tables; the card and the script say so when a schema is bigger.

## Search data

**Databases: Search Data in Schema…** finds a value in every column of every table of a schema (on MySQL / MariaDB: a database). Move to a schema, a database or a table in the tree (a connection row searches `public`, the connection's database, or `main` on SQLite), run the command and type the value. Plain text matches any cell that contains it, in any case; `=value` matches only a whole cell.

The search runs in a read-only session. Each table gets one `SELECT` with a row limit (50 per table, 200 tables at most, 2,000 matching rows in all), and the value is a bound parameter, never part of the SQL. Views are skipped. A card shows how many tables are done; `Esc` or **Cancel** stops it. At the end a list shows the tables with matching rows; `Enter` opens the table's data view with a filter that shows those rows. A table that cannot be read is counted and skipped.

The AI has the same search as `db_search_data` (read-only, runs at once).
## Compare data
Compare two results, two tables, or one table on two connections. On the first result page press `gc`: it is marked as the **source** (the rows you want). Show the other result or open the other table (another tab, table or connection) and press `gc` again: that is the **target**. `gc` twice on the same result removes the mark.
A table's data page counts as the whole table with its filter (up to 10,000 rows, read again on a read-only session); a console result counts as the rows it loaded. Rows match on the source table's key. When there is no key, termocode asks for the key columns. The card lists the rows that differ, one line per row (`−` the target's, `+` the source's), and counts the added, changed, removed and same rows; columns that only one side has are listed and not compared. When the target is table data, **Copy sync script** (`y`) and **Open script** (`o`) give the `DELETE`, `UPDATE` and `INSERT` statements, in one transaction, that make the target hold the source's rows. termocode never runs the script: review it, then run it yourself in a console.

## Schema as files

Press `F` on a schema (or a database, or any row under it) and choose:

- **Dump to a folder…** writes the schema as clean SQL, one file per object: `tables/<name>.sql` (with its keys and indexes), `views/<name>.sql` and `routines/<name>.sql` (not SQLite). The folder is relative to the workspace (`schema/<connection>-<schema>` is offered). A file that is already up to date is not touched, so the folder diffs well in git. When the folder holds files with other text, a card asks first (**Cancel** has the focus). Files of dropped objects stay; Compare shows them.
- **Compare with a folder…** lists every object under **Changed**, **Only in the database**, **Only in the folder** and **Identical**. `Space` selects rows; `Enter` takes the selected rows (or the row under the cursor) to a card: **Cancel**, **Update the folder** (write the database's version and remove the files of objects the database no longer has) or **Open the folder's SQL** (the folder's text of those objects in a read-only buffer, to review and run by hand; a changed table needs ALTER statements, not CREATE).

The schema is read on a read-only session; nothing here changes the database. When the project uses a migration tool (Flyway, Liquibase, Alembic, Prisma, Rails, golang-migrate or dbmate), the menu says so: the folder is a snapshot next to the migrations, not a migration.

## ER diagrams

**Databases: Export ER Diagram…** writes the schema under the cursor as Mermaid, PlantUML or Graphviz DOT text in a new buffer. Every table lists its columns (PK and FK marked); every foreign key is one line. The **· coloured** formats give tables with the same name prefix (the part before the first `_`) the same colour.

**Databases: Diagram Path Between Tables…** starts at the table under the cursor. Pick the other table; tables with no foreign-key path say "no path". termocode finds the shortest path along foreign keys (in either direction) and opens a Mermaid diagram of only those tables, with both ends highlighted. The first line names the path. The schema is read in a read-only session.

## Manage connections

**Databases: Manage Connection…** opens the actions of the connection under the cursor:

- **Edit…** asks for the name, host, port, user and database, one after the other, with the current value filled in.
- **Comment…** adds a note; the row shows it after the address.
- **Rename…** changes the id. The saved password moves with it.
- **Duplicate** makes a copy (without the password). For a project connection it is the only way to change it: the copy goes to your own list.
- **Copy as JSON** copies the connection without any password. **Databases: Paste Connection from Clipboard** adds it again, here or on another computer.
- **Copy SSL Settings from…** takes the SSL settings of another connection.
- **Save SSH as Profile…** keeps the SSH tunnel under a name; **Use SSH Profile…** puts it on another connection. Profiles are in `db/ssh_profiles.json` next to your connections file.
- **Save as Template…** keeps the connection (no id, no password) as a template; **Databases: New Connection from Template…** starts a new connection from it or from a built-in template (local PostgreSQL, MySQL, Redis, a SQLite file, PostgreSQL through SSH or in Kubernetes).
- **Show Driver and Server Version** reads the version from a connected server.
- **Delete…** asks first (**Cancel** is the default). It removes the connection from your file, closes its sessions and deletes its saved passwords. The database is not touched.

Without the UI, `termocode db list`, `termocode db show <id>`, `termocode db add` (JSON on stdin) and `termocode db remove <id>` work on the same file. They never print a password.

## Containers, Kubernetes and the cloud

**Databases: Run New Database Container…** runs PostgreSQL 17, MySQL 8.4, MariaDB 11 or Redis 7 in Docker (or Podman). A card shows the whole `docker run` command first; **Cancel** is the default. The port is open on `127.0.0.1` only. termocode makes a random password, passes it in the environment (never on the command line) and keeps it in the secret store. The new connection is added to your list; press `Enter` on it when the container is ready.

A server inside Kubernetes is reached through `kubectl port-forward`. Add a `kube` block to the connection: `"kube": { "context": "dev", "namespace": "shop", "pod": "pg-0", "port": 5432 }`. termocode starts the forward when you connect and ends it when the session closes.

**Databases: Discover Cloud Databases…** asks the installed `aws`, `gcloud` or `az` CLI for its PostgreSQL and MySQL instances. Pick one; termocode asks for the password, tests the connection and saves it like any new connection (with SSL `require`).

**Databases: Use Cloud IAM Token Login…** makes one of your own connections log in with a fresh token at each connect, from `aws rds generate-db-auth-token`, `gcloud sql generate-login-token` or `az account get-access-token`. A card shows the command first. The CLI must be signed in. Project files cannot use this, because they cannot run commands.

## Query log and console versions

**Databases: Show Query Log** opens every statement termocode sent to your SQL databases in this run (the newest 1,000) in a new buffer. Each statement has a `--` line above it: the time, the connection, how long it took and the result (rows, affected rows or the error). Statements termocode sends for itself (catalog reads, read-only sessions for compare and diagrams) end with `· internal`. The log stays in memory and is gone when termocode quits, because statements can hold real values.

Every run from a console saves the console first, so the local history keeps a version of it. **Databases: Show Console Version History** lists those versions for the active console, or for the console of the connection under the cursor. Pick one to compare it with the console now or to restore it. To delete one entry of the SQL history, open **Databases: History…** and pick **Delete an entry…**.

## Query parameters

A statement with parameters asks for each value before it runs: `?`, `:name`, `$1` (PostgreSQL, SQLite) and `@name` (not MySQL, where `@name` is a server variable). The values are bound, never pasted into the SQL. Type `NULL` for a null; a whole number is sent as a number. The console remembers the last values for this session and offers them next time. `Esc` runs nothing. Text in quotes and comments, `::` casts and the PostgreSQL `?` operator are not parameters.

## Sessions, locks and server info

**Databases: Show Sessions and Locks** lists the other sessions of the server in the result grid. PostgreSQL shows each `pid`, user, state, wait and `blocked_by` (the pids that hold the lock it waits for). MySQL shows the process list. **Databases: Show Server Info** shows the version, database, user, host and main settings. Both are plain reads: they are written into the connection's console and run there.

**Databases: Cancel Session Query…** stops the running query of one session. **Databases: End Session…** ends the whole session; its open transaction rolls back. Both ask for the session id (`pid` / `id` column), then show the usual run card first (Cancel is the default). SQLite and Redis have no sessions.

## pgTAP tests

Press `U` on a PostgreSQL connection, or choose **Databases: Run pgTAP Tests…**, to run the database's [pgTAP](https://pgtap.org) test functions. Type a regular expression to run only some test functions, or leave it empty to run all of them. A card shows the exact `SELECT * FROM runtests(…);` statement first; **Cancel** has the focus. The tests run on the connection's session, so connect first, and commit or roll back an open transaction. pgTAP must be installed in the database (`CREATE EXTENSION pgtap;`).

The results open in the **Testing** view under a **pgTAP** node, with one row per connection and one row per test function (✓, ✗ or skipped). A failed test shows the failing assertion and its `have` / `want` lines. Enter on a test shows its TAP output; Run on the pgTAP node, a connection or a test runs it again (one test runs as `runtests('schema'::name, '^name$'::text)`). The results stay when the Testing view refreshes its file tests. **Rerun Failed** runs only file tests.

## SQL history

Press `h` on the result page, `H` in the Databases view, or use **Databases: History…**. The newest run is at the top; `Enter` runs it again (writes ask again). **Clear history…** asks before it deletes. The history is `db/history.jsonl` in your user configuration (mode 0600): passwords are removed, each entry keeps at most 4 KB of SQL, and `db_history_max` (default 500) limits the count. **Databases: History of This Connection…** shows only the runs of the console's connection; type in either picker to search the SQL.

## Saved queries and live templates

**Databases: Save Query…** saves the statement at the cursor under a name (the same name on the same connection replaces it). **Databases: Saved Queries…** lists them; `Enter` puts the query below the cursor of a console of that connection, or opens its console. **Delete a saved query…** removes one. They are kept in `db/saved-queries.json` in your user configuration (mode 0600), with passwords removed.

In a SQL buffer, completion also offers live templates: type `sel`, `ins`, `upd`, `del`, `jn` or `cnt` and pick the *template* item; the cursor goes where the table name belongs (`SELECT * FROM |;`).

Nothing polls while you wait: an idle console or result page uses no CPU. Hiding the Databases view (another view, a hidden sidebar or zen mode) cancels a running query and closes its sessions, except a session with an open manual transaction. The command palette and other dialogs do not.

## Redis and Valkey

A Redis or Valkey connection (`"driver": "redis"`) takes a host, a port (6379 by default), an optional ACL `user`, a database index `db`, and a password from the secret store. Use `"tls": true` or a `rediss://` URL for TLS; `"ssl": {"ca": "…", "cert": "…", "key": "…"}` adds a CA and a client certificate. Containers and compose services built from `redis`, `valkey`, `keydb` or `dragonfly` images, and `REDIS_URL` in `.env`, are suggested.

Expanding a connection lists its databases (`db0 (1234 keys)`). Expanding a database lists its keys with `SCAN`, grouped into namespaces by `db_redis_separator` (`:` by default): `session:ab12` sits under `session:`. The app never sends `KEYS`. One row loads at most `db_redis_max_keys` keys (10000); past that the row says so, and opening a namespace scans only its keys. Each key shows its type: `"` string, `{` hash, `[` list, `(` set, `⇅` sorted set, `≋` stream (ASCII: `s h l S z x`).

`Enter` on a key opens its value page: the type, length, TTL and memory, then one page of the value (`]` and `[` move between pages; `db_page_rows` items per page). A whole value is never loaded: strings show their first 64 KB, other types load page by page. Values are shown safely: bytes that are not text and control characters appear as `\xHH`.

On the value page, `e` edits the value or the field under the cursor, `+` adds a field, item, member or entry, `-` deletes the one under the cursor, `t` sets the TTL (seconds or `30m`, `2h`, `1d`; empty removes it) and `D` deletes the key. Every change shows a card with the exact command, such as `HSET session:ab12 plan pro`, before it runs. On a production connection the card says so; on a read-only connection it asks **Run anyway?**. In the tree, `t`, `D` and `y` (copy the key name) work on a key row. `M` renames a key and `C` copies it to a new key (also **Databases: Rename Redis Key…** and **Databases: Copy Redis Key…** in the palette, on the open key or the selected row). The card shows `RENAMENX` or `COPY`: an existing key with the new name is never replaced, and the app says so when the name is taken. `D` on a namespace row deletes every key under it: the keys are found with `SCAN`, a card shows how many and a few of their names, and they are removed in batches with `UNLINK` (`DEL` on servers older than 4.0). If a namespace has more keys than one scan loads, run it again for the rest.

Press `n` on a Redis connection to open its console, a `.redis` file that starts with `# @conn <id>`. Write one command per line and press the send key on a line (or select several lines). Read commands run at once; other commands show a card first. Commands that can wipe data, stop the server or run code (`FLUSHALL`, `FLUSHDB`, `CONFIG SET`, `SHUTDOWN`, `DEBUG`, `MODULE`, `EVAL`, `FCALL`, `MIGRATE`, `REPLICAOF`, `ACL SETUSER`, …) show a strong warning card. `MONITOR`, `SUBSCRIBE`, `SELECT` and `MULTI` are not sent. `KEYS pattern` runs as `SCAN` with a note. An `AUTH` line is masked in the card, the page and the history. Console runs go to the same history as SQL.

**Databases: Show Server Info** on a Redis connection runs `INFO` and shows it as a section, field and value grid (server, clients, memory, persistence, stats, replication, keyspace). **Databases: Show Sessions and Locks** runs `CLIENT LIST`. **Databases: Show Redis Slow Log** runs `SLOWLOG GET 128` and shows each slow command with its id, time, duration in µs, client and name. Typing `INFO` or `SLOWLOG GET` in the console gives the same grids.

## AI tools and their rules

The AI chat has database tools: `db_connections`, `db_schema`, `db_ddl`, `db_query`, `db_explain`, `db_history` and `db_search_data`. The system prompt mentions them only when the workspace has connections.

- **Reads run at once.** Schema, DDL, plans and a read (`SELECT`, `SHOW`, `EXPLAIN` without `ANALYZE` …) run in a separate read-only session: `BEGIN READ ONLY` with a statement timeout on PostgreSQL, `START TRANSACTION READ ONLY` on MySQL / MariaDB, and `sqlite3 -readonly` for SQLite. The server or the file enforces it, not a text check. SQLite needs `sqlite3` 3.37 or newer for this.
- **Every other statement asks.** The card shows the connection and the exact SQL (Up/Down scrolls a long one). **Deny** comes first and has the focus, so a stray `Enter` writes nothing; **Allow** is the red button. **Always allow** for `db:<conn>/write` lets later writes on that connection run without asking; you can turn it off again in **AI: Agent Auto-Approve…**.
- **Production connections** (`"environment": "production"`) always ask before a write, with no "Always allow". **Read-only connections** (`"readOnly": true`) refuse AI writes.
- **Own sessions.** Each AI call opens its own session and closes it after. The AI never runs on your session and never joins your transaction. It cannot add or change connections or set passwords.
- **What you see.** Each `db_query` and `db_explain` run opens the SQL Result page marked **by AI** and adds an entry to the SQL history. Every call is in the AI audit log.
- **What the model sees.** Passwords never reach it. Server text is cleaned, cells are cut at 80 characters, and one answer is at most `db_ai_max_kb` KB (default 32); `max_rows` (default 100, max 1000) limits rows.
- **Redis.** `redis_keys` lists keys with `SCAN` (type and TTL), `redis_get` reads one key a page at a time, and `redis_command` runs one command. Read commands run at once in a read-only session that refuses anything else; `CONFIG`, `ACL` and `CLIENT` always ask because they can show secrets. Any write asks with the exact command (`db:<conn>/write`; production always asks; read-only connections refuse). Dangerous commands (`FLUSHALL`, `CONFIG SET`, scripts, `MIGRATE`, …) and `MONITOR` / `SUBSCRIBE` are refused without a card.
- **Outside agents** (Claude Code, Codex, … through termocode's MCP server) get less: the lists and schema at once, reads only with a card each time, and no writes at all. See [Give your agent the IDE's tools](agents.md#databases-and-http).

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
