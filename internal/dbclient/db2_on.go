//go:build db2

package dbclient

import (
	"database/sql/driver"

	_ "github.com/ibmdb/go_ibm_db" // cgo: needs IBM's CLI driver (clidriver)
)

// Db2Built reports a build with the Db2 driver (-tags db2).
const Db2Built = true

// db2Hint is the add picker hint of the engine.
const db2Hint = "default port 50000; the database is the Db2 database name"

func db2DriverConnector(dsn string) (driver.Connector, error) {
	return dsnConnector("go_ibm_db", dsn)
}
