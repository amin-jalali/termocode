package kube_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/kube"
)

func TestCopyFromAndToPod(t *testing.T) {
	f, c := fakeClient(t)
	if err := c.CopyFromPod(context.Background(), "api-1", "api", "/var/log/app.log", "/tmp/x/app.log"); err != nil {
		t.Fatal(err)
	}
	if err := c.CopyToPod(context.Background(), "api-1", "", "/tmp/x/conf.yaml", "/etc/app/conf.yaml"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"cp --context dev --namespace shop api-1:/var/log/app.log /tmp/x/app.log --container api",
		"cp --context dev --namespace shop /tmp/x/conf.yaml api-1:/etc/app/conf.yaml",
	}
	if got := f.Calls(); !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %q", got)
	}
	f.Set(t, "cp.err", "Error from server (Forbidden): pods \"api-1\" is forbidden: User \"dev\" cannot create resource \"pods/exec\"\n")
	if err := c.CopyFromPod(context.Background(), "api-1", "", "/a", "/tmp/a"); kube.KindOf(err) != kube.ErrForbidden {
		t.Errorf("err = %v", err)
	}
}

func TestCopyRefusesUnsafePaths(t *testing.T) {
	f, c := fakeClient(t)
	bad := [][4]string{
		{"api-1", "", "-rf", "/tmp/a"},          // looks like a flag
		{"api-1", "", "/a", "-o"},               // local looks like a flag
		{"api-1", "", "/a", "/tmp/c:d"},         // local with ':' is read as a pod
		{"api-1", "", "relative", "/tmp/a"},     // pod path must be absolute
		{"", "", "/a", "/tmp/a"},                // no pod
		{"api:1", "", "/a", "/tmp/a"},           // pod name with ':'
		{"api-1", "", "/a\nb", "/tmp/a"},        // control char
		{"api-1", "-x", "/a", "/tmp/a"},         // container looks like a flag
		{"api-1", "", "/a", "relative/dst.txt"}, // local must be absolute
	}
	for _, b := range bad {
		if err := c.CopyFromPod(context.Background(), b[0], b[1], b[2], b[3]); err == nil {
			t.Errorf("CopyFromPod accepted %q", b)
		}
		if err := c.CopyToPod(context.Background(), b[0], b[1], b[3], b[2]); err == nil {
			t.Errorf("CopyToPod accepted %q", b)
		}
	}
	if n := len(f.Calls()); n != 0 {
		t.Errorf("ran kubectl %d times for bad paths", n)
	}
}

func TestListPodDir(t *testing.T) {
	f, c := fakeClient(t)
	f.Set(t, "exec.txt", "total 12\n"+
		"drwxr-xr-x    1 root     root          4096 Nov  1 10:00 .\n"+
		"drwxr-xr-x    1 root     root          4096 Nov  1 10:00 ..\n"+
		"drwxr-xr-x    2 root     root          4096 Nov  1 10:00 conf d\n"+
		"-rw-r--r--    1 app      app            123 Oct 30  2025 app.log\n"+
		"lrwxrwxrwx    1 root     root             9 Nov  1 10:00 cur -> app.log\n"+
		"garbage line\n")
	files, err := c.ListPodDir(context.Background(), "api-1", "api", "/var/log")
	if err != nil {
		t.Fatal(err)
	}
	want := []kube.PodFile{
		{Name: "conf d", Dir: true, Size: 4096, Mode: "drwxr-xr-x"},
		{Name: "app.log", Size: 123, Mode: "-rw-r--r--"},
		{Name: "cur", Link: "app.log", Size: 9, Mode: "lrwxrwxrwx"},
	}
	if !reflect.DeepEqual(files, want) {
		t.Errorf("files = %+v", files)
	}
	if got := f.Calls(); len(got) != 1 || got[0] != "exec --context dev --namespace shop api-1 --container api -- ls -la -- /var/log" {
		t.Errorf("calls = %q", got)
	}
	if _, err := c.ListPodDir(context.Background(), "api-1", "", "-R"); err == nil {
		t.Error("accepted a flag as the folder")
	}
	f.Set(t, "exec.err", "ls: /nope: No such file or directory\n")
	if _, err := c.ListPodDir(context.Background(), "api-1", "", "/nope"); err == nil || !strings.Contains(err.Error(), "No such file") {
		t.Errorf("err = %v", err)
	}
}

func TestDebugCommand(t *testing.T) {
	c := kube.Client{Binary: "/bin/kubectl", Context: "dev", Namespace: "shop"}
	cmd, err := c.DebugCommand("api-1", "api", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/bin/kubectl", "debug", "-it", "--context", "dev", "--namespace", "shop", "api-1", "--image=" + kube.DefaultDebugImage, "--target=api", "--", "sh"}
	if !reflect.DeepEqual(cmd.Argv, want) {
		t.Errorf("argv = %q", cmd.Argv)
	}
	if cmd.Label != "debug: api-1/api" {
		t.Errorf("label = %q", cmd.Label)
	}
	own, _ := c.DebugCommand("api-1", "", "nicolaka/netshoot:v0.13")
	if j := strings.Join(own.Argv, " "); strings.Contains(j, "--target") || !strings.Contains(j, "--image=nicolaka/netshoot:v0.13") {
		t.Errorf("argv = %q", j)
	}
	for _, img := range []string{"-x", "a b", "img\n"} {
		if _, err := c.DebugCommand("api-1", "", img); err == nil {
			t.Errorf("accepted image %q", img)
		}
	}
}
