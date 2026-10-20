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
