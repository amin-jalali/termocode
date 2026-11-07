package dbclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// Cloud and containers (DBX-49): a new database container to run, the
// instances the installed cloud CLIs list, and IAM / token login through
// the cloud CLI. Nothing here runs a command; the app shows each command
// before it runs.

// DBContainerEngine is one engine "Run New Database Container…" offers.
type DBContainerEngine struct {
	ID, Title, Image, Driver, User string
	Port                           int
	PasswordEnv                    string
}

// DBContainerEngines are the engines, in picker order.
var DBContainerEngines = []DBContainerEngine{
	{ID: "postgres", Title: "PostgreSQL 17", Image: "postgres:17", Driver: "postgres", User: "postgres", Port: 5432, PasswordEnv: "POSTGRES_PASSWORD"},
	{ID: "mysql", Title: "MySQL 8.4", Image: "mysql:8.4", Driver: "mysql", User: "root", Port: 3306, PasswordEnv: "MYSQL_ROOT_PASSWORD"},
	{ID: "mariadb", Title: "MariaDB 11", Image: "mariadb:11", Driver: "mysql", User: "root", Port: 3306, PasswordEnv: "MARIADB_ROOT_PASSWORD"},
	{ID: "redis", Title: "Redis 7", Image: "redis:7", Driver: "redis", Port: 6379, PasswordEnv: "REDIS_PASSWORD"},
}

// DBContainer is a container to run and the connection to it.
type DBContainer struct {
	Engine  DBContainerEngine
	Image   string
	Name    string
	Ports   []string
	Env     map[string]string // the password: in the job's env, never argv
	Command []string
	Conn    Conn // saved without the password (the app keeps it in the secret store)
}

var suffixPattern = lazyre.New(`^[a-z0-9]{1,16}$`)

// NewDBContainer plans a container of engine published on
// 127.0.0.1:hostPort only. suffix makes the names unique.
func NewDBContainer(engine string, hostPort int, password, suffix string) (DBContainer, error) {
	var e DBContainerEngine
	for _, x := range DBContainerEngines {
		if x.ID == engine {
			e = x
		}
	}
	switch {
	case e.ID == "":
		return DBContainer{}, errors.New("unknown database engine " + sanitizeLine(engine))
	case !suffixPattern.MatchString(suffix):
		return DBContainer{}, errors.New("bad container name suffix")
	case hostPort < 1 || hostPort > 65535:
		return DBContainer{}, errors.New("bad host port")
	}
	c := DBContainer{
		Engine: e,
		Image:  e.Image,
		Name:   "termocode-" + e.ID + "-" + suffix,
		Ports:  []string{fmt.Sprintf("127.0.0.1:%d:%d", hostPort, e.Port)},
		Env:    map[string]string{e.PasswordEnv: password},
		Conn: Conn{ID: "container-" + e.ID + "-" + suffix, Name: e.Title + " container " + suffix, Driver: e.Driver,
			Host: "127.0.0.1", Port: hostPort, User: e.User, Source: SourceUser},
	}
	if e.ID == "redis" {
		// The image has no password variable: the shell inside reads it.
		c.Command = []string{"sh", "-c", `exec redis-server --requirepass "$REDIS_PASSWORD"`}
	}
	return c, nil
}

// CloudProvider is one cloud whose CLI can list database instances.
type CloudProvider struct{ ID, Title, CLI string }

// CloudProviders are the clouds, in picker order.
var CloudProviders = []CloudProvider{
	{ID: "aws", Title: "AWS RDS and Aurora", CLI: "aws"},
	{ID: "gcp", Title: "Google Cloud SQL", CLI: "gcloud"},
	{ID: "azure", Title: "Azure Database (flexible servers)", CLI: "az"},
}

// CloudListCommands are the argv lists that list provider's instances as
// JSON (each output goes to ParseCloudInstances).
func CloudListCommands(provider string) [][]string {
	switch provider {
	case "aws":
		return [][]string{{"aws", "rds", "describe-db-instances", "--output", "json"}}
	case "gcp":
		return [][]string{{"gcloud", "sql", "instances", "list", "--format=json"}}
	case "azure":
		return [][]string{
			{"az", "postgres", "flexible-server", "list", "--output", "json"},
			{"az", "mysql", "flexible-server", "list", "--output", "json"},
		}
	}
	return nil
}

