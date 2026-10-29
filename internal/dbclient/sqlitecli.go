package dbclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/lazyre"
	"github.com/amin-jalali/termocode/internal/spawn"
)

var lookupSQLite = func() (string, error) { return exec.LookPath("sqlite3") }

// hostOS and readOSRelease are the host facts behind install hints. Tests
// replace them so results do not depend on the machine running the tests.
var (
	hostOS        = func() (string, string) { return runtime.GOOS, runtime.GOARCH }
	readOSRelease = func() (string, error) {
		b, err := os.ReadFile("/etc/os-release")
		return string(b), err
	}
)

// SQLiteVersion is the parsed sqlite3 CLI version.
type SQLiteVersion struct{ Major, Minor, Patch int }

func ParseSQLiteVersion(text string) (SQLiteVersion, error) {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return SQLiteVersion{}, errors.New("could not read sqlite3 version")
	}
	parts := strings.Split(fields[0], ".")
	if len(parts) < 2 || len(parts) > 3 {
		return SQLiteVersion{}, fmt.Errorf("could not read sqlite3 version %q", fields[0])
	}
	var v SQLiteVersion
	values := []*int{&v.Major, &v.Minor, &v.Patch}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return SQLiteVersion{}, fmt.Errorf("could not read sqlite3 version %q", fields[0])
		}
		*values[i] = n
	}
	if v.Major < 3 || (v.Major == 3 && v.Minor < 33) {
		return v, fmt.Errorf("sqlite3 %s is too old; version 3.33 or newer is needed", fields[0])
	}
	return v, nil
}

func sqliteHostInstallHint() string {
	goos, goarch := hostOS()
	return SQLiteInstallHint(goos, goarch)
}

func SQLiteInstallHint(goos, goarch string) string {
	var release string
	if goos == "linux" {
		if b, err := readOSRelease(); err == nil {
			release = b
		}
	}
	return SQLiteInstallHintWithRelease(goos, goarch, release)
}

// SQLiteInstallHintWithRelease makes OS package guidance deterministic for
// callers that already know the operating system or need a stable preview.
func SQLiteInstallHintWithRelease(goos, goarch, release string) string {
	_ = goarch
	switch goos {
	case "android":
		return "Install sqlite3 with: pkg install sqlite"
	case "darwin":
		return "Install sqlite3 with: brew install sqlite"
	case "linux":
		id := strings.ToLower(release)
		if strings.Contains(id, "fedora") {
			return "Install sqlite3 with: sudo dnf install sqlite"
		}
		if strings.Contains(id, "debian") || strings.Contains(id, "ubuntu") {
			return "Install sqlite3 with: sudo apt install sqlite3"
		}
		return "Install sqlite3 with your package manager"
	default:
		return "Install sqlite3 with your package manager"
	}
}

type SQLiteSession struct {
	conn          Conn
	readonly      bool
	mu            sync.Mutex
	stateMu       sync.Mutex
	proc          *sqliteProcess
	activeCancel  context.CancelFunc
	seq           uint64
	transaction   bool
	txBegun       bool // the open transaction began with BEGIN (not a savepoint)
	restartNotice string
}

type sqliteProcess struct {
	cmd     *exec.Cmd
	in      io.WriteCloser
	out     *bufio.Reader
	outPipe *os.File
}

