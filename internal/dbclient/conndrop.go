package dbclient

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"syscall"
)

// connLost reports whether err is a dropped connection (the server or the
// network went away), not an error the server sent about the query.
func connLost(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) || errors.Is(err, driver.ErrBadConn) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	m := strings.ToLower(err.Error())
	for _, s := range []string{"unexpected eof", "connection reset", "broken pipe", "bad connection", "invalid connection", "use of closed network connection", "server closed the connection"} {
		if strings.Contains(m, s) {
			return true
		}
	}
	return false
}

// rowCount is "1 row" or "N rows".
func rowCount(n int) string {
	if n == 1 {
		return "1 row"
	}
	return strconv.Itoa(n) + " rows"
}

// lostMidQuery is the error for a connection that dropped while rows were
// arriving: the rows read so far are kept, but the result is not whole.
func lostMidQuery(cause error, rows int) error {
	return errors.New("the connection was lost mid-query after " + rowCount(rows) + "; the result is not complete: " + Redact(cause.Error()))
}
