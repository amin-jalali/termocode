package app

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/kube"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Kubernetes view: pod files and debug (CTX-30) ────────────────────────
//
// F on a pod browses its folders (`kubectl exec … ls -la`, read only); a
// picked folder opens it, a picked file asks for a local path and copies
// it there (`kubectl cp`, never over a file that is there). U uploads a
// local file into the pod: a prompt for the local file, one for the pod
// path, then a card (Cancel first). D opens a shell in a new ephemeral
// debug container after a card: it changes the pod spec. The AI can
// reach none of it (no palette id).

const (
	confirmKindKubeUpload    confirmKindEnum = 3486
	confirmKindKubeDebug     confirmKindEnum = 3487
	promptKindKubeDownload   promptKindEnum  = 3488
	promptKindKubeUpload     promptKindEnum  = 3489
	promptKindKubeUploadDest promptKindEnum  = 3490
)

// kubeFileTarget is the pod file a prompt or card asks about, in the
// scope it was asked in.
type kubeFileTarget struct {
	context, namespace, pod, container string
	podPath, local                     string
}

type kubePodDirMsg struct {
	pod, dir string
	files    []kube.PodFile
	err      error
}

type kubeCopyMsg struct {
	up             bool
	podPath, local string
	err            error
}

// kubeListPodDir lists one folder of a pod.
func (m *Model) kubeListPodDir(pod, dir string) tea.Cmd {
	c := m.kube.client()
	return func() tea.Msg {
		files, err := c.ListPodDir(context.Background(), pod, "", dir)
		return kubePodDirMsg{pod: pod, dir: dir, files: files, err: err}
	}
}

// kubePodDirItems are the picker rows: ".." first (not at /), folders,
// then files. ID: "k8s:pfile:<d|f><pod>:<path>".
func kubePodDirItems(msg kubePodDirMsg) []picker.Item {
	var dirs, files []picker.Item
	if msg.dir != "/" {
		dirs = append(dirs, picker.Item{ID: "k8s:pfile:d" + msg.pod + ":" + path.Dir(msg.dir), Title: "..", Hint: "up"})
	}
	for _, f := range msg.files {
		p := path.Join(msg.dir, f.Name)
		if f.Dir {
			dirs = append(dirs, picker.Item{ID: "k8s:pfile:d" + msg.pod + ":" + p, Title: f.Name + "/", Hint: f.Mode})
			continue
		}
		hint := f.Mode
		if f.Link != "" {
			hint = "→ " + f.Link
		}
		files = append(files, picker.Item{ID: "k8s:pfile:f" + msg.pod + ":" + p, Title: f.Name, Hint: hint})
	}
	return append(dirs, files...)
}

func (m *Model) onKubePodDir(msg kubePodDirMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot list "+msg.dir, msg.err.Error())
	}
	m.openTasksPickerItems(" Files in "+msg.pod+":"+msg.dir+" ", kubePodDirItems(msg))
	return nil
}

// handleKubeFilePick runs a picked file row (value "<d|f><pod>:<path>").
func (m *Model) handleKubeFilePick(value string) tea.Cmd {
	if len(value) < 2 {
		return nil
	}
	pod, p, ok := strings.Cut(value[1:], ":")
	if !ok {
		return nil
	}
	if value[0] == 'd' {
		return m.kubeListPodDir(pod, p)
	}
	st := m.kube
	st.pendingFile = kubeFileTarget{context: st.context, namespace: st.namespace, pod: pod, podPath: p}
	local := path.Base(p)
	if root, err := m.rootDir(); err == nil {
		local = filepath.Join(root, local)
	}
	m.askKubePath(promptKindKubeDownload, " Download from pod ", "Save "+pod+":"+p+" to (a local path):", local, "Never overwrites a file.", "")
	return nil
}

func (m *Model) askKubePath(kind promptKindEnum, title, label, value, note, errText string) {
	m.prompt = prompt.New(title, label, value)
	m.prompt.SetNote(note)
	if errText != "" {
		m.prompt.SetError(errText)
	}
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = kind
}

// askKubeUpload starts an upload into pod p: the local file first (the
// current file is the start value).
func (m *Model) askKubeUpload(p kube.Pod) {
	st := m.kube
	st.pendingFile = kubeFileTarget{context: st.context, namespace: st.namespace, pod: p.Name}
	local := ""
	if m.nvim != nil {
		if s, err := m.nvim.EvalLuaString(`return vim.fn.expand('%:p')`); err == nil {
			local = strings.TrimSpace(s)
		}
	}
	m.askKubePath(promptKindKubeUpload, " Upload to pod ", "Local file to copy into "+p.Name+":", local, "A card asks before anything is copied.", "")
}

