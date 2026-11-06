package source

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

type fakeDBSession struct {
	dbclient.Session
	ran     []string
	res     dbclient.Result
	closed  bool
	maxRows int
}

func (f *fakeDBSession) Run(ctx context.Context, sql string, o dbclient.RunOptions) dbclient.Result {
	f.ran = append(f.ran, sql)
	f.maxRows = o.MaxRows
	return f.res
}
func (f *fakeDBSession) Close() error { f.closed = true; return nil }

type fakeOpener struct {
	sess   *fakeDBSession
	err    error
	opened int
}

func (o *fakeOpener) OpenReadOnly(ctx context.Context, conn string) (dbclient.Session, string, error) {
	o.opened++
	return o.sess, "postgres", o.err
}

func dbResult() dbclient.Result {
	return dbclient.Result{
		Columns: []dbclient.Column{{Name: "id"}, {Name: "name"}},
		Rows: [][]dbclient.Cell{
			{{Text: "1"}, {Text: "ann"}},
			{{Text: "2"}, {Null: true}},
			{{Text: "3"}, {Text: "cy"}},
		},
	}
}

func TestDBSelect(t *testing.T) {
	sess := &fakeDBSession{res: dbResult()}
	op := &fakeOpener{sess: sess}
	s := &Sources{Databases: op}
	r, err := s.DB(context.Background(), DBInput{Conn: "main", SQL: "SELECT id, name FROM t", Shape: Shape{MaxItems: 2, Key: "$.id"}})
	if err != nil || keys(r) != "1 2" || !r.Truncated || r.Total != 3 {
		t.Fatalf("select: %+v %v", r, err)
	}
	if string(r.Items[1].Data) != `{"id":"2","name":null}` {
		t.Errorf("data %s", r.Items[1].Data)
	}
	if !sess.closed || sess.maxRows != 3 {
		t.Errorf("closed %v maxRows %d", sess.closed, sess.maxRows)
	}
}

func TestDBRefusesNonReadOnlySQL(t *testing.T) {
	sess := &fakeDBSession{res: dbResult()}
	op := &fakeOpener{sess: sess}
	s := &Sources{Databases: op}
	for _, q := range []string{
		"DELETE FROM t",
		"UPDATE t SET a=1",
		"SELECT 1; DROP TABLE t",
		"INSERT INTO t VALUES (1)",
		"DROP TABLE t",
		"",
		"WITH x AS (DELETE FROM t RETURNING *) SELECT * FROM x",
	} {
		_, err := s.DB(context.Background(), DBInput{Conn: "main", SQL: q})
		if !errors.Is(err, ErrNotReadOnly) {
			t.Errorf("%q: err %v", q, err)
		}
	}
	if len(sess.ran) != 0 || op.opened != 0 {
		t.Errorf("ran %v opened %d", sess.ran, op.opened)
	}
}

func TestDBErrorsAndSecrets(t *testing.T) {
	secret := "pw-" + "s3cret"
	op := &fakeOpener{err: errors.New("dial postgres://u:" + secret + "@h/db failed")}
	s := &Sources{Databases: op}
	_, err := s.DB(context.Background(), DBInput{Conn: "main", SQL: "SELECT 1"})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("open err leaks: %v", err)
	}
	sess := &fakeDBSession{res: dbclient.Result{Err: errors.New("failed password=" + secret)}}
	s = &Sources{Databases: &fakeOpener{sess: sess}}
	_, err = s.DB(context.Background(), DBInput{Conn: "main", SQL: "SELECT 1"})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("run err leaks: %v", err)
	}
	if _, err := (&Sources{}).DB(context.Background(), DBInput{Conn: "m", SQL: "SELECT 1"}); err == nil {
		t.Error("no opener: want an error")
	}
}
