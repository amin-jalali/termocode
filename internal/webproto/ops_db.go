package webproto

import "strings"

// The rest of the Databases view from the page (web parity, Databases
// area). Reads run in a read-only session of their own, as db.query
// does: a table's DDL, a statement's plan (analyze for a read only), a
// connection test, a schema compare (the script is text, never run), the
// SQL history (redacted), the workspace's CSV files and a Redis
// database's keys (names, types and TTLs, never values). db.cancel stops
// the page's own reads on a connection; db.connect starts the laptop's
// own connect (a password is typed on the laptop, never here); db.config
// names the connections file (made from the template when missing).
//
// Every change to a database or a saved connection waits for a laptop
// card (Cancel first) that shows exactly what happens: row edits (the
// statements with their values), a CSV import (the file, its rows and
// the INSERT), a Redis key's delete or TTL, a new connection, an SSH
// tunnel, SSL settings, a disconnect, commit, roll back and the
// transaction mode of the laptop's console. No arg carries a password,
// a passphrase or a URL: decoding refuses unknown fields.
//
// A read that needs the user first fails as a conflict whose data is a
// DBNeed: an unknown SSH host key (db.trust_host saves the one the page
// showed, named by its fingerprint, to known_hosts).

// Database op names (web parity).
const (
	OpDBDDL        = "db.ddl"
	OpDBExplain    = "db.explain"
	OpDBTest       = "db.test"
	OpDBCompare    = "db.compare"
	OpDBHistory    = "db.history"
	OpDBCSVFiles   = "db.csv_files"
	OpDBCancel     = "db.cancel"
	OpDBConnect    = "db.connect"
	OpDBConfig     = "db.config"
	OpRedisKeys    = "redis.keys"
	OpDBEdit       = "db.edit"
	OpDBImport     = "db.import"
	OpRedisKey     = "redis.key"
	OpDBAdd        = "db.add"
	OpDBSSH        = "db.ssh"
	OpDBSSL        = "db.ssl"
	OpDBDisconnect = "db.disconnect"
	OpDBTx         = "db.tx"
	OpDBTrustHost  = "db.trust_host"
)

