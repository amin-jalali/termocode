package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Kubernetes view: more read-only kinds (CTX-1) ────────────────────────
//
// After the four main sections come STATEFULSETS, DAEMONSETS,
// REPLICASETS, CRONJOBS, CONFIGMAPS, SECRETS, INGRESSES, PVCS and NODES.
// They start folded and load only while open, so the 5-second poll stays
// four kubectl calls until the user opens one. Secrets show names, types
// and key names: the values never leave kubectl. Nodes are cluster
// scoped (no --namespace).

// kubeExtraSections are the folded sections, in order.
var kubeExtraSections = []string{"events", "statefulsets", "daemonsets", "replicasets", "cronjobs", "configmaps", "secrets", "ingresses", "pvcs", "nodes"}

func kubeIsExtra(sec string) bool {
	for _, s := range kubeExtraSections {
		if s == sec {
			return true
		}
	}
	return false
}

// kubeMark is a row's status mark.
type kubeMark int

const (
	kubeMarkIdle kubeMark = iota // grey: nothing runs / no state
	kubeMarkOK                   // green
	kubeMarkBusy                 // yellow: on its way
	kubeMarkBad                  // red
)

func (k kubeMark) glyph() (theme.Icon, theme.Color256) {
	switch k {
	case kubeMarkOK:
		return theme.GlyphSvcUp, theme.GitAdded
	case kubeMarkBusy:
		return theme.GlyphSvcStarting, theme.GitModified
	case kubeMarkBad:
		return theme.GlyphSvcExited, theme.DiagError
	}
	return theme.GlyphSvcStopped, theme.TextDim
}

// kubeObj is one row of a folded section, ready to draw.
type kubeObj struct {
	Name     string
	Created  time.Time
	Mark     kubeMark
	Right    string // the right column (before the age)
	Wide     string // added to Right when the view is wide
	Detail   string // the footer line
	Replicas int    // statefulsets: the wanted replicas (s scales)
}

// kubeReadyMark is the mark of "ready of wanted".
func kubeReadyMark(ready, want int) kubeMark {
	switch {
	case want == 0:
		return kubeMarkIdle
	case ready >= want:
		return kubeMarkOK
	case ready == 0:
		return kubeMarkBad
	}
	return kubeMarkBusy
}

func joinOr(xs []string, none string) string {
	if len(xs) == 0 {
		return none
	}
	return strings.Join(xs, ", ")
}