// CloudInstance is one instance found, as a connection to add.
type CloudInstance struct {
	Conn   Conn
	Detail string // engine and region, for the picker
}

// cloudID is a connection id: provider-name in the id alphabet.
func cloudID(provider, name string) string {
	b := []rune(strings.ToLower(name))
	for i, r := range b {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			b[i] = '-'
		}
	}
	return provider + "-" + strings.Trim(string(b), "-")
}

// cloudDriver maps a cloud engine name to a driver ("" unsupported).
func cloudDriver(engine string) string {
	e := strings.ToLower(engine)
	switch {
	case strings.Contains(e, "postgres"):
		return "postgres"
	case strings.Contains(e, "mysql"), strings.Contains(e, "mariadb"):
		return "mysql"
	}
	return ""
}

// ParseCloudInstances reads a list command's JSON. Engines termocode
// cannot open are left out.
func ParseCloudInstances(provider string, data []byte) ([]CloudInstance, error) {
	var out []CloudInstance
	add := func(name, engine, host string, port int, user, db, detail string) {
		d := cloudDriver(engine)
		if d == "" || host == "" {
			return
		}
		if port == 0 {
			port = defaultPort(d)
		}
		c := Conn{ID: cloudID(provider, name), Name: sanitizeLine(name), Driver: d, Host: sanitizeLine(host), Port: port,
			User: sanitizeLine(user), Database: sanitizeLine(db), Source: SourceUser, SSL: &SSLConfig{Mode: SSLRequire}}
		out = append(out, CloudInstance{Conn: c, Detail: sanitizeLine(detail)})
	}
	switch provider {
	case "aws":
		var v struct {
			DBInstances []struct {
				DBInstanceIdentifier, Engine, MasterUsername, DBName, AvailabilityZone string
				Endpoint                                                               struct {
					Address string
					Port    int
				}
			}
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("aws output: %w", err)
		}
		for _, i := range v.DBInstances {
			add(i.DBInstanceIdentifier, i.Engine, i.Endpoint.Address, i.Endpoint.Port, i.MasterUsername, i.DBName, strings.TrimSpace(i.Engine+" "+i.AvailabilityZone))
		}
	case "gcp":
		var v []struct {
			Name, DatabaseVersion, Region string
			IPAddresses                   []struct{ Type, IPAddress string }
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("gcloud output: %w", err)
		}
		for _, i := range v {
			host := ""
			for _, ip := range i.IPAddresses {
				if ip.Type == "PRIMARY" || ip.Type == "PRIVATE" && host == "" {
					host = ip.IPAddress
				}
			}
			add(i.Name, i.DatabaseVersion, host, 0, "", "", strings.TrimSpace(i.DatabaseVersion+" "+i.Region))
		}
	case "azure":
		var v []struct {
			Name, FullyQualifiedDomainName, AdministratorLogin, Type, Location string
		}
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("az output: %w", err)
		}
		for _, i := range v {
			add(i.Name, i.Type, i.FullyQualifiedDomainName, 0, i.AdministratorLogin, "", strings.TrimSpace(i.Type+" "+i.Location))
		}
	default:
		return nil, errors.New("unknown cloud " + sanitizeLine(provider))
	}
	return out, nil
}

// iamWord is a host or user that needs no shell quoting.
var iamWord = lazyre.New(`^[A-Za-z0-9][A-Za-z0-9._@-]*$`)

// IAMTokenRef is the ${cmd:} password that asks provider's CLI for a
// fresh login token at each connect.
func IAMTokenRef(provider string, c Conn) (string, error) {
	switch provider {
	case "aws":
		if !iamWord.MatchString(c.Host) || !iamWord.MatchString(c.User) || c.Port < 1 || c.Port > 65535 {
			return "", errors.New("IAM login needs a plain host name, user and port")
		}
		return "${cmd:aws rds generate-db-auth-token --hostname " + c.Host + " --port " + strconv.Itoa(c.Port) + " --username " + c.User + "}", nil
	case "gcp":
		return "${cmd:gcloud sql generate-login-token}", nil
	case "azure":
		return "${cmd:az account get-access-token --resource-type oss-rdbms --query accessToken --output tsv}", nil
	}
	return "", errors.New("unknown cloud " + sanitizeLine(provider))
}
