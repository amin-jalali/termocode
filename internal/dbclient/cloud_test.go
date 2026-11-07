package dbclient

import (
	"strings"
	"testing"
)

// DBX-49: a new database container: the image, a port on 127.0.0.1 only,
// the password in the env (never argv) and the connection to save.
func TestNewDBContainer(t *testing.T) {
	c, err := NewDBContainer("postgres", 55432, "s3cret", "ab12")
	if err != nil {
		t.Fatal(err)
	}
	if c.Image != "postgres:17" || c.Name != "termocode-postgres-ab12" || strings.Join(c.Ports, ",") != "127.0.0.1:55432:5432" || c.Env["POSTGRES_PASSWORD"] != "s3cret" {
		t.Errorf("container %+v", c)
	}
	if c.Conn.ID != "container-postgres-ab12" || c.Conn.Driver != "postgres" || c.Conn.Host != "127.0.0.1" || c.Conn.Port != 55432 || c.Conn.User != "postgres" || c.Conn.Password != "" {
		t.Errorf("conn %+v", c.Conn)
	}
	for _, e := range DBContainerEngines {
		x, err := NewDBContainer(e.ID, 40000, "pw", "x1")
		if err != nil || x.Image == "" || Validate(x.Conn) != nil {
			t.Errorf("%s: %+v %v %v", e.ID, x, err, Validate(x.Conn))
		}
	}
	if r, _ := NewDBContainer("redis", 40000, "pw", "x1"); r.Env["REDIS_PASSWORD"] != "pw" || strings.Contains(strings.Join(r.Command, " "), "pw ") || !strings.Contains(strings.Join(r.Command, " "), "$REDIS_PASSWORD") {
		t.Errorf("redis password not set: %+v", r)
	}
	if _, err := NewDBContainer("oracle", 1, "pw", "x"); err == nil {
		t.Error("unknown engine accepted")
	}
	if _, err := NewDBContainer("mysql", 1, "pw", "bad name"); err == nil {
		t.Error("bad suffix accepted")
	}
}

// DBX-49: instances listed by the cloud CLIs become connections.
func TestParseCloudInstances(t *testing.T) {
	rds := `{"DBInstances":[
	 {"DBInstanceIdentifier":"shop-db","Engine":"aurora-postgresql","Endpoint":{"Address":"shop.abc.eu-west-1.rds.amazonaws.com","Port":5432},"MasterUsername":"admin","DBName":"shop"},
	 {"DBInstanceIdentifier":"ora","Engine":"oracle-ee","Endpoint":{"Address":"o.rds.amazonaws.com","Port":1521}},
	 {"DBInstanceIdentifier":"my","Engine":"mysql","Endpoint":{"Address":"my.rds.amazonaws.com","Port":3306},"MasterUsername":"root"}]}`
	got, err := ParseCloudInstances("aws", []byte(rds))
	if err != nil || len(got) != 2 {
		t.Fatalf("aws %+v %v", got, err)
	}
	if c := got[0].Conn; c.ID != "aws-shop-db" || c.Driver != "postgres" || c.Host != "shop.abc.eu-west-1.rds.amazonaws.com" || c.Port != 5432 || c.User != "admin" || c.Database != "shop" {
		t.Errorf("aws conn %+v", c)
	}
	gcp := `[{"name":"orders","databaseVersion":"POSTGRES_15","region":"europe-west1","ipAddresses":[{"type":"OUTGOING","ipAddress":"1.1.1.1"},{"type":"PRIMARY","ipAddress":"34.1.2.3"}]},
	 {"name":"legacy","databaseVersion":"SQLSERVER_2019_STANDARD","ipAddresses":[{"type":"PRIMARY","ipAddress":"34.9.9.9"}]}]`
	got, err = ParseCloudInstances("gcp", []byte(gcp))
	if err != nil || len(got) != 1 || got[0].Conn.Host != "34.1.2.3" || got[0].Conn.Port != 5432 || got[0].Conn.ID != "gcp-orders" || !strings.Contains(got[0].Detail, "europe-west1") {
		t.Fatalf("gcp %+v %v", got, err)
	}
	az := `[{"name":"inv","fullyQualifiedDomainName":"inv.postgres.database.azure.com","administratorLogin":"boss","type":"Microsoft.DBforPostgreSQL/flexibleServers"},
	 {"name":"crm","fullyQualifiedDomainName":"crm.mysql.database.azure.com","administratorLogin":"boss","type":"Microsoft.DBforMySQL/flexibleServers"}]`
	got, err = ParseCloudInstances("azure", []byte(az))
	if err != nil || len(got) != 2 || got[0].Conn.Driver != "postgres" || got[1].Conn.Driver != "mysql" || got[1].Conn.Port != 3306 || got[0].Conn.User != "boss" {
		t.Fatalf("azure %+v %v", got, err)
	}
	if _, err := ParseCloudInstances("aws", []byte("not json")); err == nil {
		t.Error("bad json accepted")
	}
	for _, p := range CloudProviders {
		if len(CloudListCommands(p.ID)) == 0 || p.CLI == "" {
			t.Errorf("%s has no list command", p.ID)
		}
	}
}

// DBX-49: IAM / token auth is a ${cmd:} password that asks the cloud CLI
// for a fresh token at each connect; names that would need shell quoting
// are refused.
func TestIAMTokenRef(t *testing.T) {
	c := Conn{Driver: "postgres", Host: "shop.abc.rds.amazonaws.com", Port: 5432, User: "app_user"}
	cases := map[string]string{
		"aws":   "${cmd:aws rds generate-db-auth-token --hostname shop.abc.rds.amazonaws.com --port 5432 --username app_user}",
		"gcp":   "${cmd:gcloud sql generate-login-token}",
		"azure": "${cmd:az account get-access-token --resource-type oss-rdbms --query accessToken --output tsv}",
	}
	for p, want := range cases {
		got, err := IAMTokenRef(p, c)
		if err != nil || got != want {
			t.Errorf("%s = %q %v", p, got, err)
		}
	}
	bad := c
	bad.User = "x; rm -rf /"
	if _, err := IAMTokenRef("aws", bad); err == nil {
		t.Error("shell text in the user accepted")
	}
	if _, err := IAMTokenRef("oracle", c); err == nil {
		t.Error("unknown provider accepted")
	}
}
