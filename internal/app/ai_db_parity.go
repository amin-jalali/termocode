package app

// DB parity: which AI tool does each database action of the GUI (the
// palette ids db-*, redis-*, containers-open-databases and the web ops
// db.*, redis.*, containers.open_db). TestAIDBEveryGUIActionHasAnAITool
// fails when an action is on neither list. The value of aiDBParity is the
// tool (first word, a chat tool) and how it does it.

// aiDBParity maps a GUI action to the tool (and op) that does it.
var aiDBParity = map[string]string{
	// Connections (db_connection).
	"db-add":                    "db_connection op add",
	"db-new-from-template":      "db_connection op add",
	"db-manage-connection":      "db_connection op edit / delete",
	"db-add-tunnel":             "db_connection op ssh",
	"db-ssl":                    "db_connection op ssl",
	"db-connect":                "db_connection op connect",
	"db-disconnect":             "db_connection op disconnect",
	"db-test-connection":        "db_connection op test",
	"containers-open-databases": "db_connection op open_container",
	"containers.open_db":        "db_connection op open_container",
	"db.add":                    "db_connection op add",
	"db.add_container":          "db_connection op open_container",
	"db.config":                 "db_connection op add (writes .termocode/databases.json)",
	"db.ssh":                    "db_connection op ssh",
	"db.ssl":                    "db_connection op ssl",
	"db.connect":                "db_connection op connect",
	"db.disconnect":             "db_connection op disconnect",
	"db.test":                   "db_connection op test",
	"db.conns":                  "db_connections",

	// The user's session (db_session).
	"db-commit":             "db_session op commit",
	"db-rollback":           "db_session op rollback",
	"db-transaction-mode":   "db_session op tx_mode",
	"db-savepoint":          "db_session op savepoint",
	"db-rollback-savepoint": "db_session op rollback_savepoint",
	"db-cancel":             "db_session op cancel",
	"db-open-sessions":      "db_session op status",
	"db.tx":                 "db_session op commit / rollback / tx_mode",
	"db.cancel":             "db_session op cancel",

	// Data in and out (db_data).
	"db-export":           "db_data op export",
	"db-export-tables":    "db_data op export (one file per table)",
	"db-run-to-file":      "db_data op export",
	"db-import-csv":       "db_data op import_csv",
	"db-import-new-table": "db_query (CREATE TABLE) + db_data op import_csv",
	"db-dump":             "db_data op dump",
	"db-restore":          "db_data op restore",
	"db-run-dump":         "db_data op restore",
	"db.import":           "db_data op import_csv",

	// SQL (db_query: a read at once, anything else asks with the SQL).
	"db-run":             "db_query",
	"db-run-file":        "db_query (one statement per call)",
	"db-run-from-caret":  "db_query",
	"db-run-smallest":    "db_query",
	"db-run-ask":         "db_query",
	"db-select":          "db_query",
	"db-data":            "db_query",
	"db-count-rows":      "db_query",
	"db-result-load-all": "db_query (max_rows)",
	"db-sessions":        "db_query",
	"db-server-info":     "db_query",
	"db-roles":           "db_query",
	"db-role-grants":     "db_query",
	"db-role-create":     "db_query (CREATE ROLE asks)",
	"db-role-alter":      "db_query (ALTER ROLE asks)",
	"db-grant":           "db_query (GRANT asks)",
	"db-revoke":          "db_query (REVOKE asks)",
	"db-create-table":    "db_query (DDL asks)",
	"db-alter-table":     "db_query (DDL asks)",
	"db-object-actions":  "db_query (DROP / TRUNCATE ask)",
	"db-copy-table-data": "db_query (a read, then INSERT on the other connection, which asks)",
	"db.query":           "db_query",
	"db.exec":            "db_query",
	"db.rows":            "db_query",
	"db.edit":            "db_query (UPDATE asks)",

	// Schema, plans, history, search.
	"db-explain":            "db_explain",
	"db-explain-analyze":    "db_explain analyze",
	"db.explain":            "db_explain",
	"db-history":            "db_history",
	"db-history-connection": "db_history conn",
	"db.history":            "db_history",
	"db-search-data":        "db_search_data",
	"db-filter":             "db_schema",
	"db-find-object":        "db_schema",
	"db-compare-schemas":    "db_schema (both sides)",
	"db.compare":            "db_schema (both sides)",
	"db-er-path":            "db_schema (foreign keys)",
	"db.tables":             "db_schema",
	"db.ddl":                "db_ddl",
	"db.csv_files":          "glob",

	// SQL files in the editor.
	"db-rename":         "edit_file",
	"db-sql-intentions": "edit_file",
	"db-sql-outline":    "read_file",
	"db-embedded-sql":   "search",
	"db-open-csv":       "read_file",

	// Palette actions execute_action runs (ai_actions_policy.go).
	"db-another-console":     "execute_action",
	"db-configure":           "execute_action",
	"db-ddl":                 "execute_action",
	"db-dependencies":        "execute_action",
	"db-describe":            "execute_action",
	"db-edit-source":         "execute_action",
	"db-er-text":             "execute_action",
	"db-find-usages":         "execute_action",
	"db-focus":               "execute_action",
	"db-format":              "execute_action",
	"db-format-file":         "execute_action",
	"db-goto-definition":     "execute_action",
	"db-guide":               "execute_action",
	"db-heatmap":             "execute_action",
	"db-highlight-usages":    "execute_action",
	"db-inline-clear":        "execute_action",
	"db-insert-hints":        "execute_action",
	"db-join-hints":          "execute_action",
	"db-param-info":          "execute_action",
	"db-quick-doc":           "execute_action",
	"db-quick-fix":           "execute_action",
	"db-refresh":             "execute_action",
	"db-refresh-object":      "execute_action",
	"db-result-column-types": "execute_action",
	"db-result-pin":          "execute_action",
	"db-result-row-numbers":  "execute_action",
	"db-schema-cache-info":   "execute_action",

	// Redis (redis_keys reads keys; redis_command runs a command, a write asks).
	"redis.keys":            "redis_keys",
	"redis-search-keys":     "redis_keys pattern",
	"redis.key":             "redis_get / redis_command",
	"redis-set-ttl":         "redis_command (EXPIRE)",
	"redis-delete-key":      "redis_command (DEL)",
	"redis-rename-key":      "redis_command (RENAME)",
	"redis-copy-key":        "redis_command (COPY)",
	"redis-new-key":         "redis_command (SET, HSET …)",
	"redis-json-path":       "redis_command (JSON.GET)",
	"redis-key-module-info": "redis_command (TS.INFO, BF.INFO …)",
	"redis-modules":         "redis_command (MODULE LIST)",
	"redis-search-indexes":  "redis_command (FT._LIST)",
	"redis-memory":          "redis_command (MEMORY STATS)",
	"redis-pubsub-channels": "redis_command (PUBSUB CHANNELS)",
	"redis-stream-groups":   "redis_command (XINFO GROUPS)",
	"redis-cluster-nodes":   "redis_command (CLUSTER NODES)",
	"redis-slowlog":         "redis_command (SLOWLOG GET)",
}

