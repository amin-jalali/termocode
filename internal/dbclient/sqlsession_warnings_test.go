package dbclient

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"github.com/amin-jalali/termocode/internal/dbclient/dbtest"
	"testing"
)

type warningRunner struct {
	db   *sql.DB
	live bool
}

func (r *warningRunner) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.live = true
	return r.db.QueryContext(ctx, q, args...)
}
func (r *warningRunner) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return r.db.ExecContext(ctx, q, args...)
}
func TestMySQLWarningsAfterDeadline(t *testing.T) {
	stub := &dbtest.Stub{Columns: []string{"Level", "Code", "Message"}, Rows: [][]driver.Value{{"Warning", int64(1969), "maximum statement execution time exceeded"}}}
	db := dbtest.Open("warnings-deadline", stub)
	defer db.Close()
	runner := &warningRunner{db: db}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := mysqlWarningsInterrupted(ctx, runner); err == nil || !runner.live {
		t.Fatalf("warnings used expired context: live=%v err=%v", runner.live, err)
	}
}
