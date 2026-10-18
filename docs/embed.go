// Package docs embeds the user guide so the app can show it offline
// (the in-app help, see internal/help). mkdocs.yml excludes this file
// from the website.
package docs

import "embed"

// FS holds the guide pages: getting started, guides, reference and the
// top-level pages. ADRs and concepts/ stay on the website only.
//
//go:embed guides/*.md reference/*.md getting-started/*.md index.md features.md extensions.md
var FS embed.FS
