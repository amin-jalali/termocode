//go:build !hive

package dbclient

import "context"

// HiveBuilt reports a build with the Hive client (-tags hive).
const HiveBuilt = false

// hiveHint is the add picker hint of the engine.
const hiveHint = "default port 10000; this build has no Hive client: rebuild with -tags hive (docs/guides/databases.md)"

func hiveDriverBackend(context.Context, HiveConfig) (hiveBackend, error) {
	return nil, errHiveMissing
}
