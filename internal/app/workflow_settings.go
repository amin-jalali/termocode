package app

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/workflow"
)

// Workflows settings (WFX-0, plan §5.8): config.json keys with Settings
// rows in a "Workflows" group. settingsConfig does not know them;
// saveSettings keeps unknown keys, and loadWorkflowSettings reads them.

type workflowSettingRow struct {
	id, key, kind, def, desc string
	lo, hi                   float64 // int and number rows: allowed range
}

var workflowSettingRows = []workflowSettingRow{
	{"setting-workflows-enabled", "workflows_enabled", "bool", strconv.FormatBool(workflow.DefaultEnabled),
		"Workflows can run: by hand, from the CLI and on their schedules. false stops every schedule and refuses new runs.", 0, 0},
	{"setting-workflows-parallel-max", "workflows_parallel_max", "int", strconv.Itoa(workflow.DefaultParallelMax),
		"Most workflow runs at the same time, across all workflows (1-16). A workflow's own parallel setting splits its items inside one run.", 1, 16},
	{"setting-workflows-day-budget", "workflows_day_budget_usd", "number", strconv.FormatFloat(workflow.DefaultDayBudgetUSD, 'f', -1, 64),
		"US dollars all workflows may spend on AI in one day; a run that reaches it pauses and asks. 0 means no day budget.", 0, 100000},
	{"setting-workflows-missed-grace", "workflows_missed_grace_min", "int", strconv.Itoa(workflow.DefaultMissedGraceMin),
		"A scheduled run that was missed (termocode was not running) still runs once if it was due at most this many minutes ago; older ones are recorded as missed. 0 never runs a missed one.", 0, 1440},
	{"setting-workflows-history-days", "workflows_history_days", "int", strconv.Itoa(workflow.DefaultHistoryDays),
		"Days a finished workflow run (its file and logs) is kept before it is deleted at start.", 1, 3650},
	{"setting-workflows-wait-hours", "workflows_wait_hours", "int", strconv.Itoa(workflow.DefaultWaitHours),
		"Hours a run waits for an answer on a card (a write to allow, a budget pause) before the step is marked waiting too long.", 1, 168},
	{"setting-workflows-agent-sandbox", "workflows_agent_sandbox", "enum", workflow.DefaultAgentSandbox,
		"Where an outside agent step works: worktree (it may change files in its own git worktree) or read-only (it may only read).", 0, 0},
	{"setting-agent-os-sandbox", "agent_os_sandbox", "bool", "false",
		"Outside agents (workflow agent steps, AI run CLI steps, race lanes) run inside an OS sandbox: bwrap on Linux, sandbox-exec on macOS. The disk is read-only; only the lane's worktree, the temp folder and the agent's own config folders can be written. The network stays on. No sandbox tool: the agent does not start.", 0, 0},
	{"setting-workflows-trigger-poll", "workflows_trigger_poll_min", "int", strconv.Itoa(workflow.DefaultTriggerPollMin),
		"Minutes between two looks at GitHub (through gh) for the github triggers of workflows that are on (1-1440).", 1, 1440},
	{"setting-workflows-webhook-port", "workflows_webhook_port", "int", strconv.Itoa(workflow.DefaultWebhookPort),
		"Port on 127.0.0.1 for the inbound webhooks of workflows (POST /hooks/<name>, signed with the secret workflow-hook.<name>). 0 means no webhooks. Put a tunnel or a reverse proxy in front to reach it from outside.", 0, 65535},
}

func init() {
	for _, r := range workflowSettingRows {
		settingsRows = append(settingsRows, settingsRow{ID: r.id, Label: r.key, Kind: r.kind, Default: r.def, Desc: r.desc})
	}
}

func workflowSettingRowByID(id string) (workflowSettingRow, bool) {
	for _, r := range workflowSettingRows {
		if r.id == id {
			return r, true
		}
	}
	return workflowSettingRow{}, false
}

// workflowSettingRange is the allowed range of a workflows_* int setting.
func workflowSettingRange(key string) (lo, hi int, ok bool) {
	for _, r := range workflowSettingRows {
		if r.key == key && r.kind == "int" {
			return int(r.lo), int(r.hi), true
		}
	}
	return 0, 0, false
}

