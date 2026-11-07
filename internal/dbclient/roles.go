package dbclient

import (
	"errors"
	"fmt"
	"strings"
)

// Users and roles (DBX-18): the catalog queries for the roles list and
// the grants view, and the DDL text for the preview card. Nothing here
// runs a statement; passwords are never part of the DDL.

// RoleQueries are the catalog queries of one dialect. Roles lists the
// roles; Grants lists the table grants of one role (PG: $1 = role name;
// MySQL: both ? = the grantee as MySQLGrantee writes it).
type RoleQueries struct {
	Roles  string
	Grants string
}

// RoleQueriesFor returns the queries for a driver; ok is false when the
// engine has no users or roles (sqlite, redis).
func RoleQueriesFor(driver string) (RoleQueries, bool) {
	switch normDriver(driver) {
	case "postgres", "postgresql":
		return RoleQueries{
			Roles:  `SELECT rolname, rolsuper, rolcanlogin, rolcreatedb, rolcreaterole, rolconnlimit, rolvaliduntil FROM pg_roles WHERE rolname NOT LIKE 'pg\_%' ORDER BY rolname`,
			Grants: `SELECT table_schema, table_name, privilege_type, is_grantable FROM information_schema.role_table_grants WHERE grantee = $1 ORDER BY table_schema, table_name, privilege_type`,
		}, true
	case "mysql":
		return RoleQueries{
			Roles:  "SELECT User, Host, account_locked, max_user_connections FROM mysql.user ORDER BY User, Host",
			Grants: "SELECT TABLE_SCHEMA, TABLE_NAME, PRIVILEGE_TYPE, IS_GRANTABLE FROM information_schema.TABLE_PRIVILEGES WHERE GRANTEE = ? UNION ALL SELECT TABLE_SCHEMA, '*', PRIVILEGE_TYPE, IS_GRANTABLE FROM information_schema.SCHEMA_PRIVILEGES WHERE GRANTEE = ? ORDER BY 1, 2, 3",
		}, true
	}
	return RoleQueries{}, false
}

// mysqlString quotes a MySQL string literal.
func mysqlString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// MySQLGrantee writes 'user'@'host' (host "" = '%').
func MySQLGrantee(user, host string) string {
	if host == "" {
		host = "%"
	}
	return mysqlString(user) + "@" + mysqlString(host)
}

// RoleSpec is a role to create or alter. ConnLimit -1 = no limit.
// Host is the MySQL account host ("" = %).
type RoleSpec struct {
	Name       string
	Host       string
	Login      bool
	Superuser  bool
	CreateDB   bool
	CreateRole bool
	ConnLimit  int
}

func checkName(kind, s string) error {
	if strings.TrimSpace(s) == "" {
		return fmt.Errorf("%s name is empty", kind)
	}
	if strings.ContainsAny(s, "\x00\r\n") {
		return fmt.Errorf("%s name has a control character", kind)
	}
	return nil
}

func flag(on bool, yes, no string) string {
	if on {
		return yes
	}
	return no
}

// RoleDDL is the CREATE (or ALTER) ROLE / USER statement for spec.
func RoleDDL(driver string, spec RoleSpec, alter bool) (string, error) {
	if err := checkName("role", spec.Name); err != nil {
		return "", err
	}
	if spec.ConnLimit < -1 {
		return "", errors.New("connection limit must be -1 (none) or more")
	}
	switch normDriver(driver) {
	case "postgres", "postgresql":
		verb := "CREATE"
		if alter {
			verb = "ALTER"
		}
		return fmt.Sprintf("%s ROLE %s WITH %s %s %s %s CONNECTION LIMIT %d;", verb,
			PostgresDialect{}.QuoteIdent(spec.Name),
			flag(spec.Login, "LOGIN", "NOLOGIN"), flag(spec.Superuser, "SUPERUSER", "NOSUPERUSER"),
			flag(spec.CreateDB, "CREATEDB", "NOCREATEDB"), flag(spec.CreateRole, "CREATEROLE", "NOCREATEROLE"),
			spec.ConnLimit), nil
	case "mysql":
		if spec.Superuser || spec.CreateDB || spec.CreateRole {
			return "", errors.New("MySQL accounts get rights with GRANT, not role flags")
		}
		if err := checkName("host", spec.Host+"%"); err != nil {
			return "", err
		}
		verb := "CREATE"
		if alter {
			verb = "ALTER"
		}
		var b strings.Builder
		b.WriteString(verb + " USER " + MySQLGrantee(spec.Name, spec.Host))
		if spec.ConnLimit >= 0 {
			fmt.Fprintf(&b, " WITH MAX_USER_CONNECTIONS %d", spec.ConnLimit)
		}
		switch {
		case !spec.Login:
			b.WriteString(" ACCOUNT LOCK")
		case alter:
			b.WriteString(" ACCOUNT UNLOCK")
		}
		b.WriteString(";")
		return b.String(), nil
	}
	return "", fmt.Errorf("%s has no users or roles", driver)
}

// GrantSpec is one GRANT (or REVOKE) on a table, or on every table of a
// schema (MySQL: database) when Table is "".
type GrantSpec struct {
	Role       string
	Host       string // MySQL account host ("" = %)
	Privileges []string
	Schema     string
	Table      string
	WithGrant  bool
	Revoke     bool
}

