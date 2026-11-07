package containers

import (
	"reflect"
	"strings"
	"testing"
)

func TestRunOptionsCommandFullArgv(t *testing.T) {
	e := &Engine{Binary: "docker", Kind: "docker"}
	o := RunOptions{
		Image: "nginx:1.27", Name: "web", Detach: true, Remove: false,
		Ports: []string{"8080:80"}, Volumes: []string{"/srv:/usr/share/nginx/html:ro"},
		Env: map[string]string{"B": "2", "A": "sk-" + "test"}, Network: "front", Restart: "unless-stopped",
		User: "101", Workdir: "/app", Hostname: "h1", Platform: "linux/amd64",
		Labels: map[string]string{"team": "x"}, Entrypoint: "/docker-entrypoint.sh", Command: []string{"nginx", "-g", "daemon off;"},
	}
	cmd, err := e.RunOptionsCommand(o)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"docker", "run", "-d", "--name", "web", "-p", "8080:80", "-v", "/srv:/usr/share/nginx/html:ro",
		"-e", "A", "-e", "B", "--network", "front", "--restart", "unless-stopped", "--user", "101", "--workdir", "/app",
		"--hostname", "h1", "--platform", "linux/amd64", "--label", "team=x", "--entrypoint", "/docker-entrypoint.sh",
		"nginx:1.27", "nginx", "-g", "daemon off;"}
	if !reflect.DeepEqual(cmd.Argv, want) {
		t.Fatalf("argv = %q\nwant   %q", cmd.Argv, want)
	}
	// Env values travel in the process env, never in argv.
	if strings.Contains(strings.Join(cmd.Argv, " "), "sk-"+"test") || cmd.Env["A"] != "sk-"+"test" || cmd.Env["B"] != "2" {
		t.Fatalf("env = %v argv = %q", cmd.Env, cmd.Argv)
	}
	if cmd.Label != "run: nginx:1.27" {
		t.Fatalf("label = %q", cmd.Label)
	}
}

func TestRunOptionsInteractiveDefault(t *testing.T) {
	e := &Engine{Binary: "podman", Kind: "podman"}
	cmd, err := e.RunOptionsCommand(RunOptions{Image: "alpine", Remove: true})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"podman", "run", "--rm", "-it", "alpine"}; !reflect.DeepEqual(cmd.Argv, want) {
		t.Fatalf("argv = %q", cmd.Argv)
	}
}

func TestRunOptionsRejectsBadValues(t *testing.T) {
	e := &Engine{Binary: "docker", Kind: "docker"}
	for _, o := range []RunOptions{
		{},
		{Image: "-v"},
		{Image: "a", Name: "--privileged"},
		{Image: "a", Ports: []string{"-p"}},
		{Image: "a", Env: map[string]string{"BAD=NAME": "x"}},
		{Image: "a", Env: map[string]string{"": "x"}},
		{Image: "a", Restart: "sometimes"},
		{Image: "a", Workdir: "rel"},
		{Image: "a", Labels: map[string]string{"-k": "v"}},
	} {
		if cmd, err := e.RunOptionsCommand(o); err == nil {
			t.Errorf("RunOptionsCommand(%+v) = %q, want error", o, cmd.Argv)
		}
	}
}

const inspectWeb = `[{"Name":"/web","Config":{"Image":"nginx:1.27","Env":["A=1","PATH=/usr/bin"],"Cmd":["nginx","-g","daemon off;"],
"Entrypoint":["/docker-entrypoint.sh"],"WorkingDir":"/app","User":"101","Hostname":"abc123","Labels":{"team":"x"}},
"HostConfig":{"Binds":["/srv:/data"],"NetworkMode":"front","RestartPolicy":{"Name":"always"},"AutoRemove":false,
"PortBindings":{"80/tcp":[{"HostIp":"","HostPort":"8080"}],"443/tcp":[{"HostIp":"127.0.0.1","HostPort":"8443"}]}}}]`

func TestRunOptionsFromInspectDuplicate(t *testing.T) {
	o, err := RunOptionsFromInspect([]byte(inspectWeb))
	if err != nil {
		t.Fatal(err)
	}
	if o.Image != "nginx:1.27" || o.Name != "web" || !o.Detach || o.Restart != "always" || o.Network != "front" ||
		o.User != "101" || o.Workdir != "/app" || o.Entrypoint != "/docker-entrypoint.sh" {
		t.Fatalf("opts = %+v", o)
	}
	if !reflect.DeepEqual(o.Ports, []string{"127.0.0.1:8443:443/tcp", "8080:80/tcp"}) {
		t.Fatalf("ports = %q", o.Ports)
	}
	if !reflect.DeepEqual(o.Volumes, []string{"/srv:/data"}) || !reflect.DeepEqual(o.Command, []string{"nginx", "-g", "daemon off;"}) {
		t.Fatalf("volumes/command = %q %q", o.Volumes, o.Command)
	}
	if o.Env["A"] != "1" || o.Env["PATH"] != "/usr/bin" || o.Labels["team"] != "x" {
		t.Fatalf("env/labels = %v %v", o.Env, o.Labels)
	}
	// The generated hostname (container id) is not copied: a duplicate gets its own.
	if o.Hostname != "" {
		t.Fatalf("hostname = %q", o.Hostname)
	}
	dup := DuplicateName(o.Name, []string{"web", "web-copy"})
	if dup != "web-copy-2" {
		t.Fatalf("DuplicateName = %q", dup)
	}
}

func TestRunOptionsFromInspectBadJSON(t *testing.T) {
	for _, b := range []string{``, `[]`, `{`, `[{"Config":{}}]`} {
		if _, err := RunOptionsFromInspect([]byte(b)); err == nil {
			t.Errorf("RunOptionsFromInspect(%q) want error", b)
		}
	}
}