func NewSQLiteSession(ctx context.Context, conn Conn) (*SQLiteSession, error) {
	path, err := lookupSQLite()
	if err != nil {
		return nil, fmt.Errorf("sqlite3 is not installed. %s", sqliteHostInstallHint())
	}
	checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cmd := spawn.CommandContext(checkCtx, path, "-version")
	b, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("could not check sqlite3 version: %w", err)
	}
	v, err := ParseSQLiteVersion(string(b))
	if err != nil {
		return nil, fmt.Errorf("%s. %s", err, sqliteHostInstallHint())
	}
	if conn.ReadOnly && (v.Major < 3 || (v.Major == 3 && v.Minor < 37)) {
		return nil, errors.New("Update sqlite3 to 3.37 or newer to let the AI read this database")
	}
	s := &SQLiteSession{conn: conn, readonly: conn.ReadOnly}
	if err := s.start(path, conn.ReadOnly); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *SQLiteSession) start(path string, readonly bool) error {
	args := []string{"-batch", "-json"}
	if readonly {
		args = append(args, "-readonly", "-safe")
	}
	args = append(args, s.conn.Path)
	cmd := spawn.CommandContext(context.Background(), path, args...)
	setSQLiteProcessGroup(cmd)
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	read, write, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.Stdout = write
	cmd.Stderr = write
	if err := cmd.Start(); err != nil {
		_ = read.Close()
		_ = write.Close()
		return fmt.Errorf("start sqlite3: %w", err)
	}
	_ = write.Close()
	p := &sqliteProcess{cmd: cmd, in: in, out: bufio.NewReaderSize(read, 64*1024), outPipe: read}
	s.stateMu.Lock()
	s.proc = p
	s.readonly = readonly
	s.stateMu.Unlock()
	for _, setup := range []string{".changes on", ".timeout 5000", ".bail off", "PRAGMA foreign_keys = ON;"} {
		if _, _, err := s.exchangeLocked(context.Background(), setup); err != nil {
			s.stop(p)
			return fmt.Errorf("configure sqlite3: %w", err)
		}
	}
	return nil
}

func (s *SQLiteSession) exchangeLocked(ctx context.Context, sql string, maxRows ...int) ([]string, int64, error) {
	s.stateMu.Lock()
	p := s.proc
	s.stateMu.Unlock()
	if p == nil {
		return nil, 0, errors.New("sqlite3 session is closed")
	}
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	s.seq++
	sentinel := fmt.Sprintf("__TC_END_%d__", s.seq)
	if _, err := io.WriteString(p.in, sql+"\n;\n.print "+sentinel+"\n"); err != nil {
		writeErr := err
		if restartErr := s.restartProcess(p, false); restartErr != nil {
			return nil, 0, fmt.Errorf("write sqlite3 statement: %w; session restart failed: %v", writeErr, restartErr)
		}
		return nil, 0, fmt.Errorf("write sqlite3 statement: %w", writeErr)
	}
	type response struct {
		lines []string
		err   error
	}
	ch := make(chan response, 1)
	limit := 0
	if len(maxRows) > 0 {
		limit = maxRows[0]
	}
	crash.Go("the SQLite console", func() {
		var lines []string
		rowsSeen := 0
		truncated := false
		for {
			line, e := p.out.ReadString('\n')
			if len(line) > 0 {
				line = strings.TrimRight(line, "\r\n")
				if line == sentinel {
					if truncated {
						var changes []string
						kept := lines[:0]
						for _, saved := range lines {
							if strings.HasPrefix(strings.TrimSpace(saved), "changes:") {
								changes = append(changes, saved)
							} else {
								kept = append(kept, saved)
							}
						}
						lines = kept
						if len(lines) > 0 {
							lines[len(lines)-1] = strings.TrimSuffix(lines[len(lines)-1], ",")
						}
						lines = append(lines, "]")
						lines = append(lines, changes...)
						lines = append(lines, "__TC_TRUNCATED__")
					}
					ch <- response{lines: lines}
					return
				}
				trim := strings.TrimSpace(line)
				if limit > 0 && strings.HasPrefix(strings.TrimLeft(trim, "["), "{") {
					rowsSeen++
					if rowsSeen > limit {
						truncated = true
						continue
					}
				}
				if !truncated || strings.HasPrefix(trim, "changes:") {
					lines = append(lines, line)
				}
			}
			if e != nil {
				ch <- response{err: e}
				return
			}
		}
	})
	select {
	case r := <-ch:
		if r.err != nil {
			stopErr := r.err
			s.restartProcess(p, false)
			return nil, 0, fmt.Errorf("sqlite3 stopped: %w", stopErr)
		}
		for _, line := range r.lines {
			if isSQLiteError(line) {
				return r.lines, parseChanges(r.lines), errors.New(cleanSQLiteError(line))
			}
		}
		return r.lines, parseChanges(r.lines), nil
	case <-ctx.Done():
		// Prefer a completed sentinel before signaling: sqlite3 may exit after SIGINT
		// in batch mode, even when it has just finished the query.
		select {
		case r := <-ch:
			if r.err == nil {
				for _, line := range r.lines {
					if isSQLiteError(line) {
						return r.lines, parseChanges(r.lines), errors.New(cleanSQLiteError(line))
					}
				}
				return r.lines, parseChanges(r.lines), nil
			}
			if restartErr := s.restartProcess(p, true); restartErr != nil {
				return nil, 0, fmt.Errorf("%w; sqlite3 session restart failed: %v", ctx.Err(), restartErr)
			}
			return nil, 0, &cancelRaceError{cause: ctx.Err()}
		default:
		}
		interruptSQLiteProcess(p)
		t := time.NewTimer(2 * time.Second)
		defer t.Stop()
		select {
		case <-ch:
			if err := s.restartProcess(p, true); err != nil {
				return nil, 0, fmt.Errorf("%w; sqlite3 session restart failed: %v", ctx.Err(), err)
			}
			return nil, 0, &cancelRaceError{cause: ctx.Err()}
		case <-t.C:
			if err := s.restartProcess(p, true); err != nil {
				return nil, 0, fmt.Errorf("%w; sqlite3 session restart failed: %v", ctx.Err(), err)
			}
			return nil, 0, &cancelRaceError{cause: ctx.Err()}
		}
	}
}