var (
	pgPrivileges    = map[string]bool{"SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true, "TRUNCATE": true, "REFERENCES": true, "TRIGGER": true, "ALL": true}
	mysqlPrivileges = map[string]bool{"SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true, "CREATE": true, "DROP": true, "ALTER": true, "INDEX": true, "REFERENCES": true, "TRIGGER": true, "ALL": true}
)

// GrantDDL is the GRANT / REVOKE statement for spec.
func GrantDDL(driver string, spec GrantSpec) (string, error) {
	if err := checkName("role", spec.Role); err != nil {
		return "", err
	}
	if err := checkName("schema", spec.Schema); err != nil {
		return "", err
	}
	if spec.Table != "" {
		if err := checkName("table", spec.Table); err != nil {
			return "", err
		}
	}
	if len(spec.Privileges) == 0 {
		return "", errors.New("no privilege chosen")
	}
	drv := normDriver(driver)
	allowed := pgPrivileges
	if drv == "mysql" {
		allowed = mysqlPrivileges
	} else if drv != "postgres" && drv != "postgresql" {
		return "", fmt.Errorf("%s has no grants", driver)
	}
	privs := make([]string, 0, len(spec.Privileges))
	for _, p := range spec.Privileges {
		p = strings.ToUpper(strings.TrimSpace(p))
		if !allowed[p] {
			return "", fmt.Errorf("unknown privilege %q", p)
		}
		if p == "ALL" {
			p = "ALL PRIVILEGES"
		}
		privs = append(privs, p)
	}
	verb, prep := "GRANT", "TO"
	if spec.Revoke {
		verb, prep = "REVOKE", "FROM"
	}
	var on, who string
	if drv == "mysql" {
		q := MySQLDialect{}.QuoteIdent
		on = q(spec.Schema) + ".*"
		if spec.Table != "" {
			on = q(spec.Schema) + "." + q(spec.Table)
		}
		who = MySQLGrantee(spec.Role, spec.Host)
	} else {
		q := PostgresDialect{}.QuoteIdent
		on = "ALL TABLES IN SCHEMA " + q(spec.Schema)
		if spec.Table != "" {
			on = "TABLE " + q(spec.Schema) + "." + q(spec.Table)
		}
		who = q(spec.Role)
	}
	s := fmt.Sprintf("%s %s ON %s %s %s", verb, strings.Join(privs, ", "), on, prep, who)
	if spec.WithGrant && !spec.Revoke {
		s += " WITH GRANT OPTION"
	}
	return s + ";", nil
}

// pgString quotes a PostgreSQL string literal.
func pgString(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// GrantsSQL is the grants query of one role with the role written in as
// a literal, so it runs as a plain read in the console. host is the
// MySQL account host ("" = %).
func GrantsSQL(driver, role, host string) (string, error) {
	if err := checkName("role", role); err != nil {
		return "", err
	}
	if strings.ContainsAny(host, "\x00\r\n") {
		return "", errors.New("host has a control character")
	}
	q, ok := RoleQueriesFor(driver)
	if !ok {
		return "", fmt.Errorf("%s has no users or roles", driver)
	}
	if normDriver(driver) == "mysql" {
		lit := mysqlString(MySQLGrantee(role, host))
		return strings.ReplaceAll(q.Grants, "?", lit), nil
	}
	return strings.Replace(q.Grants, "$1", pgString(role), 1), nil
}

// ParseAccount splits a typed account: MySQL "user@host" (the last @),
// any other engine keeps the whole trimmed text as the role name.
func ParseAccount(driver, s string) (name, host string) {
	s = strings.TrimSpace(s)
	if normDriver(driver) == "mysql" {
		if i := strings.LastIndex(s, "@"); i > 0 {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}

// ParseGrantTarget reads "SELECT, INSERT on schema.table" (or "… on
// schema" for every table of the schema).
func ParseGrantTarget(s string) (privs []string, schema, table string, err error) {
	lower := strings.ToLower(s)
	i := strings.LastIndex(lower, " on ")
	if i < 0 {
		return nil, "", "", errors.New(`write it as "SELECT, INSERT on schema.table"`)
	}
	for _, p := range strings.Split(s[:i], ",") {
		if p = strings.ToUpper(strings.TrimSpace(p)); p != "" {
			privs = append(privs, p)
		}
	}
	if len(privs) == 0 {
		return nil, "", "", errors.New("no privilege chosen")
	}
	target := strings.TrimSpace(s[i+4:])
	schema, table, _ = strings.Cut(target, ".")
	schema, table = strings.TrimSpace(schema), strings.TrimSpace(table)
	if schema == "" {
		return nil, "", "", errors.New("schema name is empty")
	}
	return privs, schema, table, nil
}

// RolePreset is one choice of the create / alter role picker.
type RolePreset struct {
	ID    string
	Title string
	Hint  string
	Spec  RoleSpec // Name and Host are filled in by the caller
}

// RolePresets are the role kinds a driver offers; empty when it has no
// roles.
func RolePresets(driver string) []RolePreset {
	switch normDriver(driver) {
	case "postgres", "postgresql":
		return []RolePreset{
			{ID: "login", Title: "Login user", Hint: "LOGIN", Spec: RoleSpec{Login: true, ConnLimit: -1}},
			{ID: "group", Title: "Group role (no login)", Hint: "NOLOGIN", Spec: RoleSpec{ConnLimit: -1}},
			{ID: "admin", Title: "Admin user", Hint: "LOGIN CREATEDB CREATEROLE", Spec: RoleSpec{Login: true, CreateDB: true, CreateRole: true, ConnLimit: -1}},
			{ID: "superuser", Title: "Superuser", Hint: "LOGIN SUPERUSER: every right", Spec: RoleSpec{Login: true, Superuser: true, ConnLimit: -1}},
		}
	case "mysql":
		return []RolePreset{
			{ID: "login", Title: "Login user", Hint: "account unlocked; rights come from Grant", Spec: RoleSpec{Login: true, ConnLimit: -1}},
			{ID: "group", Title: "Locked account", Hint: "ACCOUNT LOCK", Spec: RoleSpec{ConnLimit: -1}},
		}
	}
	return nil
}
