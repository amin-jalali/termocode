package app

import (
	"sort"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/picker"
)

// "Always allow" for outside agents (M1 MS-3). One yes on the card can be
// remembered for ONE client name and ONE tool (an execute_action id counts
// as its own tool): the next such call from that client runs without the
// card. Other clients, other tools, and the in-app chat are not affected:
// this list is not ai_auto_approve and the chat's auto-approve does not
// apply here (mcpServeGate). It is kept in config.json
// (mcp_server_auto_approve: {"claude-code": ["tasks_run"]}), the user's own
// file, which the AI cannot change (aiSettingPolicies). The name is the
// client's own claim (a same-user process can send any name), so a
// remembered allow is a convenience for one user's processes, not an
// identity check; the list is shown and revoked in AI: Agent Auto-Approve….

// mcpServeAllowSet is the remembered allows as the bridge reads them.
type mcpServeAllowSet struct {
	mu sync.RWMutex
	m  map[string]map[string]bool // client → keys
}

func (a *mcpServeAllowSet) has(client, key string) bool {
	if a == nil || key == "" {
		return false
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.m[client][key]
}

// replace swaps in a whole list (the config was read or edited).
func (a *mcpServeAllowSet) replace(list map[string][]string) {
	m := make(map[string]map[string]bool, len(list))
	for client, keys := range list {
		for _, k := range keys {
			if m[client] == nil {
				m[client] = map[string]bool{}
			}
			m[client][k] = true
		}
	}
	a.mu.Lock()
	a.m = m
	a.mu.Unlock()
}

// mcpServeAlwaysKey is what "Always allow" remembers for a call, "" when
// the card must always ask: a tool without a class, and debug_evaluate
// (it runs any code in the debuggee). execute_action is per action id.
func mcpServeAlwaysKey(t ai.Tool, args map[string]any) string {
	if t.Class == "" || t.Name == "debug_evaluate" {
		return ""
	}
	if t.Name == "execute_action" {
		id, _ := args["id"].(string)
		if id == "" {
			return ""
		}
		return t.Name + ":" + id
	}
	return t.Name
}

// mcpServeAlwaysWhat is a key in words for the card.
func mcpServeAlwaysWhat(key string) string {
	if id, ok := strings.CutPrefix(key, "execute_action:"); ok {
		return "the action " + id
	}
	return key
}

// mcpServeAllowLoad reads the remembered allows from config.json.
func mcpServeAllowLoad() map[string][]string {
	out := map[string][]string{}
	for client, keys := range loadSettings().MCPServerAutoApprove {
		for _, k := range keys {
			if k != "" && !contains(out[client], k) {
				out[client] = append(out[client], k)
			}
		}
	}
	return out
}

// mcpServeAllowSave writes the list ({} removes the key).
func mcpServeAllowSave(list map[string][]string) error {
	path, err := settingsConfigPath()
	if err != nil {
		return err
	}
	clean := map[string][]string{}
	for client, keys := range list {
		if len(keys) > 0 {
			sorted := append([]string(nil), keys...)
			sort.Strings(sorted)
			clean[client] = sorted
		}
	}
	if len(clean) == 0 {
		return mcp.SaveSetting(path, "mcp_server_auto_approve", nil)
	}
	return mcp.SaveSetting(path, "mcp_server_auto_approve", clean)
}

// mcpServeAllowAdd remembers key for client (file and running bridge).
func (m *Model) mcpServeAllowAdd(client, key string) error {
	list := mcpServeAllowLoad()
	if !contains(list[client], key) {
		list[client] = append(list[client], key)
	}
	err := mcpServeAllowSave(list)
	m.mcpServeAllowSync()
	return err
}

// mcpServeAllowRevoke forgets key for client.
func (m *Model) mcpServeAllowRevoke(client, key string) error {
	list := mcpServeAllowLoad()
	var next []string
	for _, k := range list[client] {
		if k != key {
			next = append(next, k)
		}
	}
	list[client] = next
	err := mcpServeAllowSave(list)
	m.mcpServeAllowSync()
	return err
}

// mcpServeAllowSync hands the list to the running bridge. Even when the
// save failed the bridge follows the file: a failed save must not leave
// an allow that the user cannot see.
func (m *Model) mcpServeAllowSync() {
	if m.mcpServe != nil {
		m.mcpServe.hub.allow.replace(mcpServeAllowLoad())
	}
}

const mcpAgentRowPrefix = "mcpagent:"

// mcpServeAllowItems are the "External agents (MCP server)" rows of AI:
// Agent Auto-Approve….
func mcpServeAllowItems() []picker.Item {
	items := []picker.Item{{Header: true, Title: "External agents (MCP server): Always allow, per agent and tool", Group: "mcpagents"}}
	list := mcpServeAllowLoad()
	clients := make([]string, 0, len(list))
	for c := range list {
		clients = append(clients, c)
	}
	sort.Strings(clients)
	n := 0
	for _, c := range clients {
		keys := append([]string(nil), list[c]...)
		sort.Strings(keys)
		for _, k := range keys {
			n++
			items = append(items, picker.Item{ID: mcpAgentRowPrefix + c + ":" + k, Group: "mcpagents",
				Title: c + " — " + k + " runs without asking", Hint: "AUTO · Enter revokes"})
		}
	}
	if n == 0 {
		items = append(items, picker.Item{Header: true, Title: "  none yet: \"Always allow\" on an outside agent's card adds one", Group: "mcpagents"})
	}
	return items
}

// onMCPServeAllowRow revokes one remembered allow (a picker row) and
// shows the list again. ok=false: not one of our rows.
func (m *Model) onMCPServeAllowRow(id string) (tea.Cmd, bool) {
	rest, ok := strings.CutPrefix(id, mcpAgentRowPrefix)
	if !ok {
		return nil, false
	}
	client, key, _ := strings.Cut(rest, ":")
	if err := m.mcpServeAllowRevoke(client, key); err != nil {
		return m.aiToastErr("Settings error", err), true
	}
	info := m.aiInfo(client + ": " + key + " asks first again")
	return tea.Batch(info, m.openAIAutoApprovePicker()), true
}