// cancelRaceError marks a cancel that happened after the statement was sent,
// so a write may already have finished.
type cancelRaceError struct{ cause error }

func (e *cancelRaceError) Error() string { return e.cause.Error() }
func (e *cancelRaceError) Unwrap() error { return e.cause }

type cancelMaybeFinishedError struct{ cause error }

func (e *cancelMaybeFinishedError) Error() string {
	return "cancelled · the statement may already have finished"
}
func (e *cancelMaybeFinishedError) Unwrap() error { return e.cause }

func (s *SQLiteSession) restartProcess(p *sqliteProcess, cancelled bool) error {
	// sqlite3 runs in batch mode here; SIGINT can terminate that shell, so
	// cancellation discards the CLI process and starts a fresh connection.
	s.stateMu.Lock()
	if s.proc == p {
		s.proc = nil
	}
	readonly := s.readonly
	s.stateMu.Unlock()
	s.stop(p)
	s.markSessionRestarted(cancelled)
	path, err := lookupSQLite()
	if err != nil {
		return err
	}
	return s.start(path, readonly)
}

func isSQLiteError(line string) bool {
	line = strings.TrimSpace(line)
	return strings.HasPrefix(line, "Error:") || strings.HasPrefix(line, "Parse error") || strings.HasPrefix(line, "Runtime error")
}

var sqliteLineNumber = lazyre.New(`(?i)\s+near line [0-9]+`)

func cleanSQLiteError(line string) string {
	return strings.TrimSpace(sqliteLineNumber.ReplaceAllString(line, ""))
}

func parseChanges(lines []string) int64 {
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "changes:") {
			fields := strings.Fields(strings.TrimPrefix(line, "changes:"))
			if len(fields) > 0 {
				n, _ := strconv.ParseInt(fields[0], 10, 64)
				return n
			}
		}
	}
	return 0
}

func (s *SQLiteSession) markSessionRestarted(cancelled bool) {
	notice := "sqlite3 stopped · the session was restarted"
	if cancelled {
		notice = "cancelled · the session was restarted"
	}
	if s.transaction {
		s.transaction = false
		notice += "; the open transaction was rolled back"
	}
	s.restartNotice = notice
}

