package dbclient

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Dump and restore (DBX-19) with the server's own tools: pg_dump /
// pg_restore / psql and mysqldump / mysql, looked up on PATH by the
// caller. The connection flags come from CLICommand, so the password and
// SSL settings stay in Env, never in Args. Nothing here starts a process.

// DumpTool is the dump program of a driver, "" when there is none.
func DumpTool(driver string) string {
	switch normDriver(driver) {
	case "postgres", "postgresql":
		return "pg_dump"
	case "mysql":
		return "mysqldump"
	}
	return ""
}

// dumpBase is the client command of c without the read-only options a
// dump or restore does not want.
func dumpBase(c Conn, o CLIOptions) (CLICmd, error) {
	if DumpTool(c.Driver) == "" {
		return CLICmd{}, fmt.Errorf("no dump tool for %s", c.Driver)
	}
	c.ReadOnly = false // the read-only options are for an interactive client
	cmd, err := CLICommand(c, o)
	if err != nil {
		return cmd, err
	}
	db := c.Database
	if c.URL != "" {
		if p, err := ParseURL(c.URL); err == nil && p.Database != "" {
			db = p.Database
		}
	}
	if strings.TrimSpace(db) == "" {
		return cmd, errors.New("the connection names no database")
	}
	return cmd, nil
}

func checkDumpFile(file string) error {
	if strings.TrimSpace(file) == "" || strings.ContainsAny(file, "\x00\r\n") {
		return errors.New("the file name is not valid")
	}
	return nil
}

// isPlainSQL: a .sql file is plain statements, any other a custom archive.
func isPlainSQL(file string) bool { return strings.EqualFold(filepath.Ext(file), ".sql") }

// DumpCommand writes the whole database of c into file: PostgreSQL in its
// custom format (plain SQL for a .sql file), MySQL as SQL.
func DumpCommand(c Conn, o CLIOptions, file string) (CLICmd, error) {
	if err := checkDumpFile(file); err != nil {
		return CLICmd{}, err
	}
	cmd, err := dumpBase(c, o)
	if err != nil {
		return cmd, err
	}
	switch cmd.Tool {
	case "psql":
		cmd.Tool = "pg_dump"
		format := "-Fc"
		if isPlainSQL(file) {
			format = "-Fp"
		}
		cmd.Args = append(cmd.Args, format, "--no-password", "-f", file)
	case "mysql":
		cmd.Tool = "mysqldump"
		db := cmd.Args[len(cmd.Args)-1]
		cmd.Args = append(cmd.Args[:len(cmd.Args)-1:len(cmd.Args)-1], "--single-transaction", "--routines", "--triggers", "--result-file="+file, db)
	}
	return cmd, nil
}

// RestoreCommand loads file into the database of c: pg_restore for a
// PostgreSQL archive, psql (stop at the first error) for a .sql file,
// mysql with the file on standard input for MySQL. A read-only connection
// never restores.
func RestoreCommand(c Conn, o CLIOptions, file string) (CLICmd, error) {
	if c.ReadOnly {
		return CLICmd{}, errors.New("the connection is read-only")
	}
	if err := checkDumpFile(file); err != nil {
		return CLICmd{}, err
	}
	cmd, err := dumpBase(c, o)
	if err != nil {
		return cmd, err
	}
	switch cmd.Tool {
	case "psql":
		if isPlainSQL(file) {
			cmd.Args = append(cmd.Args, "--no-password", "-v", "ON_ERROR_STOP=1", "-1", "-f", file)
			return cmd, nil
		}
		cmd.Tool = "pg_restore"
		cmd.Args = append(cmd.Args, "--no-password", "--no-owner", "--exit-on-error", file)
	case "mysql":
		if !isPlainSQL(file) {
			return CLICmd{}, errors.New("MySQL restores a .sql file")
		}
		cmd.Stdin = file
	}
	return cmd, nil
}