var opDBRows = []Policy{
	{Op: OpDBDDL, New: newOf[DBTableArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpDBExplain, New: newOf[DBExplainArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 72 * kib, Live: 1, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpDBTest, New: newOf[DBConnArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Live: 1, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpDBCompare, New: newOf[DBCompareArgs](), Role: RoleViewer, Rate: perSec(1), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpDBHistory, New: newOf[DBHistoryArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpDBCSVFiles, New: newOf[EmptyArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpDBCancel, New: newOf[DBConnArgs](), Role: RoleController, Rate: perSec(5), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpDBConnect, New: newOf[DBConnArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpDBConfig, New: newOf[EmptyArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpRedisKeys, New: newOf[RedisKeysArgs](), Role: RoleViewer, Rate: perSec(2), MaxBytes: 1 * kib, Live: 1, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpDBEdit, New: newOf[DBEditArgs](), Role: RoleController, Rate: slow, MaxBytes: 256 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpDBImport, New: newOf[DBImportArgs](), Role: RoleController, Rate: slow, MaxBytes: 2 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpRedisKey, New: newOf[RedisKeyArgs](), Role: RoleController, Rate: slow, MaxBytes: 2 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpDBAdd, New: newOf[DBAddArgs](), Role: RoleController, Rate: slow, MaxBytes: 4 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpDBSSH, New: newOf[DBSSHArgs](), Role: RoleController, Rate: slow, MaxBytes: 4 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpDBSSL, New: newOf[DBSSLArgs](), Role: RoleController, Rate: slow, MaxBytes: 4 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpDBDisconnect, New: newOf[DBConnArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpDBTx, New: newOf[DBTxArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpDBTrustHost, New: newOf[DBTrustArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opDBRows...); return true }()

func init() {
	registerResult(OpDBDDL, DBText{})
	registerResult(OpDBExplain, DBPlan{})
	registerResult(OpDBTest, DBDone{})
	registerResult(OpDBCompare, DBCompare{})
	registerResult(OpDBHistory, DBHistory{})
	registerResult(OpDBCSVFiles, DBFiles{})
	registerResult(OpDBCancel, DBDone{})
	registerResult(OpDBConnect, DBDone{})
	registerResult(OpDBConfig, DBConfigFile{})
	registerResult(OpRedisKeys, RedisKeys{})
	registerResult(OpDBTrustHost, DBDone{})
	for _, op := range []string{OpDBEdit, OpDBImport, OpRedisKey, OpDBAdd, OpDBSSH, OpDBSSL, OpDBDisconnect, OpDBTx} {
		registerResult(op, AskRef{})
	}
}

// Database limits (web parity).
const (
	MaxDBEdits       = 500       // row changes in one db.edit
	MaxDBEditCols    = 200       // columns of one row change
	MaxDBValue       = 64 << 10  // bytes of one edited value
	MaxDBPlanLines   = 2000      // lines of one plan
	MaxDBScript      = 256 << 10 // bytes of a compare script
	MaxDBHistory     = 200       // history entries
	MaxDBCSVFiles    = 500       // CSV files listed
	MaxRedisPattern  = 256
	MaxRedisKey      = 1024
	MaxRedisKeys     = 1000 // keys of one redis.keys
	MaxRedisDB       = 15
	MaxRedisTTL      = 10 * 365 * 24 * 3600 // seconds
	MaxDBHost        = 255
	MaxDBJump        = 8
	MaxDBConnName    = 128
	MaxDBLaptopPath  = 1024
	MaxDBDatabase    = 128
	MaxDBEnvironment = 32
	MaxDBFingerprint = 100 // "SHA256:" and the key's hash
)

// ── Args ────────────────────────────────────────────────────────────────

// DBTableArgs names a listed table of a connection.
type DBTableArgs struct {
	Conn  string `json:"conn"`
	Table string `json:"table"`
}

// DBExplainArgs asks for the plan of one statement; Analyze runs it (a
// read only).
type DBExplainArgs struct {
	Conn    string `json:"conn"`
	SQL     string `json:"sql"`
	Analyze bool   `json:"analyze,omitempty"`
}

// DBCompareArgs compares the default schema of Conn (the source) with
// the one of Target.
type DBCompareArgs struct {
	Conn   string `json:"conn"`
	Target string `json:"target"`
}

// DBHistoryArgs lists the SQL history, of one connection when Conn is set.
type DBHistoryArgs struct {
	Conn string `json:"conn,omitempty"`
}

// RedisKeysArgs lists the keys of one Redis database matching Pattern
// (a glob; "" is every key).
type RedisKeysArgs struct {
	Conn    string `json:"conn"`
	DB      int    `json:"db,omitempty"`
	Pattern string `json:"pattern,omitempty"`
}

// DBColValue is one column's value of a row change: its text or NULL.
type DBColValue struct {
	Col string `json:"col"`
	V   string `json:"v,omitempty"`
	N   bool   `json:"n,omitempty"`
}

// DBEdit is one pending row change: an update finds its row by Key and
// sets Set (Old: the values the page showed, checked so a row changed
// meanwhile is not overwritten); a delete finds its row by Key; an
// insert sets Set.
type DBEdit struct {
	Kind string       `json:"kind" enum:"update|insert|delete"`
	Key  []DBColValue `json:"key,omitempty"`
	Set  []DBColValue `json:"set,omitempty"`
	Old  []DBColValue `json:"old,omitempty"`
}

// DBEditArgs are the pending row changes of one listed table.
type DBEditArgs struct {
	Conn  string   `json:"conn"`
	Table string   `json:"table"`
	Edits []DBEdit `json:"edits"`
}

// DBImportArgs imports a workspace CSV file into a listed table.
type DBImportArgs struct {
	Conn     string `json:"conn"`
	Table    string `json:"table"`
	Path     string `json:"path"`
	NoHeader bool   `json:"noheader,omitempty"` // the first row is data
}

// RedisKeyArgs deletes a listed key or sets its TTL (seconds; 0 removes it).
type RedisKeyArgs struct {
	Conn string `json:"conn"`
	DB   int    `json:"db,omitempty"`
	Key  string `json:"key"`
	Verb string `json:"verb" enum:"delete|ttl"`
	TTL  int64  `json:"ttl,omitempty"`
}

// DBAddArgs is a new connection for the project's databases.json: never
// a password (the laptop asks for it on connect) nor a URL.
type DBAddArgs struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Driver   string `json:"driver" enum:"postgres|mysql|sqlite|redis"`
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	User     string `json:"user,omitempty"`
	Database string `json:"database,omitempty"`
	Path     string `json:"path,omitempty"` // sqlite: a workspace file
	DB       int    `json:"db,omitempty"`   // redis
	Env      string `json:"env,omitempty"`
	ReadOnly bool   `json:"readonly,omitempty"`
}

// DBSSHArgs sets (or, Remove, drops) a connection's SSH tunnel; its
// password or key passphrase is typed on the laptop on connect.
type DBSSHArgs struct {
	Conn    string   `json:"conn"`
	Host    string   `json:"host,omitempty"`
	Port    int      `json:"port,omitempty"`
	User    string   `json:"user,omitempty"`
	Auth    string   `json:"auth,omitempty" enum:"agent|key|password"`
	KeyFile string   `json:"keyfile,omitempty"`
	Jump    []string `json:"jump,omitempty"`
	Remove  bool     `json:"remove,omitempty"`
}

// DBSSLArgs sets a connection's SSL mode and certificate files.
type DBSSLArgs struct {
	Conn string `json:"conn"`
	Mode string `json:"mode" enum:"disable|require|verify-ca|verify-full"`
	CA   string `json:"ca,omitempty"`
	Cert string `json:"cert,omitempty"`
	Key  string `json:"key,omitempty"` // the client key's file
}

// DBTrustArgs saves the SSH host key the page was asked about (a
// DBNeed "host_key") to known_hosts; Fingerprint names it, so only the
// key the page showed is saved.
type DBTrustArgs struct {
	Conn        string `json:"conn"`
	Fingerprint string `json:"fingerprint"`
}

// DBTxArgs ends the laptop console's transaction or sets its mode.
type DBTxArgs struct {
	Conn string `json:"conn"`
	Verb string `json:"verb" enum:"commit|rollback|manual|auto"`
}

func (a *DBTableArgs) validate(int) error {
	if !validName(a.Conn, MaxID) || !validName(a.Table, MaxDBTable) {
		return invalid("bad connection or table")
	}
	return nil
}

func (a *DBExplainArgs) validate(n int) error {
	return (&DBSQLArgs{Conn: a.Conn, SQL: a.SQL}).validate(n)
}

func (a *DBCompareArgs) validate(int) error {
	if !validName(a.Conn, MaxID) || !validName(a.Target, MaxID) || a.Conn == a.Target {
		return invalid("bad connections")
	}
	return nil
}

func (a *DBHistoryArgs) validate(int) error {
	if a.Conn != "" && !validName(a.Conn, MaxID) {
		return invalid("bad connection")
	}
	return nil
}

func (a *RedisKeysArgs) validate(int) error {
	if !validName(a.Conn, MaxID) || a.DB < 0 || a.DB > MaxRedisDB || len(a.Pattern) > MaxRedisPattern || hasControl(a.Pattern) {
		return invalid("bad connection, database or pattern")
	}
	return nil
}

func validColValues(vs []DBColValue) bool {
	if len(vs) > MaxDBEditCols {
		return false
	}
	seen := map[string]bool{}
	for _, v := range vs {
		if !validName(v.Col, MaxDBTable) || seen[v.Col] || len(v.V) > MaxDBValue || strings.ContainsRune(v.V, 0) || v.N && v.V != "" {
			return false
		}
		seen[v.Col] = true
	}
	return true
}

func (a *DBEditArgs) validate(int) error {
	if !validName(a.Conn, MaxID) || !validName(a.Table, MaxDBTable) || len(a.Edits) == 0 || len(a.Edits) > MaxDBEdits {
		return invalid("bad connection, table or changes")
	}
	for _, e := range a.Edits {
		if !validColValues(e.Key) || !validColValues(e.Set) || !validColValues(e.Old) {
			return invalid("bad change")
		}
		ok := false
		switch e.Kind {
		case "update":
			ok = len(e.Key) > 0 && len(e.Set) > 0
		case "delete":
			ok = len(e.Key) > 0 && len(e.Set) == 0
		case "insert":
			ok = len(e.Key) == 0 && len(e.Set) > 0
		}
		if !ok {
			return invalid("bad change")
		}
	}
	return nil
}

func (a *DBImportArgs) validate(int) error {
	if !validName(a.Conn, MaxID) || !validName(a.Table, MaxDBTable) || !validPath(a.Path, false) {
		return invalid("bad connection, table or file")
	}
	return nil
}

func (a *RedisKeyArgs) validate(int) error {
	if !validName(a.Conn, MaxID) || a.DB < 0 || a.DB > MaxRedisDB || a.Key == "" || len(a.Key) > MaxRedisKey || strings.ContainsRune(a.Key, 0) {
		return invalid("bad connection, database or key")
	}
	switch {
	case a.Verb == "delete" && a.TTL == 0:
	case a.Verb == "ttl" && a.TTL >= 0 && a.TTL <= MaxRedisTTL:
	default:
		return invalid("bad action or TTL")
	}
	return nil
}

// validDBHost is a host or ~/.ssh/config alias: no space, no leading "-".
func validDBHost(h string) bool {
	return validName(h, MaxDBHost) && !strings.HasPrefix(h, "-") && !strings.ContainsAny(h, " /\\@")
}

// validDBID is a connection id databases.json takes.
func validDBID(id string) bool {
	if id == "" || len(id) > MaxID {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// validLaptopPath is a path on the laptop (absolute, ~ or relative), not
// an option.
func validLaptopPath(p string) bool {
	return p == "" || validName(p, MaxDBLaptopPath) && !strings.HasPrefix(p, "-")
}

func (a *DBAddArgs) validate(int) error {
	if !validDBID(a.ID) || len(a.Name) > MaxDBConnName || hasControl(a.Name) || len(a.Env) > MaxDBEnvironment || hasControl(a.Env) ||
		len(a.User) > MaxDBHost || hasControl(a.User) || len(a.Database) > MaxDBDatabase || hasControl(a.Database) {
		return invalid("bad id, name, user, database or environment")
	}
	switch a.Driver {
	case "sqlite":
		if !validPath(a.Path, false) || a.Host != "" || a.Port != 0 {
			return invalid("a sqlite connection needs a workspace file")
		}
	case "postgres", "mysql", "redis", "cassandra":
		if !validDBHost(a.Host) || a.Port < 0 || a.Port > 65535 || a.Path != "" {
			return invalid("bad host or port")
		}
	default:
		return invalid("bad driver")
	}
	if a.DB < 0 || a.DB > MaxRedisDB || a.DB != 0 && a.Driver != "redis" {
		return invalid("bad redis database")
	}
	return nil
}

func (a *DBSSHArgs) validate(int) error {
	if !validName(a.Conn, MaxID) {
		return invalid("bad connection")
	}
	if a.Remove {
		if a.Host != "" || a.User != "" || a.Port != 0 || a.Auth != "" || a.KeyFile != "" || len(a.Jump) > 0 {
			return invalid("remove takes nothing else")
		}
		return nil
	}
	if !validDBHost(a.Host) || a.Port < 0 || a.Port > 65535 || len(a.User) > MaxDBHost || hasControl(a.User) || strings.HasPrefix(a.User, "-") ||
		!validLaptopPath(a.KeyFile) || len(a.Jump) > MaxDBJump {
		return invalid("bad host, port, user or key file")
	}
	switch a.Auth {
	case "", "agent", "key", "password":
	default:
		return invalid("bad auth")
	}
	for _, j := range a.Jump {
		if !validName(j, MaxDBHost) || strings.HasPrefix(j, "-") || strings.ContainsAny(j, " /\\") {
			return invalid("bad jump host")
		}
	}
	return nil
}

func (a *DBSSLArgs) validate(int) error {
	if !validName(a.Conn, MaxID) || !validLaptopPath(a.CA) || !validLaptopPath(a.Cert) || !validLaptopPath(a.Key) {
		return invalid("bad connection or file")
	}
	switch a.Mode {
	case "disable", "require", "verify-ca", "verify-full":
	default:
		return invalid("bad mode")
	}
	return nil
}

func (a *DBTrustArgs) validate(int) error {
	hash, ok := strings.CutPrefix(a.Fingerprint, "SHA256:")
	if !validName(a.Conn, MaxID) || !ok || hash == "" || len(a.Fingerprint) > MaxDBFingerprint || strings.ContainsAny(hash, " \t") || hasControl(hash) {
		return invalid("bad connection or fingerprint")
	}
	return nil
}

func (a *DBTxArgs) validate(int) error {
	if !validName(a.Conn, MaxID) {
		return invalid("bad connection")
	}
	switch a.Verb {
	case "commit", "rollback", "manual", "auto":
	default:
		return invalid("bad action")
	}
	return nil
}

// ── Results ─────────────────────────────────────────────────────────────

// DBNeed is the data of a database op's conflict that the user can
// clear from the page. Need "host_key": the SSH server (Jump: a jump
// host on the way) is not in known_hosts yet; Host, KeyType and
// Fingerprint are what to check before db.trust_host.
type DBNeed struct {
	Need        string `json:"need"`
	Host        string `json:"host,omitempty"`
	KeyType     string `json:"key_type,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Jump        bool   `json:"jump,omitempty"`
}

// DBText answers db.ddl.
type DBText struct {
	Text string `json:"text"`
}

// DBPlan answers db.explain: the plan as indented lines.
type DBPlan struct {
	Lines     []DBPlanLine `json:"lines"`
	Summary   []string     `json:"summary,omitempty"`
	Analyze   bool         `json:"analyze,omitempty"`
	Truncated bool         `json:"truncated,omitempty"`
	Ms        int64        `json:"ms"`
}

// DBPlanLine is one node, or one detail of the node above.
type DBPlanLine struct {
	Depth  int    `json:"depth"`
	Text   string `json:"text"`
	Detail bool   `json:"detail,omitempty"`
	Costly bool   `json:"costly,omitempty"` // the node taking the biggest share
}

// DBDone answers db.test, db.cancel and db.connect: one line.
type DBDone struct {
	Text string `json:"text"`
	Ms   int64  `json:"ms,omitempty"`
}

// DBCompare answers db.compare: the migration script that would make
// Target like Conn (text only, never run).
type DBCompare struct {
	Script      string `json:"script"`
	Changes     int    `json:"changes"`
	Destructive int    `json:"destructive,omitempty"`
	Truncated   bool   `json:"truncated,omitempty"`
}

// DBHistory answers db.history, newest first.
type DBHistory struct {
	Entries []DBHistoryEntry `json:"entries"`
}

// DBHistoryEntry is one SQL run (redacted).
type DBHistoryEntry struct {
	Conn string `json:"conn"`
	SQL  string `json:"sql"`
	At   int64  `json:"at"` // unix ms
	Ms   int64  `json:"ms,omitempty"`
	Rows int    `json:"rows,omitempty"`
}

// DBFiles answers db.csv_files: workspace paths.
type DBFiles struct {
	Files []string `json:"files"`
	More  int      `json:"more,omitempty"`
}

// DBConfigFile answers db.config: the connections file to open.
type DBConfigFile struct {
	Path    string `json:"path"`
	Created bool   `json:"created,omitempty"`
}

// RedisKeys answers redis.keys.
type RedisKeys struct {
	DB   int        `json:"db"`
	Keys []RedisKey `json:"keys"`
	More bool       `json:"more,omitempty"`
}

// RedisKey is one key: its type and TTL (ms; -1 none), never its value.
type RedisKey struct {
	Key  string `json:"key"`
	Type string `json:"type"`
	TTL  int64  `json:"ttl"`
}
