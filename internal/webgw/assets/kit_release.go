//go:build !webdev

package assets

import "io/fs"

// KitEnabled: a release build carries no component kit.
const KitEnabled = false

// KitFS returns the kit page files (dev builds only; nil otherwise).
func KitFS() fs.FS { return nil }
