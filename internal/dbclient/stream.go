package dbclient

import (
	"context"
	"errors"
)

// StreamRows runs query and hands every row to sink as the server sends it.
// There is no row limit and no 30 second timeout: ctx ends it. A read-only
// session reads inside a read-only transaction; a session in a manual
// transaction reads through that transaction.
func (s *SQLSession) StreamRows(ctx context.Context, query string, args []any, sink RowSink) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("database session is closed")
	}
	ctx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	tx := s.tx
	readonly := s.readonly && tx == nil
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		s.cancel = nil
		s.mu.Unlock()
	}()
	var runner sqlRunner = s.db
	if tx != nil {
		runner = tx
	} else {
		conn, err := s.db.Conn(ctx)
		if err != nil {
			return safeSQLError(err)
		}
		defer conn.Close()
		runner = conn
		if readonly {
			if _, err := conn.ExecContext(ctx, s.dialect.ReadOnlyBegin()); err != nil {
				return safeSQLError(err)
			}
			defer func() { _, _ = conn.ExecContext(context.Background(), "ROLLBACK") }()
		}
	}
	rows, err := runner.QueryContext(ctx, query, args...)
	if err != nil {
		return streamError(ctx, err)
	}
	defer rows.Close()
	cols, err := columnsFromRows(rows)
	if err != nil {
		return safeSQLError(err)
	}
	if err := sink.Columns(cols); err != nil {
		return err
	}
	values := make([]any, len(cols))
	dests := make([]any, len(cols))
	for i := range values {
		dests[i] = &values[i]
	}
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := rows.Scan(dests...); err != nil {
			return safeSQLError(err)
		}
		row := make([]Cell, len(values))
		for i, v := range values {
			row[i] = sqlCell(v, cols[i].Type)
		}
		if err := sink.Row(row); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return streamError(ctx, err)
	}
	return nil
}

// streamError keeps a cancel a cancel (the driver may word it its own way).
func streamError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return safeSQLError(err)
}

var _ RowStreamer = (*SQLSession)(nil)
