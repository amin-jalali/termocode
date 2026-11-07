package kube

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseStatefulSets(t *testing.T) {
	ss, err := ParseStatefulSets(fixture(t, "statefulsets.json"))
	if err != nil || len(ss) != 2 {
		t.Fatalf("statefulsets = %d, %v", len(ss), err)
	}
	if got := []string{ss[0].Ready(), ss[1].Ready()}; !reflect.DeepEqual(got, []string{"2/3", "0/1"}) {
		t.Errorf("ready = %v", got)
	}
	if ss[0].Namespace != "shop" || ss[0].Name != "db" {
		t.Errorf("db = %+v", ss[0])
	}
}

func TestParseDaemonSets(t *testing.T) {
	ds, err := ParseDaemonSets(fixture(t, "daemonsets.json"))
	if err != nil || len(ds) != 1 {
		t.Fatalf("daemonsets = %d, %v", len(ds), err)
	}
	d := ds[0]
	if d.Desired != 4 || d.Current != 4 || d.ReadyN != 3 || d.Updated != 4 || d.Available != 3 || d.Ready() != "3/4" {
		t.Errorf("log-agent = %+v", d)
	}
}

func TestParseReplicaSets(t *testing.T) {
	rs, err := ParseReplicaSets(fixture(t, "replicasets.json"))
	if err != nil || len(rs) != 2 {
		t.Fatalf("replicasets = %d, %v", len(rs), err)
	}
	if rs[0].Desired != 3 || rs[0].Current != 3 || rs[0].ReadyN != 3 || rs[0].Owner != "Deployment/api" {
		t.Errorf("rs 0 = %+v", rs[0])
	}
	if rs[1].Desired != 0 || rs[1].Ready() != "0/0" {
		t.Errorf("rs 1 = %+v", rs[1])
	}
}

func TestParseCronJobs(t *testing.T) {
	cs, err := ParseCronJobs(fixture(t, "cronjobs.json"))
	if err != nil || len(cs) != 2 {
		t.Fatalf("cronjobs = %d, %v", len(cs), err)
	}
	if cs[0].Schedule != "0 2 * * *" || cs[0].Suspend || cs[0].Active != 1 {
		t.Errorf("nightly = %+v", cs[0])
	}
	if want := time.Date(2026, 10, 29, 2, 0, 0, 0, time.UTC); !cs[0].LastSchedule.Equal(want) {
		t.Errorf("last schedule = %v", cs[0].LastSchedule)
	}
	if !cs[1].Suspend || cs[1].Active != 0 || !cs[1].LastSchedule.IsZero() {
		t.Errorf("report = %+v", cs[1])
	}
}

func TestParseConfigMapsKeepsKeyNamesOnly(t *testing.T) {
	cm, err := ParseConfigMaps(fixture(t, "configmaps.json"))
	if err != nil || len(cm) != 2 {
		t.Fatalf("configmaps = %d, %v", len(cm), err)
	}
	if want := []string{"FEATURE_X", "LOG_LEVEL", "logo.png"}; !reflect.DeepEqual(cm[0].Keys, want) {
		t.Errorf("keys = %v, want %v (sorted)", cm[0].Keys, want)
	}
	if len(cm[1].Keys) != 0 {
		t.Errorf("empty keys = %v", cm[1].Keys)
	}
}

func TestParseSecretsHasNoValues(t *testing.T) {
	ss, err := ParseSecrets(fixture(t, "secrets.txt"))
	if err != nil || len(ss) != 3 {
		t.Fatalf("secrets = %d, %v", len(ss), err)
	}
	if ss[0].Name != "db-password" || ss[0].Type != "Opaque" || !reflect.DeepEqual(ss[0].Keys, []string{"password", "user"}) {
		t.Errorf("secret 0 = %+v", ss[0])
	}
	if want := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC); !ss[0].Created.Equal(want) {
		t.Errorf("created = %v", ss[0].Created)
	}
	if ss[1].Type != "kubernetes.io/dockerconfigjson" || !reflect.DeepEqual(ss[1].Keys, []string{".dockerconfigjson"}) {
		t.Errorf("secret 1 = %+v", ss[1])
	}
	if len(ss[2].Keys) != 0 {
		t.Errorf("no-keys = %v", ss[2].Keys)
	}
	if _, err := ParseSecrets([]byte("only-a-name\n")); err == nil {
		t.Error("no error for a short line")
	}
}

func TestSecretsTemplateNeverPrintsValues(t *testing.T) {
	for _, bad := range []string{"$v}}", "index", "printf", "base64"} {
		if strings.Contains(secretsTemplate, bad) {
			t.Errorf("secrets template prints values (%q): %s", bad, secretsTemplate)
		}
	}
}

func TestParseIngresses(t *testing.T) {
	is, err := ParseIngresses(fixture(t, "ingresses.json"))
	if err != nil || len(is) != 2 {
		t.Fatalf("ingresses = %d, %v", len(is), err)
	}
	w := is[0]
	if w.Class != "nginx" || !reflect.DeepEqual(w.Hosts, []string{"shop.example.com", "api.example.com"}) ||
		!reflect.DeepEqual(w.Address, []string{"203.0.113.7", "lb.example.com"}) || w.PortsText() != "80, 443" {
		t.Errorf("web = %+v %q", w, w.PortsText())
	}
	if got := is[1].HostsText(); got != "*" {
		t.Errorf("internal hosts = %q", got)
	}
	if is[1].PortsText() != "80" {
		t.Errorf("internal ports = %q", is[1].PortsText())
	}
}

func TestParsePVCs(t *testing.T) {
	ps, err := ParsePVCs(fixture(t, "persistentvolumeclaims.json"))
	if err != nil || len(ps) != 2 {
		t.Fatalf("pvcs = %d, %v", len(ps), err)
	}
	p := ps[0]
	if p.Status != "Bound" || p.Volume != "pvc-1a2b" || p.Capacity != "10Gi" || p.StorageClass != "standard" || p.AccessText() != "RWO" {
		t.Errorf("data-db-0 = %+v %q", p, p.AccessText())
	}
	if ps[1].Status != "Pending" || ps[1].AccessText() != "RWX,ROX" {
		t.Errorf("scratch = %+v %q", ps[1], ps[1].AccessText())
	}
}

func TestParseNodes(t *testing.T) {
	ns, err := ParseNodes(fixture(t, "nodes.json"))
	if err != nil || len(ns) != 3 {
		t.Fatalf("nodes = %d, %v", len(ns), err)
	}
	type row struct{ name, status, roles, version string }
	want := []row{
		{"cp-1", "Ready", "control-plane,master", "v1.31.2"},
		{"worker-1", "Ready,SchedulingDisabled", "<none>", "v1.31.1"},
		{"worker-2", "NotReady", "<none>", "v1.31.1"},
	}
	for i, w := range want {
		n := ns[i]
		if got := (row{n.Name, n.Status, n.RolesText(), n.Version}); got != w {
			t.Errorf("node %d = %+v, want %+v", i, got, w)
		}
	}
}
