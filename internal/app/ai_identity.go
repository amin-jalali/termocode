package app

import (
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/ai"
)

// Who the AI is (every step, every provider): the AI inside termocode on
// the owner's own computer, in this project, with termocode's tools. It
// opens the system prompt, before the project rules, so a CLI step
// (Codex, Claude Code) never goes to its own docs or skills to learn
// what it is, and a question about data goes to the db_* tools, not to
// guesses from the project files.

// aiIdentityEnd ends the identity paragraph; the tool guide of a step
// goes right after it (aiWithToolGuide).
const aiIdentityEnd = "When the owner asks what you can do, answer from this text.\n"

// aiIdentityPrompt is the identity paragraph for the project at root.
func aiIdentityPrompt(root string) string {
	name := filepath.Base(filepath.Clean(root))
	return "You are the AI inside termocode, the owner's IDE, running on the owner's own computer in project " + name +
		". You talk with the owner in termocode's chat and work through termocode's tools on this machine. " +
		"Do not read your own docs, skills or config files, and do not search the web, to learn what you are or what you can do: " +
		"you are termocode's AI and this text says what you can do. " + aiActRules + aiIdentityEnd
}

// aiActRules is how the AI works on every step: it acts with its tools
// and does not ask or offer (the owner's chat: "how many tables do we
// have on remote?" got "there is no remote database", then the answer
// "BaaytonaDB" got "what would you like to inspect?").
const aiActRules = "Act, do not ask. When one of your tools can answer, call it now: do not ask the owner first and do not offer to do it. " +
	"When the owner answers with only a name (a connection, container, server, file), it is the answer to your last question: " +
	"carry the earlier request on and do it (\"how many tables on remote?\", then \"BaaytonaDB\": count BaaytonaDB's tables). " +
	"\"Remote\" means anything that is not on this computer: a connection through an SSH tunnel (even when its host is 127.0.0.1), " +
	"a docker context on ssh://, a container on such a context, a host that is another machine; db_connections and contexts_list say which ones are remote. " +
	"termocode's tools reach those servers: never say you cannot reach a server before you tried them; when one fails, give its error and the next step. " +
	"Never give the owner a command to run in place of a tool. Ask only when two or more things fit and the tools cannot tell which one is meant. " +
	"Answer in the same language as the owner. "

// aiToolGroup is one kind of tool in the guide: its names (a name ending
// in "_" is a prefix) and when to use them.
type aiToolGroup struct {
	title string
	names []string
	when  string
}

var aiToolGroups = []aiToolGroup{
	{"Files", []string{"read_file", "list_dir", "search", "glob", "ast_grep", "write_file", "edit_file", "create_file", "create_dir", "delete", "rename"}, "read and change the project's files"},
	{"Terminal", []string{"run_command", "terminal_send", "terminal_read", "terminal_tabs", "terminal_keys"}, "run commands, read and use the owner's terminals"},
	{"Git", []string{"git_"}, "status, diffs, history, branches, commits"},
	{"Databases", []string{"db_", "redis_"}, "the owner's database connections, on this computer or on a server: tables, schema, data. Use them for every question about a database; never guess from project files"},
	{"Containers and servers", []string{"contexts_list", "containers_", "images_", "volumes_list", "networks_list", "compose_up", "registries_list", "kube_"}, "Docker here and on the owner's ssh servers (contexts_list shows them); use these, not the docker command"},
	{"Services", []string{"services_", "service_"}, "the project's dev services: status, logs, start and stop"},
	{"HTTP", []string{"http_"}, "the owner's .http requests and sending them"},
	{"Tests and tasks", []string{"tests_", "tasks_"}, "run tests and project tasks and read their results"},
	{"Debugger", []string{"debug_"}, "breakpoints, start, step, evaluate"},
	{"Code intelligence", []string{"diagnostics", "lsp_"}, "problems, hover, symbols, definitions"},
	{"Editor and IDE", []string{"open_file", "get_editor_state", "get_ide_state", "show_panel", "notify", "list_actions", "execute_action", "settings_"}, "open files for the owner, read and drive the IDE"},
	{"Workflows and agents", []string{"workflow_", "agent_run_"}, "the owner's workflows and background agent runs"},
	{"Web", []string{"web_fetch", "web_get", "web_search"}, "read a web page or search the web"},
}

// aiToolMatch reports whether tool name matches a group name or prefix.
func aiToolMatch(name, pat string) bool {
	if strings.HasSuffix(pat, "_") {
		return strings.HasPrefix(name, pat)
	}
	return name == pat
}

// aiToolGuide is the short list of the tools a step has, by kind ("" for
// none). viaMCP: the step reaches them through termocode's MCP server
// (a CLI step), so it says where they come from.
func aiToolGuide(tools []ai.Tool, viaMCP bool) string {
	var b strings.Builder
	for _, g := range aiToolGroups {
		var names []string
		for _, t := range tools {
			for _, p := range g.names {
				if aiToolMatch(t.Name, p) {
					names = append(names, t.Name)
					break
				}
			}
		}
		if len(names) == 0 {
			continue
		}
		if len(names) > 6 {
			names = append(names[:6], "…")
		}
		b.WriteString("- " + g.title + ": " + strings.Join(names, ", ") + ": " + g.when + ".\n")
	}
	if b.Len() == 0 {
		return ""
	}
	head := "termocode's tools you have (use them before you guess):\n"
	if viaMCP {
		head = "termocode's tools you have come from its MCP server \"termocode\" (use them before you guess; " +
			"prefer them to your own shell for databases, containers and servers):\n"
	}
	return head + b.String() + "Reads run at once. Every change asks the owner on a card first; when a card is declined, do not retry it. Secrets show as [REDACTED].\n"
}

// aiCLIInstructions is what a CLI step (Codex, Claude Code) gets above
// the prompt (clistep.Spec.Instructions): who it is, termocode's tools
// it has from the step's MCP server (served; none: no server) and what
// its own tools may do (write: the step's Tools setting lets them change
// the project folder). Its own docs and skills never answer who it is,
// and its read-only shell never hides termocode's tools.
func aiCLIInstructions(root string, served []ai.Tool, write bool) string {
	var b strings.Builder
	b.WriteString(aiIdentityPrompt(root))
	b.WriteString(aiToolGuide(served, true))
	if len(served) > 0 {
		b.WriteString("Call them as tools (mcp__" + aiStepMCPName + "__<name>); never give the owner a command to run in place of one of them.\n")
	}
	switch {
	case write:
		b.WriteString("Your own tools can change files in the project folder; for databases, containers and servers use termocode's tools.\n")
	case len(served) > 0:
		b.WriteString("Your own shell and file tools are read-only (this step's Tools setting). That is not a limit of termocode's tools: " +
			"do every change they offer through them (each one asks the owner on a card), and never say you cannot when one of them can.\n")
	default:
		b.WriteString("This step only reads (its Tools setting): your own tools are read-only.\n")
	}
	return b.String()
}

// aiWithToolGuide puts the tool guide of tools right after the identity
// paragraph of base (at the end when base has none).
func aiWithToolGuide(base string, tools []ai.Tool, viaMCP bool) string {
	g := aiToolGuide(tools, viaMCP)
	if g == "" {
		return base
	}
	if i := strings.Index(base, aiIdentityEnd); i >= 0 {
		i += len(aiIdentityEnd)
		return base[:i] + g + base[i:]
	}
	return base + "\n" + g
}
