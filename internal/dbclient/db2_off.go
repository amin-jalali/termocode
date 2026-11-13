//go:build !db2

package dbclient

import "database/sql/driver"

// Db2Built reports a build with the Db2 driver (-tags db2).
const Db2Built = false

// db2Hint is the add picker hint of the engine.
const db2Hint = "default port 50000; this build has no Db2 driver: rebuild with -tags db2 (docs/guides/databases.md)"

func db2DriverConnector(string) (driver.Connector, error) { return nil, errDb2Missing }