func (s *SQLiteSession) takeRestartNotice() string {
	if s.restartNotice == "" {
		return ""
	}
	notice := s.restartNotice
	s.restartNotice = ""
	return notice
}

func (s *SQLiteSession) Run(ctx context.Context, stmt string, opts RunOptions) Result {
	start := time.Now()
	res := Result{}
	statements, splitErr := Split(stmt, string(DialectSQLite))
	if splitErr != nil {
		res.Err = splitErr
		return res
	}
	if len(statements) != 1 {
		res.Err = errors.New("run one SQL statement at a time")
		return res
	}
	stmt = strings.TrimSpace(statements[0])
	words := tokenWordsDialect(stmt, DialectSQLite)
	if s.readonly && sqliteUnsafeWords(words) {
		if containsSequence(words, "VACUUM") && containsSequence(words, "INTO") {
			res.Err = errors.New("VACUUM INTO is not allowed in the read-only SQLite session")
		} else if containsSequence(words, "WRITABLE_SCHEMA") {
			res.Err = errors.New("PRAGMA writable_schema is not allowed in the read-only SQLite session")
		} else {
			res.Err = errors.New("statement is not allowed in the SQLite session")
		}
		return res
	}
	if s.readonly {
		if w := twinBlockedIdentifier(stmt); w != "" {
			res.Err = fmt.Errorf("%s is not allowed in the read-only SQLite session", strings.ToLower(w))
			return res
		}
	}
	for _, line := range strings.Split(stmt, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), ".") {
			res.Err = errors.New("sqlite shell commands are not sent")
			return res
		}
	}
	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		res.Err = err
		return res
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.stateMu.Lock()
	s.activeCancel = cancel
	s.stateMu.Unlock()
	var lines []string
	var affected int64
	var err error
	if len(opts.Args) > 0 {
		err = s.bindLocked(runCtx, opts.Args)
	}
	if err == nil {
		sent := stmt
		if opts.MaxRows > 0 {
			// One row more than kept: the cut still marks the result truncated.
			sent, _ = sqliteRowLimit(stmt, opts.MaxRows+1)
		}
		lines, affected, err = s.exchangeLocked(runCtx, sent, opts.MaxRows)
	}
	if len(opts.Args) > 0 {
		// Unbound parameters of later statements are NULL again.
		_, _, _ = s.exchangeLocked(context.Background(), ".parameter clear")
	}
	if err == nil {
		s.trackTransaction(words)
	}
	cancel()
	s.stateMu.Lock()
	s.activeCancel = nil
	s.stateMu.Unlock()
	notice := s.takeRestartNotice()
	s.mu.Unlock()
	res.Elapsed = time.Since(start)
	if isDML(words) {
		res.Affected = affected
	}
	var race *cancelRaceError
	if errors.As(err, &race) {
		if kind, _ := Classify(stmt, string(DialectSQLite)); kind != KindRead {
			err = &cancelMaybeFinishedError{cause: race.cause}
		}
	}
	res.Err = err
	if notice != "" {
		res.Notices = append(res.Notices, notice)
	}
	if err != nil {
		return res
	}
	if len(lines) > 0 && lines[len(lines)-1] == "__TC_TRUNCATED__" {
		res.Truncated = true
		lines = lines[:len(lines)-1]
	}
	if rows, ok := sqliteQueryPlan(words, lines); ok {
		res.Columns = []Column{{Name: "plan"}}
		res.Rows = rows
		return res
	}
	rows, cols, err := parseSQLiteJSON(lines)
	if err != nil {
		res.Err = err
		return res
	}
	res.Rows = rows
	res.Columns = cols
	if i, j, ok := sqliteFindBlob(lines, res.Rows); ok {
		res.Rows[i][j].Kind = "binary"
		res.Notices = append(res.Notices, "BLOB values are shown as text by sqlite3's JSON output")
	}
	return res
}

