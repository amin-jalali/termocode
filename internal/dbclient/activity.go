package dbclient

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Sessions, locks and server info (DBX-17). Each query is one line, so
// it can be written into a console and run like any statement. They are
// plain reads; stopping a session is not, so the run card asks first.

const pgSessionsSQL = `SELECT pid, usename AS "user", datname AS "database", application_name AS app, client_addr::text AS client, state, wait_event_type AS wait, pg_blocking_pids(pid)::text AS blocked_by, date_trunc('second', now() - query_start)::text AS running_for, left(query, 200) AS query FROM pg_stat_activity WHERE pid <> pg_backend_pid() ORDER BY query_start NULLS LAST;`

const mysqlSessionsSQL = "SELECT ID AS id, USER AS user, HOST AS host, DB AS `database`, COMMAND AS command, TIME AS seconds, STATE AS state, LEFT(INFO, 200) AS query FROM information_schema.PROCESSLIST WHERE ID <> CONNECTION_ID() ORDER BY TIME DESC;"

const pgServerInfoSQL = `SELECT version() AS version, current_database() AS "database", current_user AS "user", inet_server_addr()::text AS host, inet_server_port() AS port, current_setting('server_encoding') AS encoding, current_setting('TimeZone') AS timezone, pg_size_pretty(pg_database_size(current_database())) AS size, date_trunc('second', now() - pg_postmaster_start_time())::text AS uptime;`

const mysqlServerInfoSQL = "SELECT VERSION() AS version, DATABASE() AS `database`, CURRENT_USER() AS user, @@hostname AS host, @@port AS port, @@character_set_server AS charset, @@time_zone AS timezone, @@max_connections AS max_connections;"

const sqliteServerInfoSQL = `SELECT sqlite_version() AS version;`

// SessionsSQL lists the other sessions of the server (PostgreSQL also
// shows which sessions block each one). ok=false: the driver has none.
func SessionsSQL(driver string) (string, bool) {
	switch normDriver(driver) {
	case "postgres", "postgresql":
		return pgSessionsSQL, true
	case "mysql":
		return mysqlSessionsSQL, true
	}
	return "", false
}

// ServerInfoSQL shows the server version, database, user and settings.
func ServerInfoSQL(driver string) (string, bool) {
	switch normDriver(driver) {
	case "postgres", "postgresql":
		return pgServerInfoSQL, true
	case "mysql":
		return mysqlServerInfoSQL, true
	case "sqlite":
		return sqliteServerInfoSQL, true
	}
	return "", false
}

// StopSessionSQL cancels the running query of session id, or (end) ends
// the whole session.
func StopSessionSQL(driver string, id int64, end bool) (string, error) {
	if id <= 0 {
		return "", errors.New("the session id must be a positive number")
	}
	switch normDriver(driver) {
	case "postgres", "postgresql":
		if end {
			return fmt.Sprintf("SELECT pg_terminate_backend(%d);", id), nil
		}
		return fmt.Sprintf("SELECT pg_cancel_backend(%d);", id), nil
	case "mysql":
		if end {
			return fmt.Sprintf("KILL %d;", id), nil
		}
		return fmt.Sprintf("KILL QUERY %d;", id), nil
	}
	return "", errors.New("this connection has no sessions to stop")
}

// ParseSessionID reads a session id typed by the user ("42" or "#42").
func ParseSessionID(s string) (int64, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("type the session id (a positive number, the pid / id column)")
	}
	return id, nil
}
