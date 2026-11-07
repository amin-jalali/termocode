package dbclient

import (
	"strings"
	"testing"
)

// A connection may reach its server through `kubectl port-forward` to a
// pod: "kube": {"context", "namespace", "pod", "port"}. Each bad block is
// marked on its entry only.
func TestParseConfigKubeBlock(t *testing.T) {
	body := `{"connections":[
	  {"id":"ok","driver":"postgres","kube":{"context":"dev","namespace":"shop","pod":"pg-0"}},
	  {"id":"port","driver":"mysql","kube":{"pod":"db-0","port":3307}},
	  {"id":"nopod","driver":"postgres","kube":{"namespace":"shop"}},
	  {"id":"badpod","driver":"postgres","kube":{"pod":"--all"}},
	  {"id":"badns","driver":"postgres","kube":{"pod":"pg-0","namespace":"-n"}},
	  {"id":"badctx","driver":"postgres","kube":{"pod":"pg-0","context":"--kubeconfig=/x"}},
	  {"id":"badport","driver":"postgres","kube":{"pod":"pg-0","port":70000}},
	  {"id":"lite","driver":"sqlite","path":"a.db","kube":{"pod":"pg-0"}},
	  {"id":"both","driver":"postgres","ssh":{"host":"b"},"kube":{"pod":"pg-0"}}
	]}`
	cs, err := ParseConfig([]byte(body), SourceUser, "u", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"ok":      "",
		"port":    "",
		"nopod":   "kube.pod is required",
		"badpod":  "kube.pod is not a valid pod name",
		"badns":   "kube.namespace is not valid",
		"badctx":  "kube.context is not valid",
		"badport": "kube.port is not a valid port",
		"lite":    "kube port-forward needs a server database",
		"both":    "use ssh or kube, not both",
	}
	for _, c := range cs {
		if c.Problem != want[c.ID] {
			t.Errorf("%s: problem %q, want %q", c.ID, c.Problem, want[c.ID])
		}
	}
	if cs[0].Kube == nil || cs[0].Kube.Port != 5432 || cs[0].Kube.Context != "dev" || cs[0].Kube.Namespace != "shop" {
		t.Errorf("ok: kube %+v, want the pod port to default to the driver's", cs[0].Kube)
	}
	if cs[1].Kube.Port != 3307 {
		t.Errorf("port: kube port %d, want 3307", cs[1].Kube.Port)
	}
}

// The add-connection form accepts a kube connection without a host: the
// host and port come from the forward.
func TestValidateKubeConnection(t *testing.T) {
	ok := Conn{ID: "k", Driver: "postgres", Kube: &KubeConfig{Pod: "pg-0", Port: 5432}}
	if err := Validate(ok); err != nil {
		t.Errorf("kube without host: %v", err)
	}
	bad := Conn{ID: "k", Driver: "postgres", Kube: &KubeConfig{Pod: "Bad_Pod"}}
	if err := Validate(bad); err == nil || !strings.Contains(err.Error(), "pod name") {
		t.Errorf("bad pod: %v", err)
	}
	if err := Validate(Conn{ID: "k", Driver: "postgres"}); err == nil {
		t.Error("no host and no kube was accepted")
	}
}

// Forwarded points the connection at the local end of the forward and
// keeps the original untouched.
func TestConnForwarded(t *testing.T) {
	c := Conn{ID: "k", Driver: "postgres", Host: "", Port: 5432, Kube: &KubeConfig{Pod: "pg-0", Port: 5432}}
	f := c.Forwarded(40123)
	if f.Host != "127.0.0.1" || f.Port != 40123 {
		t.Errorf("forwarded to %s:%d", f.Host, f.Port)
	}
	if c.Host != "" || c.Port != 5432 {
		t.Errorf("original changed: %s:%d", c.Host, c.Port)
	}
	if !strings.Contains(ConfigTemplate(), `"kube"`) {
		t.Error("the config template does not show the kube block")
	}
}
