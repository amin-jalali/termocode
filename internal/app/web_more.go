package app

import (
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// State of the v2 More views on the hub (WEB-43 … WEB-46): what each
// read-only topic sent last. Update only.

// webMorePub holds the publishers of the More views' topics.
type webMorePub struct {
	settings webSettingsPub
	mcp      webMCPPub
	ai       webAIPub                          // the ai topic (web_ai_topic.go)
	branches []string                          // the local branches git.branches listed last (git.checkout takes one of them)
	remotes  []string                          // the remote branches git.branches listed last (git.checkout's remote takes one of them)
	dbTables map[string]map[string]bool        // the tables db.tables listed last, by connection (db.rows takes one)
	ctrs     map[string]webproto.ContainerItem // the containers containers.list gave last, by id (stop and remove take one)
	ctrRaw   map[string]containers.Container   // the same, as the engine listed them (compose folder, image, ports)
	host     *webHostSeen                      // what containers.host listed last (the image, storage, context and registry actions take one)
	servers  *webServersSeen                   // the hosts tree's last lists (web_servers.go)
	rebase   *webRebasePlan                    // the list git.rebase_plan gave last (git.rebase takes its rows)
	prs      map[int]ghci.PRDetail             // the pull requests pr.view read last (the action cards name them)
	db       *webDBState                       // the Databases ops' state (web_db_more.go)
	gitLists webGitLists                       // the tags, stash entries and reflog the git lists gave last (web_git_lists.go)
}
