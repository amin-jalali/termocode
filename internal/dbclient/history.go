package dbclient

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type HistoryEntry struct {
	ID        string    `json:"id"`
	Time      time.Time `json:"time"`
	Conn      string    `json:"conn"`
	Kind      string    `json:"kind,omitempty"`
	SQL       string    `json:"sql"`
	Rows      int       `json:"rows,omitempty"`
	ElapsedMS int64     `json:"elapsed_ms,omitempty"`
}
type History struct {
	Path string
	Max  int
	mu   sync.Mutex
}

func (h *History) Append(e HistoryEntry) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	e.SQL = Redact(e.SQL)
	if len(e.SQL) > 4096 {
		e.SQL = e.SQL[:4096]
	}
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	a, err := h.read()
	if err != nil {
		return err
	}
	a = append(a, e)
	n := h.Max
	if n <= 0 {
		n = 500
	}
	if len(a) > n {
		a = a[len(a)-n:]
	}
	return h.write(a)
}
func (h *History) List() ([]HistoryEntry, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	a, e := h.read()
	for i, j := 0, len(a)-1; i < j; i, j = i+1, j-1 {
		a[i], a[j] = a[j], a[i]
	}
	return a, e
}
func (h *History) Delete(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	a, e := h.read()
	if e != nil {
		return e
	}
	o := a[:0]
	for _, v := range a {
		if v.ID != id {
			o = append(o, v)
		}
	}
	return h.write(o)
}
func (h *History) Clear() error { h.mu.Lock(); defer h.mu.Unlock(); return h.write(nil) }
func (h *History) read() ([]HistoryEntry, error) {
	b, e := os.ReadFile(h.Path)
	if errors.Is(e, os.ErrNotExist) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var a []HistoryEntry
	s := bufio.NewScanner(bytes.NewReader(b))
	s.Buffer(make([]byte, 1024), 1<<20)
	for s.Scan() {
		var v HistoryEntry
		if json.Unmarshal(s.Bytes(), &v) == nil {
			a = append(a, v)
		}
	}
	return a, s.Err()
}
func (h *History) write(a []HistoryEntry) error {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	for _, v := range a {
		if e := enc.Encode(v); e != nil {
			return e
		}
	}
	if e := os.MkdirAll(filepath.Dir(h.Path), 0700); e != nil {
		return e
	}
	tmp := h.Path + ".tmp"
	if e := os.WriteFile(tmp, b.Bytes(), 0600); e != nil {
		return e
	}
	if e := os.Chmod(tmp, 0600); e != nil {
		return e
	}
	return os.Rename(tmp, h.Path)
}
