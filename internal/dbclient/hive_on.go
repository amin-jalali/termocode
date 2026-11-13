//go:build hive

package dbclient

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/beltran/gohive"
)

// HiveBuilt reports a build with the Hive client (-tags hive).
const HiveBuilt = true

// hiveHint is the add picker hint of the engine.
const hiveHint = "default port 10000; HiveServer2 or the Spark Thrift Server; the database is optional"

// gohiveBackend is one gohive session.
type gohiveBackend struct {
	conn *gohive.Connection
}

func hiveDriverBackend(ctx context.Context, c HiveConfig) (hiveBackend, error) {
	cfg := gohive.NewConnectConfiguration()
	cfg.Username, cfg.Password, cfg.Service = c.Username, c.Password, c.Service
	cfg.Database = c.Database
	cfg.TransportMode = c.Transport
	if c.HTTPPath != "" {
		cfg.HTTPPath = c.HTTPPath
	}
	cfg.TLSConfig = c.TLS
	cfg.ConnectTimeout = 15 * time.Second
	if c.Dial != nil {
		cfg.DialContext = gohive.DialContextFunc(c.Dial)
	}
	conn, err := gohive.Connect(c.Host, c.Port, c.Auth, cfg)
	if err != nil {
		return nil, errors.New(Redact(err.Error()))
	}
	return &gohiveBackend{conn: conn}, nil
}

func (b *gohiveBackend) Close() error { return b.conn.Close() }

func (b *gohiveBackend) Query(ctx context.Context, q string) (hiveResult, error) {
	cur := b.conn.Cursor()
	cur.Exec(ctx, q)
	if cur.Err != nil {
		err := errors.New(Redact(cur.Err.Error()))
		cur.Close()
		return nil, err
	}
	r := &gohiveRows{cur: cur, ctx: ctx}
	for _, d := range cur.Description() {
		if len(d) < 2 {
			continue
		}
		r.cols = append(r.cols, d[0])
		r.types = append(r.types, strings.TrimSuffix(d[1], "_TYPE"))
	}
	// A statement without a result set (USE, SET, INSERT) has no columns.
	if len(r.cols) == 0 {
		cur.Err = nil
	}
	return r, nil
}

type gohiveRows struct {
	cur         *gohive.Cursor
	ctx         context.Context
	cols, types []string
}

func (r *gohiveRows) Columns() []string { return r.cols }
func (r *gohiveRows) Types() []string   { return r.types }
func (r *gohiveRows) Close() error      { r.cur.Close(); return nil }
func (r *gohiveRows) Next() ([]driver.Value, error) {
	if len(r.cols) == 0 || !r.cur.HasMore(r.ctx) {
		if err := r.cur.Error(); err != nil {
			return nil, errors.New(Redact(err.Error()))
		}
		return nil, io.EOF
	}
	m := r.cur.RowMap(r.ctx)
	if err := r.cur.Error(); err != nil {
		return nil, errors.New(Redact(err.Error()))
	}
	row := make([]driver.Value, len(r.cols))
	for i, name := range r.cols {
		row[i] = hiveValue(m[name])
	}
	return row, nil
}

// hiveValue turns a gohive value into one database/sql can scan.
func hiveValue(v any) driver.Value {
	switch x := v.(type) {
	case int8:
		return int64(x)
	case int16:
		return int64(x)
	case int32:
		return int64(x)
	case float32:
		return float64(x)
	}
	return v
}