// workflowSettingsCategories puts the rows in their own Settings group.
func workflowSettingsCategories(cats map[string]string) map[string]string {
	for _, r := range workflowSettingRows {
		cats[r.id] = "Workflows"
	}
	return cats
}

// workflowSettingChoices are the values of an enum row (nil: not ours).
func workflowSettingChoices(id string) []string {
	if id == "setting-workflows-agent-sandbox" {
		return workflow.Sandboxes
	}
	return nil
}

// loadWorkflowSettings reads the workflows_* keys from config.json:
// key → raw JSON value (a missing key is absent).
func loadWorkflowSettings() map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	path, err := settingsConfigPath()
	if err != nil {
		return out
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var all map[string]json.RawMessage
	if json.Unmarshal(b, &all) != nil {
		return out
	}
	for _, r := range workflowSettingRows {
		if v, ok := all[r.key]; ok {
			out[r.key] = v
		}
	}
	return out
}

// workflowSettingValue renders a row's current value; a missing or bad
// value shows the default. ok=false: not ours.
func workflowSettingValue(id string) (string, bool) {
	r, ok := workflowSettingRowByID(id)
	if !ok {
		return "", false
	}
	raw, ok := loadWorkflowSettings()[r.key]
	if !ok {
		return r.def, true
	}
	switch r.kind {
	case "bool":
		var b bool
		if json.Unmarshal(raw, &b) == nil {
			return strconv.FormatBool(b), true
		}
	case "int", "number":
		var f float64
		if json.Unmarshal(raw, &f) == nil && f >= r.lo && f <= r.hi && (r.kind == "number" || f == float64(int(f))) {
			return strconv.FormatFloat(f, 'f', -1, 64), true
		}
	case "enum":
		var s string
		if json.Unmarshal(raw, &s) == nil {
			for _, c := range workflowSettingChoices(r.id) {
				if s == c {
					return s, true
				}
			}
		}
	}
	return r.def, true
}

// applyWorkflowSetting validates and saves one row. ok=false: not ours.
func (m *Model) applyWorkflowSetting(id, value string) (tea.Cmd, bool) {
	r, ok := workflowSettingRowByID(id)
	if !ok {
		return nil, false
	}
	value = strings.TrimSpace(value)
	var v any
	switch r.kind {
	case "bool":
		b, err := parseBoolLoose(value)
		if err != nil {
			return m.pushToastDetail(toast.Errr, r.key+": expected true or false", ""), true
		}
		v, value = b, strconv.FormatBool(b)
	case "enum":
		choices := workflowSettingChoices(r.id)
		found := false
		for _, c := range choices {
			if strings.EqualFold(c, value) {
				v, value, found = c, c, true
			}
		}
		if !found {
			return m.pushToastDetail(toast.Errr, r.key+": expected one of "+strings.Join(choices, ", "), ""), true
		}
	case "number":
		f, err := strconv.ParseFloat(value, 64)
		if err != nil || f < r.lo || f > r.hi {
			return m.pushToastDetail(toast.Errr, r.key+": expected a number ≥ 0 (0 = no budget)", ""), true
		}
		v, value = f, strconv.FormatFloat(f, 'f', -1, 64)
	default:
		n, err := strconv.Atoi(value)
		if err != nil || float64(n) < r.lo || float64(n) > r.hi {
			return m.pushToastDetail(toast.Errr, r.key+": expected an integer "+strconv.Itoa(int(r.lo))+"…"+strconv.Itoa(int(r.hi)), ""), true
		}
		v = n
	}
	if err := saveHTTPSetting(r.key, v); err != nil {
		return m.pushToastDetail(toast.Errr, "Settings error", err.Error()), true
	}
	var sync tea.Cmd
	if r.key == "workflows_enabled" && m.wfx != nil {
		sync = m.wfxSchedSync()
	}
	return tea.Batch(sync, m.pushToastDetail(toast.Info, r.key+" = "+value, "")), true
}