// sqliteFindBlob returns the first cell that holds BLOB bytes. Some builds
// (macOS) print each high byte as a \u0080-\u00ff escape, which decodes to
// ordinary Latin-1 text; real UTF-8 text is printed raw, never escaped, so
// such an escape in the raw output marks the cell holding that rune.
func sqliteFindBlob(lines []string, rows [][]Cell) (int, int, bool) {
	byteEscape := sqliteRawByteEscape.MatchString(strings.Join(lines, "\n"))
	for i := range rows {
		for j := range rows[i] {
			text := rows[i][j].Text
			if sqliteJSONBlob(text) || byteEscape && strings.IndexFunc(text, func(r rune) bool { return r >= 0x80 && r <= 0xff }) >= 0 {
				return i, j, true
			}
		}
	}
	return 0, 0, false
}

// sqliteQueryPlan reads EXPLAIN QUERY PLAN output: sqlite3 prints it as a
// text tree ("QUERY PLAN" then "|--SCAN …") even in -json mode. Each tree
// line becomes a row of one "plan" column.
func sqliteQueryPlan(words, lines []string) ([][]Cell, bool) {
	if len(words) < 3 || words[0] != "EXPLAIN" || words[1] != "QUERY" || words[2] != "PLAN" {
		return nil, false
	}
	var rows [][]Cell
	for _, line := range lines {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "changes:") {
			continue
		}
		if len(rows) == 0 && strings.HasPrefix(strings.TrimSpace(line), "[") {
			return nil, false // JSON after all (a future sqlite3)
		}
		rows = append(rows, []Cell{{Text: line}})
	}
	return rows, true
}

func isDML(words []string) bool {
	for _, word := range words {
		switch word {
		case "INSERT", "UPDATE", "DELETE", "REPLACE", "MERGE":
			return true
		}
	}
	return false
}

func sqliteJSONBlob(text string) bool {
	// sqlite3's JSON mode renders non-text bytes differently by version and
	// platform: u plus eight hex digits (sign-extended byte), a bare control
	// escape such as u0000, or real invalid UTF-8 / U+FFFD. All mean binary.
	return sqliteJSONBlobEscape.MatchString(text) || !utf8.ValidString(text) || strings.ContainsRune(text, utf8.RuneError)
}

var sqliteRawByteEscape = lazyre.New(`(?i)\\u00[89a-f][0-9a-f]`)

var sqliteJSONBlobEscape = lazyre.New(`(?i)u[0-9a-f]{8}|u00[01][0-9a-f]`)

// parseSQLiteJSONDecoder is parseSQLiteJSON through encoding/json: slow
// (a decoder per cell), but it reads any JSON and reports its errors.
func parseSQLiteJSONDecoder(lines []string) ([][]Cell, []Column, error) {
	var data []byte
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "changes:") {
			continue
		}
		data = append(data, []byte(line)...)
		data = append(data, '\n')
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, nil, err
	}
	if tok != json.Delim('[') {
		return nil, nil, fmt.Errorf("unexpected sqlite3 JSON output: %s", strings.Join(lines, "\\n"))
	}
	var rows [][]Cell
	var cols []Column
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, nil, err
		}
		if t != json.Delim('{') {
			return nil, nil, fmt.Errorf("unexpected sqlite3 JSON row")
		}
		var row []Cell
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return nil, nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, nil, fmt.Errorf("invalid sqlite3 JSON column")
			}
			var value json.RawMessage
			if err := dec.Decode(&value); err != nil {
				return nil, nil, err
			}
			cell := Cell{}
			if string(value) == "null" {
				cell.Null = true
			} else {
				var v any
				d := json.NewDecoder(bytes.NewReader(value))
				d.UseNumber()
				if err := d.Decode(&v); err != nil {
					return nil, nil, err
				}
				cell = CellOf(v)
			}
			row = append(row, cell)
			if len(rows) == 0 {
				cols = append(cols, Column{Name: name})
			}
		}
		if _, err := dec.Token(); err != nil {
			return nil, nil, err
		}
		rows = append(rows, row)
	}
	if t, err := dec.Token(); err != nil || t != json.Delim(']') {
		return nil, nil, errors.New("sqlite3 output ended early")
	}
	return rows, cols, nil
}

