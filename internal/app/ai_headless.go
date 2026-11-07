package app

import (
	"os"

	"github.com/amin-jalali/termocode/internal/ai"
)

// HeadlessAI builds the main AI provider the way the UI does (settings,
// credentials through the app's secret store, env), for commands without
// a UI such as `termocode acp`. A locked or missing store gives a
// provider whose Stream fails with ai.ErrNotConfigured; err is a
// credentials read error worth showing.
func HeadlessAI() (ai.Provider, error) {
	_, _ = configureSecretStore() // a broken store reads as locked
	var st ai.Settings
	if p, err := ai.ConfigPath(); err == nil {
		st = ai.LoadSettings(p)
	}
	var creds ai.Credentials
	var loadErr error
	credPath, err := ai.CredentialsPath()
	if err == nil {
		creds, loadErr = ai.LoadCredentials(credPath)
	}
	cfg := ai.Resolve(st, creds, os.Getenv)
	if cfg.Kind == ai.KindClaudeOAuth && creds.ClaudeOAuth != nil && credPath != "" {
		src := &ai.TokenSource{Path: credPath}
		cfg.Token = src.Token
	}
	return ai.NewProvider(cfg), loadErr
}
