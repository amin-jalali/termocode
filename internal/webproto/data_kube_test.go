package webproto

import (
	"encoding/json"
	"testing"
)

// Spec (WEBP-4, Kubernetes): every read is open to viewers and audited
// nowhere; restarting a deployment needs a controller and a laptop card.
func TestKubeRows(t *testing.T) {
	for _, op := range []string{OpKubeContexts, OpKubeNamespaces, OpKubeList, OpKubeLogs, OpKubeDescribe} {
		p, ok := Lookup(op)
		if !ok || p.Role != RoleViewer || p.LaptopCard || p.Phase != PhaseV2 {
			t.Errorf("%s row %+v", op, p)
		}
	}
	if p, _ := Lookup(OpKubeRestart); p.Role != RoleController || !p.LaptopCard || p.Audit != AuditYes {
		t.Errorf("restart row %+v", p)
	}
}

// Spec: names are Kubernetes names; a context is printable and never a
// flag; kinds are the four the laptop shows.
func TestKubeArgsRules(t *testing.T) {
	good := map[string][]string{
		OpKubeList:     {`{}`, `{"context":"arn:aws:eks:eu-west-1:1:cluster/prod","namespace":"kube-system"}`, `{"context":"gke_p_z_c"}`},
		OpKubeLogs:     {`{"pod":"api-7d9f-x2"}`, `{"pod":"a","container":"b","lines":1000}`},
		OpKubeDescribe: {`{"kind":"job","name":"migrate.v2"}`},
	}
	bad := map[string][]string{
		OpKubeList:     {`{"context":"--kubeconfig=/x"}`, `{"context":"a b"}`, `{"namespace":"Shop"}`, `{"namespace":"-a"}`, `{"namespace":"a_b"}`},
		OpKubeLogs:     {`{"pod":""}`, `{"pod":"a","lines":1001}`, `{"pod":"a","container":"--previous"}`, `{"pod":"a/b"}`},
		OpKubeDescribe: {`{"kind":"secret","name":"a"}`, `{"kind":"pod","name":"a."}`, `{"kind":"pod"}`},
		OpKubeRestart:  {`{"kind":"deployment","name":"api","replicas":0}`},
	}
	for op, list := range good {
		for _, a := range list {
			if _, err := DecodeArgs(op, json.RawMessage(a)); err != nil {
				t.Errorf("%s %s: %v", op, a, err)
			}
		}
	}
	for op, list := range bad {
		for _, a := range list {
			if _, err := DecodeArgs(op, json.RawMessage(a)); err == nil {
				t.Errorf("%s %s: accepted", op, a)
			}
		}
	}
}
