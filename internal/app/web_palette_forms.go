package app

// The MCP setup commands from the web (WEBP-9). The AI may never run
// them (they add servers or sign in to services: the user's AI setup),
// but the web user is the user, so the web palette offers them, always
// behind a laptop card (palette.ask). After the laptop's yes the
// laptop's own form opens on the laptop: sign-ins, keys and tokens are
// typed there, never on the web page, and nothing secret goes over the
// link. Each says what its form does.
var webPaletteLaptopForms = map[string]string{
	"ai-mcp-catalog":   "browse the MCP catalog and add a server",
	"ai-mcp-bundle":    "install an MCP bundle (.mcpb) from a file on the laptop",
	"ai-mcp-connect":   "sign in to a service (GitHub, Linear, Slack, Notion, Sentry)",
	"mcp-import-other": "import MCP servers from other apps on the laptop",
}

// webPaletteFormNote is the laptop card's line for a form command.
const webPaletteFormNote = "Its form opens here, on the laptop: sign-ins, keys and tokens are typed here, on the laptop, never on the web."