// kubeLoadExtra lists one folded section as rows.
func kubeLoadExtra(ctx context.Context, c kube.Client, sec string, now time.Time) ([]kubeObj, error) {
	var out []kubeObj
	switch sec {
	case "events":
		// The namespace's newest events, live while the section is open
		// (CTX-3). A row is named after its object: keys act on that.
		xs, err := c.Events(ctx, "", "")
		for _, x := range xs[:min(len(xs), kubeEventsMax)] {
			o := kubeObj{Name: x.Object, Created: x.Last, Right: x.Reason}
			if x.Type == "Warning" {
				o.Mark = kubeMarkBad
			}
			msg := strings.Join(strings.Fields(x.Message), " ")
			if x.Count > 1 {
				msg = "x" + strconv.Itoa(x.Count) + " " + msg
			}
			o.Detail = x.Type + " · " + msg
			out = append(out, o)
		}
		return out, err
	case "statefulsets":
		xs, err := c.StatefulSets(ctx)
		for _, x := range xs {
			out = append(out, kubeObj{Name: x.Name, Created: x.Created, Mark: kubeReadyMark(x.ReadyN, x.Replicas), Right: x.Ready(),
				Detail: "ready " + x.Ready() + " · " + strconv.Itoa(x.Replicas) + " replicas", Replicas: x.Replicas})
		}
		return out, err
	case "daemonsets":
		xs, err := c.DaemonSets(ctx)
		for _, x := range xs {
			out = append(out, kubeObj{Name: x.Name, Created: x.Created, Mark: kubeReadyMark(x.ReadyN, x.Desired), Right: x.Ready(),
				Wide:   fmt.Sprintf(" upd %d av %d", x.Updated, x.Available),
				Detail: fmt.Sprintf("ready %s · current %d · updated %d · available %d", x.Ready(), x.Current, x.Updated, x.Available)})
		}
		return out, err
	case "replicasets":
		xs, err := c.ReplicaSets(ctx)
		for _, x := range xs {
			out = append(out, kubeObj{Name: x.Name, Created: x.Created, Mark: kubeReadyMark(x.ReadyN, x.Desired), Right: x.Ready(),
				Detail: "ready " + x.Ready() + " · owner " + orStr(x.Owner, "none")})
		}
		return out, err
	case "cronjobs":
		xs, err := c.CronJobs(ctx)
		for _, x := range xs {
			o := kubeObj{Name: x.Name, Created: x.Created, Right: x.Schedule}
			last := "never ran"
			if !x.LastSchedule.IsZero() {
				last = "last run " + kube.Age(x.LastSchedule, now) + " ago"
				o.Wide = " · " + kube.Age(x.LastSchedule, now)
			}
			o.Detail = x.Schedule + " · " + strconv.Itoa(x.Active) + " active · " + last
			switch {
			case x.Suspend:
				o.Right += " (off)"
				o.Detail += " · suspended"
			case x.Active > 0:
				o.Mark = kubeMarkBusy
			default:
				o.Mark = kubeMarkOK
			}
			out = append(out, o)
		}
		return out, err
	case "configmaps":
		xs, err := c.ConfigMaps(ctx)
		for _, x := range xs {
			out = append(out, kubeObj{Name: x.Name, Created: x.Created, Right: kubeKeyCount(len(x.Keys)),
				Detail: "keys: " + joinOr(x.Keys, "none")})
		}
		return out, err
	case "secrets":
		xs, err := c.Secrets(ctx)
		for _, x := range xs {
			out = append(out, kubeObj{Name: x.Name, Created: x.Created, Right: kubeKeyCount(len(x.Keys)), Wide: " " + x.Type,
				Detail: x.Type + " · keys: " + joinOr(x.Keys, "none")})
		}
		return out, err
	case "ingresses":
		xs, err := c.Ingresses(ctx)
		for _, x := range xs {
			mark := kubeMarkBusy // no address yet
			if len(x.Address) > 0 {
				mark = kubeMarkOK
			}
			out = append(out, kubeObj{Name: x.Name, Created: x.Created, Mark: mark, Right: x.HostsText(), Wide: " " + x.PortsText(),
				Detail: "class " + orStr(x.Class, "none") + " · " + x.HostsText() + " · address " + joinOr(x.Address, "none") + " · ports " + x.PortsText()})
		}
		return out, err
	case "pvcs":
		xs, err := c.PVCs(ctx)
		for _, x := range xs {
			mark := kubeMarkBad
			switch x.Status {
			case "Bound":
				mark = kubeMarkOK
			case "Pending":
				mark = kubeMarkBusy
			}
			out = append(out, kubeObj{Name: x.Name, Created: x.Created, Mark: mark, Right: strings.TrimSpace(x.Status + " " + x.Capacity), Wide: " " + x.AccessText(),
				Detail: x.Status + " · " + orStr(x.Volume, "no volume") + " · " + x.AccessText() + " · class " + orStr(x.StorageClass, "none")})
		}
		return out, err
	case "nodes":
		xs, err := c.Nodes(ctx)
		for _, x := range xs {
			mark := kubeMarkBad
			switch {
			case strings.HasPrefix(x.Status, "Ready") && strings.Contains(x.Status, "SchedulingDisabled"):
				mark = kubeMarkBusy
			case strings.HasPrefix(x.Status, "Ready"):
				mark = kubeMarkOK
			}
			out = append(out, kubeObj{Name: x.Name, Created: x.Created, Mark: mark, Right: x.Status, Wide: " " + x.Version,
				Detail: x.Status + " · roles " + x.RolesText() + " · " + x.Version})
		}
		return out, err
	}
	return nil, nil
}

func orStr(s, none string) string {
	if s == "" {
		return none
	}
	return s
}

// kubeEventsMax bounds the EVENTS rows (E on a header shows them all).
const kubeEventsMax = 50

// rowTarget is the kind and name the keys act on: the row's object, or
// an event row's involved object.
func (st *kubeState) rowTarget(r kubeRow) (string, string) {
	if r.section == "events" {
		kind, name, _ := strings.Cut(r.name, "/")
		return kind, name
	}
	return kubeKindOf(r.section), r.name
}

func kubeKeyCount(n int) string {
	if n == 1 {
		return "1 key"
	}
	return strconv.Itoa(n) + " keys"
}

// openExtras are the folded sections that are open now (they load).
func (st *kubeState) openExtras() []string {
	var out []string
	for _, sec := range kubeExtraSections {
		if !st.collapsed[sec] {
			out = append(out, sec)
		}
	}
	return out
}

// kubeFold opens or folds a section; opening a folded section that has
// not loaded yet loads now.
func (m *Model) kubeFold(sec string) tea.Cmd {
	st := m.kube
	st.toggleFold(sec)
	if kubeIsExtra(sec) && !st.collapsed[sec] && !st.extraLoaded[sec] && st.binary != "" {
		return m.kubeReload()
	}
	return nil
}

// extraObj is the folded-section object of a row.
func (st *kubeState) extraObj(r kubeRow) (kubeObj, bool) {
	xs := st.extra[r.section]
	if r.kind != kubeRowItem || r.idx < 0 || r.idx >= len(xs) {
		return kubeObj{}, false
	}
	return xs[r.idx], true
}
