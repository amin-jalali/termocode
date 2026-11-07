package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Kubernetes view: any kind by name (CTX-20) ───────────────────────────
//
// : in the view (or Kubernetes: Show Kind…) reads `kubectl api-resources`
// (CRDs included) and asks for a kind: its plural, singular, short name,
// Kind, name.group, or an alias from kube_aliases.json next to
// config.json ({"tls": "certificates.cert-manager.io"}). Tab completes.
// " -A" at the end lists every namespace. The kind then shows as one
// more section after JOBS, loaded with the others; i, v, E and y work on
// its rows (in the object's own namespace). A toggles all namespaces; an
// empty answer hides the section. Nothing here changes the cluster.

const promptKindKubeKind promptKindEnum = 3481

// kubeKindSection is the section of the picked kind.
const kubeKindSection = "kind"

// kubeAliasFile is the aliases file, next to config.json.
const kubeAliasFile = "kube_aliases.json"

// loadKubeAliases reads kube_aliases.json (alias → kind); missing or
// unreadable is no aliases.
func loadKubeAliases() map[string]string {
	path, err := settingsConfigPath()
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(filepath.Dir(path), kubeAliasFile))
	if err != nil {
		return nil
	}
	var raw map[string]string
	if json.Unmarshal(b, &raw) != nil {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		if k = strings.ToLower(strings.TrimSpace(k)); k != "" && strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	return out
}

type kubeKindsMsg struct {
	context string
	res     []kube.APIResource
	err     error
}

// kubeAskKind reads the kinds the cluster serves, then asks.
func (m *Model) kubeAskKind() tea.Cmd {
	st := m.kube
	if st == nil || st.binary == "" {
		return m.servicesToast(toast.Info, "kubectl is not installed", "")
	}
	c := st.client()
	return func() tea.Msg {
		rs, err := c.APIResources(context.Background())
		return kubeKindsMsg{context: c.Context, res: rs, err: err}
	}
}

func (m *Model) onKubeKinds(msg kubeKindsMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot list the kinds", msg.err.Error())
	}
	st := m.kube
	st.apiRes, st.apiResCtx = msg.res, msg.context
	m.openKubeKindPrompt("", "")
	return nil
}

// openKubeKindPrompt asks for the kind (value: the text to show).
func (m *Model) openKubeKindPrompt(value, errText string) {
	st := m.kube
	aliases := loadKubeAliases()
	if value == "" && errText == "" && st.kind != nil {
		value = st.kind.Name
		if st.kindAll {
			value += " -A"
		}
	}
	m.prompt = prompt.New(" Show kind ", "Kind (name, short name, Kind or name.group):", value)
	custom := 0
	for _, r := range st.apiRes {
		if r.Custom() {
			custom++
		}
	}
	note := itoa(len(st.apiRes)) + " kinds (" + itoa(custom) + " custom). Tab completes · \" -A\" every namespace · empty hides."
	if len(aliases) > 0 {
		note += " Aliases: " + strings.Join(kubeSortedKeys(aliases), ", ") + "."
	}
	m.prompt.SetNote(note)
	m.prompt.SetAllowEmpty(true)
	m.prompt.SetCompleter(kubeKindCompleter(st.apiRes, aliases))
	if errText != "" {
		m.prompt.SetError(errText)
	}
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindKubeKind
}

func kubeSortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// kubeKindCompleter completes to the one name that starts with the
// text, or to the longest start all such names share.
func kubeKindCompleter(rs []kube.APIResource, aliases map[string]string) func(string) string {
	return func(s string) string {
		q := strings.ToLower(s)
		if q == "" {
			return s
		}
		seen := map[string]bool{}
		var hits []string
		add := func(n string) {
			if n != "" && strings.HasPrefix(n, q) && !seen[n] {
				seen[n] = true
				hits = append(hits, n)
			}
		}
		for _, r := range rs {
			add(r.Name)
			for _, sn := range r.ShortNames {
				add(sn)
			}
		}
		for a := range aliases {
			add(a)
		}
		if len(hits) == 0 {
			return s
		}
		common := hits[0]
		for _, h := range hits[1:] {
			for !strings.HasPrefix(h, common) {
				common = common[:len(common)-1]
			}
		}
		if len(common) < len(s) {
			return s
		}
		return common
	}
}