// handleKubeFilePrompt takes the download and upload path answers.
func (m *Model) handleKubeFilePrompt(value string) (tea.Cmd, bool) {
	st := m.kube
	t := st.pendingFile
	text := strings.TrimSpace(value)
	switch m.promptKind {
	case promptKindKubeDownload:
		if t.pod == "" {
			return nil, true
		}
		if !filepath.IsAbs(text) {
			m.askKubePath(promptKindKubeDownload, " Download from pod ", "Save "+t.pod+":"+t.podPath+" to (a local path):", text, "Never overwrites a file.", "Type an absolute path.")
			return nil, true
		}
		if _, err := os.Lstat(text); err == nil {
			m.askKubePath(promptKindKubeDownload, " Download from pod ", "Save "+t.pod+":"+t.podPath+" to (a local path):", text, "Never overwrites a file.", "This path is there already. Pick another one.")
			return nil, true
		}
		st.pendingFile = kubeFileTarget{}
		c := st.client()
		c.Context, c.Namespace = t.context, t.namespace
		return func() tea.Msg {
			return kubeCopyMsg{podPath: t.pod + ":" + t.podPath, local: text, err: c.CopyFromPod(context.Background(), t.pod, t.container, t.podPath, text)}
		}, true
	case promptKindKubeUpload:
		if t.pod == "" {
			return nil, true
		}
		if fi, err := os.Stat(text); err != nil || !filepath.IsAbs(text) || fi.IsDir() {
			m.askKubePath(promptKindKubeUpload, " Upload to pod ", "Local file to copy into "+t.pod+":", text, "A card asks before anything is copied.", "Type the absolute path of a file.")
			return nil, true
		}
		st.pendingFile.local = text
		m.askKubePath(promptKindKubeUploadDest, " Upload to pod ", "Path in "+t.pod+" (absolute):", "/tmp/"+filepath.Base(text), "A file there is replaced.", "")
		return nil, true
	case promptKindKubeUploadDest:
		if t.pod == "" || t.local == "" {
			return nil, true
		}
		if !path.IsAbs(text) || strings.HasPrefix(text, "-") {
			m.askKubePath(promptKindKubeUploadDest, " Upload to pod ", "Path in "+t.pod+" (absolute):", text, "A file there is replaced.", "Type an absolute path.")
			return nil, true
		}
		st.pendingFile.podPath = text
		body := "This copies " + t.local + "\ninto " + t.pod + ":" + text + ". A file there is replaced.\n\n" +
			"Context: " + t.context + " · Namespace: " + t.namespace + "\n" +
			"Command: kubectl cp " + t.local + " " + t.pod + ":" + text
		m.confirm = confirm.Risky("Upload into the pod?", body, confirm.Button{ID: "cancel", Title: "Cancel"},
			confirm.Button{ID: "upload", Title: "Upload", Style: confirm.StyleDestructive})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindKubeUpload
		return nil, true
	}
	return nil, false
}

// askKubeDebug opens the debug card for pod p.
func (m *Model) askKubeDebug(p kube.Pod) {
	st := m.kube
	target := ""
	if len(p.Containers) > 0 {
		target = p.Containers[0]
	}
	st.pendingFile = kubeFileTarget{context: st.context, namespace: st.namespace, pod: p.Name, container: target}
	shared := ""
	if target != "" {
		shared = " It shares the processes of " + target + "."
	}
	body := "This adds a debug container (" + kube.DefaultDebugImage + ") to " + p.Name + " and opens a shell in it." + shared + "\n" +
		"It changes the pod spec; the container stays until the pod is gone.\n\n" +
		"Context: " + st.context + " · Namespace: " + st.namespace + "\n" +
		"Command: kubectl debug -it " + p.Name + " --image=" + kube.DefaultDebugImage
	m.confirm = confirm.Risky("Debug the pod?", body, confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "debug", Title: "Debug", Style: confirm.StyleDestructive})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindKubeDebug
}

// handleKubeFileConfirm runs the upload and debug cards' answers.
func (m *Model) handleKubeFileConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindKubeUpload && m.confirmKind != confirmKindKubeDebug {
		return nil, false
	}
	st := m.kube
	t := st.pendingFile
	st.pendingFile = kubeFileTarget{}
	c := st.client()
	c.Context, c.Namespace = t.context, t.namespace // the scope the card showed
	if m.confirmKind == confirmKindKubeDebug {
		if id != "debug" || t.pod == "" {
			return nil, true
		}
		cmd, err := c.DebugCommand(t.pod, t.container, "")
		if err != nil {
			return m.servicesToast(toast.Errr, "Cannot debug "+t.pod, err.Error()), true
		}
		job := terminalJob{Label: cmd.Label, Cmdline: svcShellLine(cmd.Argv), Service: "k8s:debug:" + t.context + "/" + t.namespace + "/" + t.pod}
		if err := kubeSpawnJob(m, job); err != nil {
			return m.servicesToast(toast.Errr, "Cannot open the debug shell", err.Error()), true
		}
		return nil, true
	}
	if id != "upload" || t.pod == "" || t.local == "" || t.podPath == "" {
		return nil, true
	}
	return func() tea.Msg {
		return kubeCopyMsg{up: true, podPath: t.pod + ":" + t.podPath, local: t.local, err: c.CopyToPod(context.Background(), t.pod, t.container, t.local, t.podPath)}
	}, true
}

func (m *Model) onKubeCopy(msg kubeCopyMsg) tea.Cmd {
	if msg.err != nil {
		return m.servicesToast(toast.Errr, "Cannot copy "+msg.podPath, msg.err.Error())
	}
	if msg.up {
		return m.servicesToast(toast.Info, "Uploaded", msg.local+" → "+msg.podPath)
	}
	return m.servicesToast(toast.Info, "Downloaded", msg.podPath+" → "+msg.local)
}