// aiDBNotForAI are the GUI actions the AI does not get, each with why.
var aiDBNotForAI = map[string]string{
	// The AI is the chat: these ask the chat itself.
	"db-ai-explain-query": "the AI is the chat: it explains a query when asked",
	"db-ai-fix-error":     "the AI is the chat: it fixes a query error when asked",
	"db-ai-generate-sql":  "the AI is the chat: it writes SQL from db_schema when asked",

	// Secrets and trust stay with the user.
	"db.secret":             "a password or passphrase is typed by the user; the AI never holds one (connect asks the user)",
	"db.trust_host":         "only the user trusts an SSH host key; connect shows the key and the user answers",
	"db-import-connections": "reads the user's password files of other tools",
	"db-paste-connection":   "reads the user's clipboard, which may hold a password",
	"db-iam-auth":           "makes a connection run a command at each connect",
	"db-cloud-discover":     "runs the user's cloud CLI with their account",
	"db-query-log":          "the log holds the literal values of every statement",

	// Other people's sessions.
	"db-cancel-session": "stops another session's work on the server; only the user chooses it",
	"db-end-session":    "ends another session and rolls back its transaction; only the user chooses it",

	// The user's own pending edits and editor state.
	"db-submit-changes":     "the user's own pending grid edits; the AI writes with db_query",
	"db-revert-changes":     "the user's own pending grid edits",
	"db-pending-sources":    "the user's own pending object sources",
	"db-submit-source":      "the user's own edited object source; the AI changes objects with db_query (DDL asks)",
	"db-console-session":    "picks which session the user's console uses; db_query has its own",
	"db-switch-connection":  "binds the user's SQL file to a connection in the editor; db_query takes conn on each call",
	"db-sql-folder-dialect": "sets the editor's SQL dialect of a folder for the user",
	"db-new-console":        "opens a console tab for the user; the AI runs SQL with db_query",
	"db-console-history":    "local history of the user's console buffer",
	"db-virtual-objects":    "opens the user's virtual objects file in the editor for the user",
	"db-saved-runs":         "a saved run plays SQL and shell steps from a project file; only the user starts it",
	"db-save-query":         "the user's saved query list; the AI keeps SQL in files (write_file)",
	"db-saved-queries":      "the user's saved query list; the AI keeps SQL in files (write_file)",

	// The result page: display and clipboard only.
	"db-chart":               "a chart of the user's result page",
	"db-copy-as":             "writes the user's clipboard",
	"db-result-create-table": "writes the user's clipboard",
	"db-result-beside":       "a menu of the user's result page",
	"db-result-cell-menu":    "a menu of the user's result page",
	"db-result-goto-row":     "moves the user's cursor on the result page",
	"db-result-page-size":    "a display setting of the user's result page",
	"db-exec-mode":           "how the user's console runs a selection; db_query runs one statement",
	"db-er-diagram":          "an image file for the user; the AI reads the schema with db_schema",

	// Runs only the user starts.
	"db-generate-data":  "it writes a script of rows the user reviews and runs; the AI writes SQL with db_query",
	"db-pgtap-run":      "runs the user's test functions after a card only the user answers",
	"db-run-container":  "starts a new database container; containers are the containers area's piece",
	"redis-listen":      "reads live messages of the user's channels until the user stops it",
	"redis-export-keys": "writes key values to a file after a card only the user answers",
}
