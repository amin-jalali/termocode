package app

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/secrets"
)

// secretsDemoPass is the passphrase of the demo store. It protects four
// made-up names in a sandbox config dir, nothing else.
const secretsDemoPass = "demo-passphrase-123"

// secretsDemo is the dev action "secretsdemo:<variant>" (shots and
// goldens): "card" is the migration card with sample counts, "locked" and
// "open" the store screen over a small encrypted file with made-up names
// (one reference of each kind). It writes secrets.age and secrets.json
// into the current config dir, so run it only in a sandbox.
func (m *Model) secretsDemo(variant string) tea.Cmd {
	switch variant {
	case "card":
		m.openSecretsMigrateCard(secretsMigratePlan{
			ask:        true,
			counts:     secrets.Counts{AIKeys: 2, ClaudeOAuth: 1, MCPSecrets: 3, MCPOAuth: 2},
			keyringWhy: "no D-Bus session",
		}, false)
		return nil
	case "locked", "open":
		dir, err := ai.ConfigDir()
		if err != nil {
			return nil
		}
		maker, err := secrets.Open(secrets.Options{
			Dir: dir, Backend: "age", ScryptWorkFactor: 14,
			PlainPath: filepath.Join(dir, "ai", "credentials.json"),
		})
		if err != nil || maker.Unlock(secretsDemoPass) != nil {
			return nil
		}
		for _, e := range [][2]string{
			{"ai.key.anthropic", "demo-value-not-a-real-key"},
			{"ai.key.openai", "${env:OPENAI_API_KEY}"},
			{"mcp.secret.GITHUB_PAT", "${cmd:gh auth token}"},
			{"db.local-pg", "${dotenv:POSTGRES_PASSWORD}"},
		} {
			if err := maker.Set(e[0], e[1]); err != nil {
				return nil
			}
		}
		maker.Lock()
		if variant == "open" {
			if s, err := configureSecretStore(); err == nil && s != nil {
				_ = s.Unlock(secretsDemoPass)
			}
		}
		return m.openSecretsStore()
	}
	return nil
}
