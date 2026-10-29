package help

// PageInfo is one entry of the guide's table of contents.
type PageInfo struct {
	Section string // "Getting started", "Guides", "Reference"
	Title   string // the nav title on the website
	Path    string
}

// nav is the website's navigation (mkdocs.yml) for the embedded pages,
// in site order. TestPagesMatchNav keeps it in step with mkdocs.yml.
var nav = []PageInfo{
	{"Home", "Home", "index.md"},
	{"Getting started", "Install", "getting-started/install.md"},
	{"Getting started", "First 5 minutes", "getting-started/first-steps.md"},
	{"Getting started", "Check your setup (Doctor)", "getting-started/help-doctor.md"},
	{"Getting started", "Feature tour", "features.md"},
	{"Guides", "Edit and move around", "guides/editing.md"},
	{"Guides", "Find and replace across files", "guides/search.md"},
	{"Guides", "Git: stage, commit, sync, conflicts", "guides/git.md"},
	{"Guides", "Set up AI", "guides/ai-setup.md"},
	{"Guides", "Use AI", "guides/ai-use.md"},
	{"Guides", "AI profiles and pipelines", "guides/ai-profiles.md"},
	{"Guides", "AI skills", "guides/ai-skills.md"},
	{"Guides", "Itermoc, the AI home", "guides/itermoc.md"},
	{"Guides", "Watch AI runs (Runs)", "guides/runs.md"},
	{"Guides", "Connect MCP servers", "guides/mcp.md"},
	{"Guides", "Give your agent the IDE's tools", "guides/agents.md"},
	{"Guides", "Test an HTTP API (.http files)", "guides/http-client.md"},
	{"Guides", "Debug a program", "guides/debug.md"},
	{"Guides", "Run and explore tests", "guides/tests.md"},
	{"Guides", "Containers", "guides/containers.md"},
	{"Guides", "Dev services", "guides/services.md"},
	{"Guides", "Browse databases", "guides/databases.md"},
	{"Guides", "Tasks and problems", "guides/tasks-problems.md"},
	{"Guides", "The terminal and its splits", "guides/terminal.md"},
	{"Guides", "Install language servers and debuggers", "guides/language-servers.md"},
	{"Guides", "Workspaces and sessions", "guides/workspaces.md"},
	{"Guides", "Remote and SSH", "guides/remote-ssh.md"},
	{"Guides", "Customize termocode", "guides/customize.md"},
	{"Guides", "Import settings from another editor", "guides/import-from-another-editor.md"},
	{"Guides", "Write an extension", "extensions.md"},
	{"Reference", "Reference", "reference/index.md"},
	{"Reference", "Keyboard shortcuts", "reference/keys.md"},
	{"Reference", "Commands", "reference/commands.md"},
	{"Reference", "Settings", "reference/settings.md"},
	{"Reference", "Config files", "reference/config-files.md"},
	{"Reference", "Import mappings", "reference/import-mappings.md"},
	{"Reference", "AI tools", "reference/ai-tools.md"},
}

// Pages lists the built-in guide pages in site order.
func Pages() []PageInfo { return append([]PageInfo(nil), nav...) }

func navTitle(page string) string {
	for _, p := range nav {
		if p.Path == page {
			return p.Title
		}
	}
	return ""
}
