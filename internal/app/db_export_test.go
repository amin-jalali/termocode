package app

import (
	"context"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func SetDatabaseSessionFactoryForTest(factory func(context.Context, dbclient.Conn, string) (dbclient.Session, error)) func() {
	old := dbSQLSessionFactory
	dbSQLSessionFactory = factory
	return func() { dbSQLSessionFactory = old }
}

func (m Model) DatabaseStateForTest() (active string, sessions int) {
	if m.databases == nil {
		return "", 0
	}
	return m.databases.activeID, len(m.databases.sessions)
}
