package app

import "github.com/amin-jalali/termocode/internal/secrets"

// The one bridge from the hub's files to the secret store (ADR 0020: no
// web_*.go file imports internal/secrets; webtotp_*.go reads the TOTP
// seed the same way). Push subscriptions and the VAPID key (web_push.go)
// and the GitLab token of a project's merge requests (web_project_tasks.go)
// go through it; a value never goes to the page.

// webSecretStore is the store as the hub's code holds it.
type webSecretStore = secrets.Store

// webPushStore is the secret store of the push parts (a seam for tests).
var webPushStore = func() (webSecretStore, error) { return configureSecretStore() }

// webPushValidName says whether n may name a stored secret.
func webPushValidName(n string) bool { return secrets.ValidName(n) == nil }

// webSecretValue is a stored value, with a ${env:…} / ${cmd:…}
// reference resolved on the laptop (repo: the git root it runs in).
func webSecretValue(stored, repo string) (string, error) {
	ref, err := secrets.ParseRef(stored)
	if err != nil {
		return stored, nil // a plain value
	}
	return newRefResolver(repo).Resolve(ref)
}
