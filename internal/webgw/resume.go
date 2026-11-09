//go:build unix

package webgw

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// The hand-over across a reload into a new build. termocode re-execs
// itself (same pid, same terminal); its gateway, a child of the old
// image, would only see the UI go (4012) and end every browser session.
// Instead the UI says bye 1012 first: the gateway writes this file and
// closes the browsers with 1012, which the page retries. The new image
// starts its gateway with --resume on the same port; it takes the file's
// sessions, so each page gets back in with the token it holds. The file
// holds token digests only (as the gateway's memory does), never a
// token or the launch secret; it is 0600, read once and good for
// ResumeTTL.

// ResumeTTL is how long a resume file is good: a reload takes seconds.
const ResumeTTL = 2 * time.Minute

// Resume is what one gateway hands the next.
type Resume struct {
	V       int       `json:"v"`
	At      time.Time `json:"at"`
	Port    int       `json:"port"`
	SSHPort int       `json:"ssh_port,omitempty"` // SSH mode: the user's forwarded port
	// Started is when the first gateway of the chain began: the panel's
	// Max counts from there, a reload does not make it longer.
	Started time.Time     `json:"started"`
	Tokens  []TokenRecord `json:"tokens"`
}

// resumeV is the file's version.
const resumeV = 1

// TokenRecord is one browser session by the digest of its token.
type TokenRecord struct {
	Hash     string    `json:"h"`
	ID       string    `json:"id"`
	Device   string    `json:"device"`
	Role     Role      `json:"role"`
	Created  time.Time `json:"created"`
	LastSeen time.Time `json:"last_seen"`
}

// Export lists the live sessions (expired ones are left out).
func (t *Tokens) Export(now time.Time) []TokenRecord {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]TokenRecord, 0, len(t.byHash))
	for h, s := range t.byHash {
		if t.expired(s, now) {
			continue
		}
		out = append(out, TokenRecord{Hash: hex.EncodeToString(h[:]), ID: s.ID, Device: s.Device, Role: s.Role,
			Created: s.Created, LastSeen: s.LastSeen})
	}
	return out
}

// Import takes over sessions another gateway exported and says how many.
// A bad digest, an unknown role or an expired session is skipped, and
// MaxSessions still caps them.
func (t *Tokens) Import(recs []TokenRecord, now time.Time) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for _, r := range recs {
		b, err := hex.DecodeString(r.Hash)
		if err != nil || len(b) != sha256.Size || (r.Role != RoleControl && r.Role != RoleViewer) || len(t.byHash) >= MaxSessions {
			continue
		}
		s := &Sess{ID: r.ID, Device: r.Device, Role: r.Role, Created: r.Created, LastSeen: r.LastSeen}
		if t.expired(s, now) {
			continue
		}
		t.byHash[[sha256.Size]byte(b)] = s
		n++
	}
	return n
}

// WriteResume writes r to path atomically with mode 0600.
func WriteResume(path string, r Resume) error {
	r.V = resumeV
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".web-resume-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// errResumeStale: the file is older than ResumeTTL (or from the future).
var errResumeStale = errors.New("the resume file is stale")

// ReadResume reads and removes the resume file. A stale or damaged one
// is removed too and refused.
func ReadResume(path string, now time.Time) (Resume, error) {
	return readResume(path, now, true)
}

// PeekResume reads the resume file and leaves it for the gateway (the
// UI needs the port to start it).
func PeekResume(path string, now time.Time) (Resume, error) {
	return readResume(path, now, false)
}

func readResume(path string, now time.Time, take bool) (Resume, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Resume{}, err
	}
	if take {
		_ = os.Remove(path)
	}
	var r Resume
	if err := json.Unmarshal(b, &r); err != nil {
		return Resume{}, fmt.Errorf("the resume file is damaged: %w", err)
	}
	if r.V != resumeV || r.Port < 0 || r.Port > 65535 {
		return Resume{}, errors.New("the resume file is of another version")
	}
	if age := now.Sub(r.At); age > ResumeTTL || age < -time.Minute {
		return Resume{}, errResumeStale
	}
	return r, nil
}
