package dbclient

import (
	"fmt"
	"path/filepath"
	"strconv"
)

// CLICmd is the command line client for one connection (DBX-45). The
// password and SSL settings are in Env, never in Args, so they do not
// show in the process list. Nothing here starts a process.
type CLICmd struct {
	Tool string   // psql, mysql, sqlite3 or redis-cli (looked up on PATH by the caller)
	Args []string // arguments after Tool
	Env  []string // KEY=value to add to the environment
	// Stdin is a file the tool reads on standard input ("" = none): a
	// MySQL restore feeds the dump file to mysql this way.
	Stdin string
}

// CLIOptions change the command. Host/Port, when set, replace the
// configured address (an SSH tunnel's local end).
type CLIOptions struct {
	Password string
	Host     string
	Port     int
	Root     string // workspace root for a relative SQLite path
}

// CLITool is the client program for a driver, "" when there is none.
func CLITool(driver string) string {
	switch normDriver(driver) {
	case "postgres", "postgresql":
		return "psql"
	case "mysql":
		return "mysql"
	case "sqlite":
		return "sqlite3"
	case "redis":
		return "redis-cli"
	}
	return ""
}

// CLICommand builds the client command for c.
func CLICommand(c Conn, o CLIOptions) (CLICmd, error) {
	tool := CLITool(c.Driver)
	if tool == "" {
		return CLICmd{}, fmt.Errorf("no command line client for %s", c.Driver)
	}
	if c.URL != "" && tool != "sqlite3" {
		if p, err := ParseURL(c.URL); err == nil {
			c.Host, c.Port, c.User, c.Database = p.Host, p.Port, p.User, p.Database
			if p.DB != 0 {
				c.DB = p.DB
			}
		}
	}
	if c.Port == 0 {
		c.Port = defaultPort(normDriver(c.Driver))
	}
	if o.Host != "" {
		c.Host = o.Host
	}
	if o.Port != 0 {
		c.Port = o.Port
	}
	out := CLICmd{Tool: tool}
	env := func(k, v string) {
		if v != "" {
			out.Env = append(out.Env, k+"="+v)
		}
	}
	arg := func(flag, v string) {
		if v != "" {
			out.Args = append(out.Args, flag, v)
		}
	}
	port := ""
	if c.Port != 0 {
		port = strconv.Itoa(c.Port)
	}
	switch tool {
	case "psql":
		arg("-h", c.Host)
		arg("-p", port)
		arg("-U", c.User)
		arg("-d", c.Database)
		env("PGPASSWORD", o.Password)
		if c.SSL != nil {
			env("PGSSLMODE", c.SSL.Mode)
			env("PGSSLROOTCERT", c.SSL.CA)
			env("PGSSLCERT", c.SSL.Cert)
			env("PGSSLKEY", c.SSL.Key)
		}
		if c.ReadOnly {
			env("PGOPTIONS", "-c default_transaction_read_only=on")
		}
	case "mysql":
		out.Args = append(out.Args, "--protocol=TCP")
		arg("-h", c.Host)
		arg("-P", port)
		arg("-u", c.User)
		if c.ReadOnly {
			out.Args = append(out.Args, "--init-command=SET SESSION TRANSACTION READ ONLY")
		}
		if c.Database != "" {
			out.Args = append(out.Args, c.Database)
		}
		env("MYSQL_PWD", o.Password)
	case "sqlite3":
		p := c.Path
		if p == "" {
			return CLICmd{}, fmt.Errorf("sqlite path is required")
		}
		if !filepath.IsAbs(p) && o.Root != "" {
			p = filepath.Join(o.Root, p)
		}
		if c.ReadOnly {
			out.Args = append(out.Args, "-readonly")
		}
		out.Args = append(out.Args, p)
	case "redis-cli":
		arg("-h", c.Host)
		arg("-p", port)
		arg("--user", c.User)
		if c.DB != 0 {
			out.Args = append(out.Args, "-n", strconv.Itoa(c.DB))
		}
		if c.TLS {
			out.Args = append(out.Args, "--tls")
		}
		env("REDISCLI_AUTH", o.Password)
	}
	return out, nil
}
