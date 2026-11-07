package dbclient

// MySQLPreset is a named server of the MySQL family that runs on the
// MySQL driver with its own defaults (DBX-61).
type MySQLPreset struct {
	ID      string
	Title   string
	Driver  string
	Port    int
	User    string
	Host    string // the host the add prompt offers
	SSLMode string // "" = the driver default
	Hint    string // the picker hint
	Note    string // what differs from MySQL
}

var mysqlPresets = []MySQLPreset{
	{ID: "mariadb", Title: "MariaDB", Driver: "mysql", Port: 3306, User: "root", Host: "localhost",
		Hint: "MySQL driver, port 3306", Note: "MariaDB: its own EXPLAIN format, sequences and system-versioned tables."},
	{ID: "tidb", Title: "TiDB", Driver: "mysql", Port: 4000, User: "root", Host: "localhost",
		Hint: "MySQL driver, port 4000", Note: "TiDB: EXPLAIN ANALYZE shows the distributed plan; some MySQL features (triggers, events) are not supported."},
	{ID: "planetscale", Title: "PlanetScale / Vitess", Driver: "mysql", Port: 3306, Host: "aws.connect.psdb.cloud", SSLMode: SSLVerifyFull,
		Hint: "MySQL driver, TLS required", Note: "Vitess: no foreign key constraints by default; one keyspace per database."},
	{ID: "aurora-mysql", Title: "Amazon Aurora (MySQL)", Driver: "mysql", Port: 3306, User: "admin", Host: "localhost", SSLMode: SSLRequire,
		Hint: "MySQL driver, TLS", Note: "Aurora MySQL: use the cluster endpoint for writes and the reader endpoint for reads."},
	{ID: "singlestore", Title: "SingleStore", Driver: "mysql", Port: 3306, User: "root", Host: "localhost",
		Hint: "MySQL driver, port 3306", Note: "SingleStore: rowstore and columnstore tables; EXPLAIN shows its own plan."},
}

// MySQLPresets lists the MySQL family presets.
func MySQLPresets() []MySQLPreset { return append([]MySQLPreset(nil), mysqlPresets...) }

// PresetByID finds a preset by its ID.
func PresetByID(id string) (MySQLPreset, bool) {
	for _, p := range mysqlPresets {
		if p.ID == id {
			return p, true
		}
	}
	return MySQLPreset{}, false
}

// PresetTitle is the preset's name, or "" when the connection has none
// (or an unknown one).
func (c Conn) PresetTitle() string {
	if p, ok := PresetByID(c.Preset); ok {
		return p.Title
	}
	return ""
}
