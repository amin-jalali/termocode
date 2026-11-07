package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"text/tabwriter"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/jsonc"
)

// `termocode db` (DBX-50): your database connections without the UI.
// Passwords are never printed; a pasted literal password is dropped.

const dbUsage = `termocode db: manage your database connections

  termocode db list            list your connections
  termocode db show <id>       print one connection as JSON (no password)
  termocode db add             add a connection read as JSON from stdin
  termocode db remove <id>     remove a connection

The connections are in $XDG_CONFIG_HOME/termocode/db/connections.json
(~/.config/termocode/... when it is not set). Project connections in
.termocode/databases.json are not changed.
`

func runDB(args []string) int {
	return runDBIn(args, os.Stdin, os.Stdout, os.Stderr)
}

func dbConnectionsPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "termocode", "db", "connections.json"), nil
}

func dbReadConnections(path string) ([]dbclient.Conn, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f struct {
		Connections []dbclient.Conn `json:"connections"`
	}
	if err := json.Unmarshal(jsonc.Strip(b), &f); err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	for i := range f.Connections {
		f.Connections[i].Source = dbclient.SourceUser
	}
	return f.Connections, nil
}

func dbWriteConnections(path string, cs []dbclient.Conn) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(struct {
		Connections []dbclient.Conn `json:"connections"`
	}{cs}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

func dbTarget(c dbclient.Conn) string {
	switch {
	case c.Path != "":
		return c.Path
	case c.Kube != nil:
		return "kube:" + c.Kube.Pod
	case c.URL != "":
		return "url"
	}
	return c.Host + ":" + strconv.Itoa(c.Port)
}

func runDBIn(args []string, in io.Reader, out, errw io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(errw, dbUsage)
		return 2
	}
	fail := func(err error) int {
		fmt.Fprintf(errw, "termocode db: %v\n", err)
		return 1
	}
	path, err := dbConnectionsPath()
	if err != nil {
		return fail(err)
	}
	needID := func() (string, bool) {
		if len(args) != 2 || args[1] == "" {
			fmt.Fprintf(errw, "termocode db %s: give one connection id\n", args[0])
			return "", false
		}
		return args[1], true
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(out, dbUsage)
		return 0
	case "list":
		cs, err := dbReadConnections(path)
		if err != nil {
			return fail(err)
		}
		if len(cs) == 0 {
			fmt.Fprintln(out, "no connections")
			return 0
		}
		tw := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "ID\tDRIVER\tTARGET\tNAME")
		for _, c := range cs {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", c.ID, c.Driver, dbTarget(c), c.Name)
		}
		_ = tw.Flush()
		return 0
	case "show":
		id, ok := needID()
		if !ok {
			return 2
		}
		cs, err := dbReadConnections(path)
		if err != nil {
			return fail(err)
		}
		for _, c := range cs {
			if c.ID == id {
				text, err := dbclient.ConnJSON(c)
				if err != nil {
					return fail(err)
				}
				fmt.Fprintln(out, text)
				return 0
			}
		}
		return fail(fmt.Errorf("no connection %q", id))
	case "add":
		b, err := io.ReadAll(io.LimitReader(in, 1<<20))
		if err != nil {
			return fail(err)
		}
		cs, err := dbReadConnections(path)
		if err != nil {
			return fail(err)
		}
		c, err := dbclient.ParseConnJSON(string(b), cs)
		if err != nil {
			return fail(err)
		}
		if err := dbWriteConnections(path, append(cs, c)); err != nil {
			return fail(err)
		}
		fmt.Fprintf(out, "added %s\n", c.ID)
		return 0
	case "remove", "rm":
		id, ok := needID()
		if !ok {
			return 2
		}
		cs, err := dbReadConnections(path)
		if err != nil {
			return fail(err)
		}
		left, err := dbclient.DeleteConn(cs, id)
		if err != nil {
			return fail(err)
		}
		if err := dbWriteConnections(path, left); err != nil {
			return fail(err)
		}
		fmt.Fprintf(out, "removed %s (its saved password stays in the secret store)\n", id)
		return 0
	}
	fmt.Fprint(errw, dbUsage)
	return 2
}
