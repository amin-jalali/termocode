package containers

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
)

// CTX-24: create, inspect and remove docker contexts (podman connections).

func TestCreateContextArgsDocker(t *testing.T) {
	e := Engine{Kind: "docker"}
	cases := []struct {
		spec ContextSpec
		want []string
	}{
		{ContextSpec{Name: "lab", Host: "ssh://me@lab.example"},
			[]string{"context", "create", "lab", "--docker", "host=ssh://me@lab.example"}},
		{ContextSpec{Name: "ci", Description: "build box", Host: "tcp://10.0.0.5:2376", CA: "/k/ca.pem", Cert: "/k/cert.pem", Key: "/k/key.pem"},
			[]string{"context", "create", "ci", "--description", "build box", "--docker", "host=tcp://10.0.0.5:2376,ca=/k/ca.pem,cert=/k/cert.pem,key=/k/key.pem"}},
		{ContextSpec{Name: "test", Host: "tcp://h:2376", SkipTLSVerify: true},
			[]string{"context", "create", "test", "--docker", "host=tcp://h:2376,skip-tls-verify=true"}},
	}
	for _, c := range cases {
		got, err := e.CreateContextArgs(c.spec)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("CreateContextArgs(%+v) = %q, %v; want %q", c.spec, got, err, c.want)
		}
	}
}

func TestCreateContextArgsPodman(t *testing.T) {
	e := Engine{Kind: "podman"}
	got, err := e.CreateContextArgs(ContextSpec{Name: "lab", Host: "ssh://me@lab/run/podman/podman.sock", Key: "/k/id_ed25519"})
	want := []string{"system", "connection", "add", "--identity", "/k/id_ed25519", "lab", "ssh://me@lab/run/podman/podman.sock"}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, %v; want %q", got, err, want)
	}
	if _, err := e.CreateContextArgs(ContextSpec{Name: "x", Host: "tcp://h:2376", CA: "/ca"}); err == nil {
		t.Fatal("podman took TLS files")
	}
}

func TestCreateContextArgsRefusesBadInput(t *testing.T) {
	e := Engine{Kind: "docker"}
	for _, s := range []ContextSpec{
		{Name: "", Host: "ssh://a"},
		{Name: "-rm", Host: "ssh://a"},
		{Name: "has space", Host: "ssh://a"},
		{Name: "default", Host: "ssh://a"},
		{Name: "ok", Host: ""},
		{Name: "ok", Host: "http://a"},
		{Name: "ok", Host: "tcp://a,ca=/evil"},
		{Name: "ok", Host: "tcp://a", CA: "/x,key=/y"},
		{Name: "ok", Host: "ssh://a", CA: "/ca.pem"},
		{Name: "ok", Host: "ssh://a", SkipTLSVerify: true},
	} {
		if args, err := e.CreateContextArgs(s); err == nil {
			t.Errorf("CreateContextArgs(%+v) = %q, want an error", s, args)
		}
	}
}

func TestRemoveAndInspectContextArgs(t *testing.T) {
	d := Engine{Kind: "docker"}
	p := Engine{Kind: "podman"}
	if got, err := d.RemoveContextArgs("lab"); err != nil || !reflect.DeepEqual(got, []string{"context", "rm", "lab"}) {
		t.Errorf("docker rm = %q, %v", got, err)
	}
	if got, err := p.RemoveContextArgs("lab"); err != nil || !reflect.DeepEqual(got, []string{"system", "connection", "remove", "lab"}) {
		t.Errorf("podman rm = %q, %v", got, err)
	}
	for _, n := range []string{"default", "", "-f", "a b"} {
		if _, err := d.RemoveContextArgs(n); err == nil {
			t.Errorf("RemoveContextArgs(%q) took it", n)
		}
	}
	if got, err := d.InspectContextArgs("lab"); err != nil || !reflect.DeepEqual(got, []string{"context", "inspect", "lab"}) {
		t.Errorf("docker inspect = %q, %v", got, err)
	}
	if _, err := d.InspectContextArgs("-x"); err == nil {
		t.Error("inspect took a flag as a name")
	}
}

func TestContextManageCallsRunWithoutTheSessionContext(t *testing.T) {
	f := servicestest.New(t)
	f.Set(t, "context-ls.json", `[{"Name":"lab","Endpoints":{"docker":{"Host":"ssh://me@lab"}}}]`)
	e := Engine{Binary: f.Docker(), Kind: "docker", X: services.SpawnExec{}, Context: "prod"}
	ctx := context.Background()
	if err := e.CreateContext(ctx, ContextSpec{Name: "lab", Host: "ssh://me@lab"}); err != nil {
		t.Fatal(err)
	}
	out, err := e.InspectContext(ctx, "lab")
	if err != nil || !strings.Contains(out, "ssh://me@lab") {
		t.Fatalf("inspect = %q, %v", out, err)
	}
	if err := e.RemoveContext(ctx, "lab"); err != nil {
		t.Fatal(err)
	}
	want := []string{"|context create lab --docker host=ssh://me@lab", "|context inspect lab", "|context rm lab"}
	if c := f.CtxCalls(); !reflect.DeepEqual(c, want) {
		t.Fatalf("calls = %q, want %q", c, want)
	}
}

func TestContextManageReportsEngineErrors(t *testing.T) {
	f := servicestest.New(t)
	f.Set(t, "all.err", "context \"lab\" is in use")
	e := Engine{Binary: f.Docker(), Kind: "docker", X: services.SpawnExec{}}
	if err := e.RemoveContext(context.Background(), "lab"); err == nil {
		t.Fatal("a failing rm returned nil")
	}
}
