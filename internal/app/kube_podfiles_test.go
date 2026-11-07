package app

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestKubePodFilesListsTheRootAndOpensFolders(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	f.Set(t, "exec.txt", "total 8\ndrwxr-xr-x 2 root root 4096 Jan 1 10:00 .\ndrwxr-xr-x 2 root root 4096 Jan 1 10:00 ..\ndrwxr-xr-x 2 root root 4096 Jan 1 10:00 etc\n-rw-r--r-- 1 root root 12 Jan 1 10:00 app.log\n")
	kubeCursorTo(t, m, "pods", "web-0")
	cmd := kubeKey(t, m, "F")
	if cmd == nil {
		t.Fatal("F listed nothing")
	}
	msg := cmd().(kubePodDirMsg)
	if msg.err != nil || msg.dir != "/" {
		t.Fatalf("list = %+v", msg)
	}
	m.onKubePodDir(msg)
	if !m.pickerOpen {
		t.Fatal("no file picker")
	}
	if f.Count("exec --context dev --namespace shop web-0") != 1 || f.Count("ls -la -- /") != 1 {
		t.Errorf("calls = %v", f.Calls())
	}
	items := kubePodDirItems(msg)
	if len(items) != 2 || !strings.HasSuffix(items[0].ID, ":/etc") || !strings.HasSuffix(items[1].ID, ":/app.log") {
		t.Errorf("items = %+v", items)
	}
	// A folder lists that folder.
	next := m.handleKubePickerSelect(items[0].ID)
	if next == nil || next().(kubePodDirMsg).dir != "/etc" {
		t.Error("the folder did not open")
	}
	// A file asks where to save it.
	m.handleKubePickerSelect(items[1].ID)
	if !m.promptOpen || m.promptKind != promptKindKubeDownload {
		t.Fatal("no save prompt for a file")
	}
}

func TestKubePodDownloadCopiesToALocalPathAndNeverOverwrites(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	m.kube.pendingFile = kubeFileTarget{context: "dev", namespace: "shop", pod: "web-0", podPath: "/app.log"}
	m.promptKind = promptKindKubeDownload
	dir := t.TempDir()
	dst := filepath.Join(dir, "app.log")
	cmd, ok := m.handleKubePromptSubmit(dst)
	if !ok || cmd == nil {
		t.Fatal("no download")
	}
	if msg := cmd().(kubeCopyMsg); msg.err != nil {
		t.Fatalf("download = %v", msg.err)
	}
	if f.Count("cp --context dev --namespace shop web-0:/app.log "+dst) != 1 {
		t.Errorf("calls = %v", f.Calls())
	}
	// A path that is there already is refused.
	m.kube.pendingFile = kubeFileTarget{context: "dev", namespace: "shop", pod: "web-0", podPath: "/app.log"}
	m.promptKind = promptKindKubeDownload
	if _, ok := m.handleKubePromptSubmit(dir); !ok || !m.promptOpen {
		t.Error("an existing path was not refused")
	}
}

func TestKubePodUploadAsksFirstWithCancelFocused(t *testing.T) {
	f, m := kubeLoaded(t, 150, 42)
	kubeCursorTo(t, m, "pods", "web-0")
	kubeKey(t, m, "U")
	if !m.promptOpen || m.promptKind != promptKindKubeUpload {
		t.Fatal("no upload prompt")
	}
	m.promptOpen = false
	m.promptKind = promptKindKubeUploadDest
	m.kube.pendingFile.local = "/tmp/conf.yaml"
	if _, ok := m.handleKubePromptSubmit("/etc/app/conf.yaml"); !ok {
		t.Fatal("dest not taken")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindKubeUpload {
		t.Fatal("no upload card")
	}
	b := m.confirm.Buttons()
	if len(b) != 2 || b[0].ID != "cancel" || m.confirm.SafeID() != "cancel" {
		t.Errorf("buttons %+v safe %q", b, m.confirm.SafeID())
	}
	m.confirmOpen = false
	if cmd := m.handleConfirmSelect("cancel"); cmd != nil || f.Count("cp ") != 0 {
		t.Error("cancel copied")
	}
	m.kube.pendingFile = kubeFileTarget{context: "dev", namespace: "shop", pod: "web-0", local: "/tmp/conf.yaml", podPath: "/etc/app/conf.yaml"}
	m.confirmKind = confirmKindKubeUpload
	cmd := m.handleConfirmSelect("upload")
	if cmd == nil {
		t.Fatal("no upload command")
	}
	if msg := cmd().(kubeCopyMsg); msg.err != nil {
		t.Fatalf("upload = %v", msg.err)
	}
	if f.Count("cp --context dev --namespace shop /tmp/conf.yaml web-0:/etc/app/conf.yaml") != 1 {
		t.Errorf("calls = %v", f.Calls())
	}
}

func TestKubePodDebugAsksFirstThenOpensATerminal(t *testing.T) {
	_, m := kubeLoaded(t, 150, 42)
	var jobs []terminalJob
	old := kubeSpawnJob
	kubeSpawnJob = func(_ *Model, j terminalJob) error { jobs = append(jobs, j); return nil }
	t.Cleanup(func() { kubeSpawnJob = old })
	kubeCursorTo(t, m, "pods", "web-0")
	if cmd := kubeKey(t, m, "D"); cmd != nil {
		t.Error("D ran something before the card")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindKubeDebug || m.confirm.SafeID() != "cancel" {
		t.Fatal("no debug card with Cancel focused")
	}
	m.confirmOpen = false
	m.handleConfirmSelect("cancel")
	if len(jobs) != 0 {
		t.Fatal("cancel opened a debug shell")
	}
	kubeKey(t, m, "D")
	m.confirmOpen = false
	m.handleConfirmSelect("debug")
	if len(jobs) != 1 || !strings.Contains(jobs[0].Cmdline, "debug -it") || !strings.Contains(jobs[0].Cmdline, "web-0") {
		t.Errorf("jobs = %+v", jobs)
	}
}
