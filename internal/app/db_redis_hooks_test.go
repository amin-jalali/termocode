package app

import "github.com/amin-jalali/termocode/internal/dbclient"

// SetRedisDialForTest makes Redis sessions dial through dial (a fake
// server over net.Pipe); the returned func restores the default.
func SetRedisDialForTest(dial dbclient.RedisDialFunc) func() {
	old := dbRedisDial
	dbRedisDial = dial
	return func() { dbRedisDial = old }
}
