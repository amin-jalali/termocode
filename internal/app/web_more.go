package app

import "github.com/amin-jalali/termocode/internal/webproto"

// State of the v2 More views on the hub (WEB-43 … WEB-46): what each
// read-only topic sent last. Update only.

// webMorePub holds the publishers of the More views' topics.
type webMorePub struct {
	settings webSettingsPub
	mcp      webMCPPub
	branches []string                          // the local branches git.branches listed last (git.checkout takes one of them)
	ctrs     map[string]webproto.ContainerItem // the containers containers.list gave last, by id (stop and remove take one)
}
