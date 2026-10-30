package app

// State of the v2 More views on the hub (WEB-43 … WEB-46): what each
// read-only topic sent last. Update only.

// webMorePub holds the publishers of the More views' topics.
type webMorePub struct {
	settings webSettingsPub
	mcp      webMCPPub
}
