package dbclient

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Query log (DBX-48): every statement a SQL session sends, the internal
// ones too (read-only sessions for catalogs, compare and diagrams, and
// catalog reads through Introspect), kept in memory for this run only.
// It is never written to disk: statements can hold literal values.

// QueryLogEntry is one statement sent.
type QueryLogEntry struct {
	At       time.Time
	Conn     string
	SQL      string
	Dur      time.Duration
	Rows     int
	Affected int64
	Err      string
	Internal bool // sent by termocode, not typed by the user
}

// QueryLog keeps the newest Max entries.
type QueryLog struct {
	mu      sync.Mutex
	max     int
	entries []QueryLogEntry
}

// NewQueryLog is a log of the newest max entries.
func NewQueryLog(max int) *QueryLog { return &QueryLog{max: max} }

// Add appends e and drops the oldest entries over the bound.
func (l *QueryLog) Add(e QueryLogEntry) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, e)
	if over := len(l.entries) - l.max; l.max > 0 && over > 0 {
		l.entries = append([]QueryLogEntry(nil), l.entries[over:]...)
	}
}

// Entries is a copy of the log, oldest first.
func (l *QueryLog) Entries() []QueryLogEntry {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]QueryLogEntry(nil), l.entries...)
}

// Clear empties the log.
func (l *QueryLog) Clear() {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.entries = nil
	l.mu.Unlock()
}

// loggedSession sends every call on to Session and logs it.
type loggedSession struct {
	Session
	log      *QueryLog
	conn     string
	internal bool
}

// LogSession wraps s so every statement it sends goes into l under conn.
func LogSession(s Session, l *QueryLog, conn string) Session {
	if s == nil || l == nil {
		return s
	}
	if _, ok := s.(*loggedSession); ok {
		return s
	}
	return &loggedSession{Session: s, log: l, conn: conn}
}

// UnwrapSession is the driver session under a logged one (s itself when
// it is not logged), for type checks on the driver.
func UnwrapSession(s Session) Session {
	if w, ok := s.(*loggedSession); ok {
		return w.Session
	}
	return s
}

func (w *loggedSession) add(sql string, start time.Time, err error, internal bool) {
	e := QueryLogEntry{At: start, Conn: w.conn, SQL: sql, Dur: time.Since(start), Internal: w.internal || internal}
	if err != nil {
		e.Err = err.Error()
	}
	w.log.Add(e)
}

func (w *loggedSession) Run(ctx context.Context, sql string, o RunOptions) Result {
	start := time.Now()
	r := w.Session.Run(ctx, sql, o)
	e := QueryLogEntry{At: start, Conn: w.conn, SQL: sql, Dur: time.Since(start), Rows: len(r.Rows), Affected: r.Affected, Internal: w.internal}
	if r.Err != nil {
		e.Err = r.Err.Error()
	}
	w.log.Add(e)
	return r
}

func (w *loggedSession) Introspect(ctx context.Context, q IntrospectionRequest) (IntrospectionResult, error) {
	start := time.Now()
	r, err := w.Session.Introspect(ctx, q)
	what := strings.TrimSpace(strings.Join([]string{q.Kind, strings.Trim(q.Database+"."+q.Schema+"."+q.Object, ".")}, " "))
	w.add("-- catalog read: "+what, start, err, true)
	return r, err
}

func (w *loggedSession) Begin(ctx context.Context) error {
	start := time.Now()
	err := w.Session.Begin(ctx)
	w.add("BEGIN", start, err, false)
	return err
}

func (w *loggedSession) Commit(ctx context.Context) error {
	start := time.Now()
	err := w.Session.Commit(ctx)
	w.add("COMMIT", start, err, false)
	return err
}

func (w *loggedSession) Rollback(ctx context.Context) error {
	start := time.Now()
	err := w.Session.Rollback(ctx)
	w.add("ROLLBACK", start, err, false)
	return err
}

func (w *loggedSession) ReadOnlySession() (Session, error) {
	ro, err := w.Session.ReadOnlySession()
	if err != nil || ro == nil {
		return ro, err
	}
	return &loggedSession{Session: ro, log: w.log, conn: w.conn, internal: true}, nil
}

// FormatQueryLog is the log as SQL text: a "--" header line per
// statement, then the statement.
func FormatQueryLog(entries []QueryLogEntry) string {
	var b strings.Builder
	for _, e := range entries {
		outcome := fmt.Sprintf("%d rows", e.Rows)
		switch {
		case e.Err != "":
			outcome = "error: " + strings.ReplaceAll(e.Err, "\n", " ")
		case e.Rows == 0 && e.Affected > 0:
			outcome = fmt.Sprintf("%d affected", e.Affected)
		}
		fmt.Fprintf(&b, "-- %s · %s · %s · %s", e.At.Local().Format("2006-01-02 15:04:05.000"), strings.ReplaceAll(e.Conn, "\n", " "), e.Dur.Round(time.Millisecond), outcome)
		if e.Internal {
			b.WriteString(" · internal")
		}
		b.WriteString("\n" + strings.TrimRight(e.SQL, "\n") + "\n\n")
	}
	return b.String()
}