// bindLocked fills the sqlite3 shell's parameter table, which binds ?1,
// ?2, … of the next statement: the shell has no other way to take a bound
// value. Each value travels as a hex literal (only 0-9 A-F, never SQL
// text) and is stored as TEXT or NULL; the statement itself only holds
// placeholders.
func (s *SQLiteSession) bindLocked(ctx context.Context, args []any) error {
	if _, _, err := s.exchangeLocked(ctx, ".parameter init"); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("REPLACE INTO temp.sqlite_parameters(key, value) VALUES ")
	for i, a := range args {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "('?%d', %s)", i+1, sqliteBindLiteral(a))
	}
	_, _, err := s.exchangeLocked(ctx, b.String())
	return err
}

func sqliteBindLiteral(v any) string {
	switch x := v.(type) {
	case nil:
		return "NULL"
	case []byte:
		return fmt.Sprintf("X'%X'", x)
	case string:
		return fmt.Sprintf("CAST(X'%X' AS TEXT)", x)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", x)
	case bool:
		if x {
			return "1"
		}
		return "0"
	default:
		return fmt.Sprintf("CAST(X'%X' AS TEXT)", fmt.Sprint(x))
	}
}

func (s *SQLiteSession) trackTransaction(words []string) {
	if len(words) == 0 {
		return
	}
	switch words[0] {
	case "BEGIN":
		s.transaction, s.txBegun = true, true
	case "SAVEPOINT":
		if !s.transaction {
			s.transaction, s.txBegun = true, false
		}
	case "COMMIT", "END":
		s.transaction, s.txBegun = false, false
	case "RELEASE":
		// Only the savepoint that began the transaction ends it; a
		// savepoint inside BEGIN … COMMIT does not.
		if !s.txBegun {
			s.transaction = false
		}
	case "ROLLBACK":
		if len(words) < 2 || words[1] != "TO" {
			s.transaction, s.txBegun = false, false
		}
	}
}

func (s *SQLiteSession) Ping(ctx context.Context) error {
	r := s.Run(ctx, "SELECT 1", RunOptions{})
	return r.Err
}
func (s *SQLiteSession) Begin(ctx context.Context) error {
	r := s.Run(ctx, "BEGIN", RunOptions{})
	return r.Err
}
func (s *SQLiteSession) Commit(ctx context.Context) error {
	r := s.Run(ctx, "COMMIT", RunOptions{})
	return r.Err
}
func (s *SQLiteSession) Rollback(ctx context.Context) error {
	r := s.Run(ctx, "ROLLBACK", RunOptions{})
	return r.Err
}
func (s *SQLiteSession) ReadOnlySession() (Session, error) {
	c := s.conn
	c.ReadOnly = true
	return NewSQLiteSession(context.Background(), c)
}
func (s *SQLiteSession) Cancel() {
	s.stateMu.Lock()
	cancel := s.activeCancel
	s.stateMu.Unlock()
	if cancel != nil {
		cancel()
	}
}
func (s *SQLiteSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stateMu.Lock()
	p := s.proc
	s.proc = nil
	s.stateMu.Unlock()
	if p == nil {
		return nil
	}
	_ = p.in.Close()
	_ = p.cmd.Wait()
	_ = p.outPipe.Close()
	return nil
}
func (s *SQLiteSession) stop(p *sqliteProcess) {
	if p == nil || p.cmd.Process == nil {
		return
	}
	killSQLiteProcess(p)
	_ = p.in.Close()
	_ = p.cmd.Wait()
	_ = p.outPipe.Close()
}
