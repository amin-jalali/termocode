package dbclient

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// SQLCipherPreset marks an encrypted SQLite file (DBX-64). It runs on the
// sqlcipher CLI, which speaks the sqlite3 shell protocol; the key comes
// from the connection's password reference and goes to stdin only.
const SQLCipherPreset = "sqlcipher"

var lookupSQLCipher = func() (string, error) { return exec.LookPath("sqlcipher") }

// IsSQLCipher reports whether c is an encrypted SQLite connection.
func IsSQLCipher(c Conn) bool {
	return normDriver(c.Driver) == "sqlite" && strings.EqualFold(c.Preset, SQLCipherPreset)
}

var sqlcipherRawKey = lazyre.New(`^x'[0-9A-Fa-f]+'$`)

// SQLCipherKeyPragma is the statement that unlocks the file. A raw hex key
// (x'…') goes in double quotes as SQLCipher expects; any other key is one
// SQL string with its quotes doubled.
func SQLCipherKeyPragma(key string) string {
	if sqlcipherRawKey.MatchString(key) {
		return `PRAGMA key = "` + key + `";`
	}
	return "PRAGMA key = '" + strings.ReplaceAll(key, "'", "''") + "';"
}

// NewSQLCipherSession opens an encrypted SQLite file with key.
func NewSQLCipherSession(ctx context.Context, conn Conn, key string) (*SQLiteSession, error) {
	path, err := lookupSQLCipher()
	if err != nil {
		return nil, fmt.Errorf("sqlcipher is not installed. %s", sqlcipherHostInstallHint())
	}
	if key == "" {
		return nil, errors.New("this encrypted database needs its key; set the connection password to a ${secret:…} reference")
	}
	if _, err := sqliteVersionOf(ctx, path); err != nil {
		return nil, err
	}
	conn.Preset = SQLCipherPreset
	s := &SQLiteSession{conn: conn, readonly: conn.ReadOnly, cipherKey: key}
	if err := s.start(path, conn.ReadOnly); err != nil {
		return nil, err
	}
	return s, nil
}

// sqlcipherKeyError turns sqlite's answer to a wrong key into plain words.
func sqlcipherKeyError(err error) error {
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "file is not a database") {
		return errors.New("could not open the encrypted database: wrong key, or not a SQLCipher file")
	}
	return err
}

func sqlcipherHostInstallHint() string {
	goos, _ := hostOS()
	release := ""
	if goos == "linux" {
		if b, err := readOSRelease(); err == nil {
			release = b
		}
	}
	return SQLCipherInstallHintWithRelease(goos, release)
}

// SQLCipherInstallHintWithRelease is the package guidance for the sqlcipher CLI.
func SQLCipherInstallHintWithRelease(goos, release string) string {
	switch goos {
	case "android":
		return "Install sqlcipher with: pkg install sqlcipher"
	case "darwin":
		return "Install sqlcipher with: brew install sqlcipher"
	case "linux":
		id := strings.ToLower(release)
		if strings.Contains(id, "fedora") {
			return "Install sqlcipher with: sudo dnf install sqlcipher"
		}
		if strings.Contains(id, "debian") || strings.Contains(id, "ubuntu") {
			return "Install sqlcipher with: sudo apt install sqlcipher"
		}
		return "Install sqlcipher with your package manager"
	default:
		return "Install sqlcipher with your package manager"
	}
}
