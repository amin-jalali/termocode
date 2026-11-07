package dbclient

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// SavedQuery is one bookmarked statement (DBX-6).
type SavedQuery struct {
	ID   string    `json:"id"`
	Name string    `json:"name"`
	Conn string    `json:"conn"`
	SQL  string    `json:"sql"`
	Time time.Time `json:"time"`
}

// SavedQueries keeps the saved queries in one JSON file (0600). Secrets
// in the SQL are redacted like the history.
type SavedQueries struct {
	Path string
	mu   sync.Mutex
}

// SavedQueryMax caps the SQL of one saved query.
const SavedQueryMax = 64 << 10

// Save stores q; a query with the same name (any case) on the same
// connection is replaced and keeps its ID.
func (s *SavedQueries) Save(q SavedQuery) (SavedQuery, error) {
	q.Name = strings.TrimSpace(q.Name)
	q.SQL = strings.TrimSpace(Redact(q.SQL))
	if q.Name == "" {
		return q, errors.New("the name is empty")
	}
	if q.SQL == "" {
		return q, errors.New("the query is empty")
	}
	if len(q.SQL) > SavedQueryMax {
		return q, errors.New("the query is too long to save (64 KB at most)")
	}
	if q.Time.IsZero() {
		q.Time = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.read()
	if err != nil {
		return q, err
	}
	for i := range a {
		if a[i].Conn == q.Conn && strings.EqualFold(a[i].Name, q.Name) {
			q.ID = a[i].ID
			a[i] = q
			return q, s.write(a)
		}
	}
	if q.ID == "" {
		var b [8]byte
		_, _ = rand.Read(b[:])
		q.ID = hex.EncodeToString(b[:])
	}
	return q, s.write(append(a, q))
}

// List returns the saved queries of conn ("" = all), by name.
func (s *SavedQueries) List(conn string) ([]SavedQuery, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.read()
	out := a[:0]
	for _, q := range a {
		if conn == "" || q.Conn == conn {
			out = append(out, q)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if li, lj := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name); li != lj {
			return li < lj
		}
		return out[i].Conn < out[j].Conn
	})
	return out, err
}

// Delete removes one saved query.
func (s *SavedQueries) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, err := s.read()
	if err != nil {
		return err
	}
	out := a[:0]
	for _, q := range a {
		if q.ID != id {
			out = append(out, q)
		}
	}
	return s.write(out)
}

func (s *SavedQueries) read() ([]SavedQuery, error) {
	b, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var a []SavedQuery
	if err := json.Unmarshal(b, &a); err != nil {
		return nil, errors.New("the saved queries file is broken: " + err.Error())
	}
	return a, nil
}

func (s *SavedQueries) write(a []SavedQuery) error {
	if a == nil {
		a = []SavedQuery{}
	}
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	tmp := s.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.Path)
}
