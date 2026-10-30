//go:build webdev

package assets

import (
	"embed"
	"io/fs"
)

// KitEnabled: this is a dev build; the component kit is embedded.
const KitEnabled = true

//go:embed kit.html kit.js kit.css
var kitFiles embed.FS

// KitFS returns the kit page files (dev builds only; nil otherwise).
func KitFS() fs.FS { return kitFiles }
