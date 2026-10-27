package session

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/amin-jalali/termocode/internal/workspace"
)

// Sessions and hot-exit backups written before workspace IDs were keyed
// by this hash of the path; a local workspace ID must find them again.
func TestKeyForLocalIDIsTheOldPathKey(t *testing.T) {
	sum := sha256.Sum256([]byte("/home/u/proj"))
	old := hex.EncodeToString(sum[:8])
	if got := KeyForID(workspace.LocalID("/home/u/proj")); got != old {
		t.Fatalf("KeyForID(local) = %s, want %s", got, old)
	}
	if got := Key("/home/u/proj/"); got != old {
		t.Fatalf("Key = %s, want %s", got, old)
	}
	if KeyForID("container:prod/api-1:/app") == Key("/app") {
		t.Fatal("a container workspace shares the key of a local /app")
	}
}