// handleKubeKindSubmit takes the kind answer; ok=false: not ours.
func (m *Model) handleKubeKindSubmit(value string) (tea.Cmd, bool) {
	if m.promptKind != promptKindKubeKind || m.kube == nil {
		return nil, false
	}
	st := m.kube
	text := strings.TrimSpace(value)
	all := false
	for _, suf := range []string{" -A", " --all-namespaces"} {
		if strings.HasSuffix(text, suf) {
			text, all = strings.TrimSpace(strings.TrimSuffix(text, suf)), true
		}
	}
	if text == "" {
		if st.kind == nil {
			return nil, true
		}
		st.kind, st.kindObjs, st.kindAll = nil, nil, false
		delete(st.errs, kubeKindSection)
		st.clampCursor()
		st.touch()
		return nil, true
	}
	r, ok := kube.ResolveKind(st.apiRes, loadKubeAliases(), text)
	if !ok {
		m.openKubeKindPrompt(strings.TrimSpace(value), "No kind "+text+" in this cluster. Tab completes the names.")
		return nil, true
	}
	st.kind, st.kindObjs, st.kindAll = &r, nil, all && r.Namespaced
	delete(st.errs, kubeKindSection)
	st.collapsed[kubeKindSection] = false
	st.touch()
	return m.kubeReload(), true
}

// kubeToggleKindAll is A: the kind section in every namespace or in the
// view's one.
func (m *Model) kubeToggleKindAll() tea.Cmd {
	st := m.kube
	if st.kind == nil {
		return nil
	}
	if !st.kind.Namespaced {
		return m.servicesToast(toast.Info, st.kind.Name+" has no namespace", "It is the same in every namespace")
	}
	st.kindAll = !st.kindAll
	st.kindObjs = nil
	st.touch()
	return m.kubeReload()
}

// kindTitle is the kind section's header: "CERTIFICATES · all namespaces".
func (st *kubeState) kindTitle() string {
	if st.kind == nil {
		return "KIND"
	}
	t := strings.ToUpper(st.kind.Name)
	if st.kind.Group != "" {
		t += "." + st.kind.Group
	}
	if st.kindAll {
		t += " · all namespaces"
	}
	return t
}

// selectedKindObject is the kind row under the cursor.
func (st *kubeState) selectedKindObject() (kube.Object, bool) {
	r, ok := st.currentRow()
	if !ok || r.kind != kubeRowItem || r.section != kubeKindSection || st.kind == nil || r.idx >= len(st.kindObjs) {
		return kube.Object{}, false
	}
	return st.kindObjs[r.idx], true
}

// kindClient is kubectl in the object's own namespace.
func (st *kubeState) kindClient(o kube.Object) kube.Client {
	c := st.client()
	if o.Namespace != "" {
		c.Namespace = o.Namespace
	}
	return c
}

// handleKubeKindKey is a key on a kind row; ok=false: not handled here.
func (m *Model) handleKubeKindKey(key string) (tea.Cmd, bool) {
	st := m.kube
	o, ok := st.selectedKindObject()
	if !ok {
		return nil, false
	}
	ref, kindName, plural := st.kind.Ref(), st.kind.Kind, st.kind.Name
	c := st.kindClient(o)
	switch key {
	case "enter", "i":
		return func() tea.Msg {
			text, err := c.Describe(context.Background(), ref, o.Name)
			return kubeDescribeMsg{kind: ref, name: o.Name, text: text, err: err}
		}, true
	case "v":
		return func() tea.Msg {
			text, err := c.YAML(context.Background(), ref, o.Name)
			return kubeYAMLMsg{kind: ref, name: o.Name, text: text, err: err}
		}, true
	case "E":
		title := "events: " + ref + "/" + o.Name
		return func() tea.Msg {
			evs, err := c.Events(context.Background(), kindName, o.Name)
			return kubeEventsMsg{title: title, events: evs, err: err}
		}, true
	case "d", "r", "s", "f", "e", "l", "o", "I":
		return m.servicesToast(toast.Info, "Read only", "The "+plural+" rows can be described (i), shown as YAML (v) and copied (y)"), true
	}
	return nil, false
}
