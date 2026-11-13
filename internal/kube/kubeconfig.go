package kube

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// ── Add a cluster to the kubeconfig (CTX-39) ─────────────────────────
//
// Everything else in this package only reads the kubeconfig. This file
// is the one writer. It never rewrites the file: PlanAddCluster only
// inserts lines (the user's comments, order and layout stay), the app
// shows DiffText on a Cancel-first card, and WriteKubeconfig copies the
// old file to a timestamped backup before it writes the new one.

// ClusterAdd is one cluster to add: a cluster entry plus a context of the
// same name (no user: add credentials with your cluster's own tools).
type ClusterAdd struct {
	Name   string
	Server string // https://host:port
	CAFile string // optional absolute path of the CA certificate
}

var clusterNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@-]*$`)

// CheckClusterAdd refuses what must never reach the file.
func CheckClusterAdd(a ClusterAdd) error {
	if !clusterNameRe.MatchString(a.Name) {
		return errors.New("the name may use letters, digits and . _ : / @ - and must start with a letter or digit")
	}
	u, err := url.Parse(a.Server)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || strings.ContainsAny(a.Server, " \t\r\n\"'#") {
		return errors.New("the server must be a URL like https://10.0.0.1:6443")
	}
	if a.CAFile != "" {
		if !filepath.IsAbs(a.CAFile) || strings.ContainsAny(a.CAFile, "\r\n\"#") {
			return errors.New("the CA file must be an absolute path")
		}
	}
	return nil
}

// KubeconfigPath is the file kubectl writes: the first entry of
// KUBECONFIG, else ~/.kube/config.
func KubeconfigPath(env func(string) string, home string) string {
	if v := env("KUBECONFIG"); v != "" {
		if first := strings.Split(v, string(os.PathListSeparator))[0]; first != "" {
			return first
		}
	}
	return filepath.Join(home, ".kube", "config")
}

// scalar writes s plain when YAML reads it back as the same string.
func scalar(s string) string {
	var v any
	if err := yaml.Unmarshal([]byte(s), &v); err == nil {
		if got, ok := v.(string); ok && got == s && !strings.ContainsAny(s[:1], "!&*-?:,[]{}#|>@`%\"'") {
			return s
		}
	}
	return strconv.Quote(s)
}

func listNames(m map[string]any, key string) map[string]bool {
	out := map[string]bool{}
	l, _ := m[key].([]any)
	for _, e := range l {
		if em, ok := e.(map[string]any); ok {
			if n, ok := em["name"].(string); ok {
				out[n] = true
			}
		}
	}
	return out
}

// PlanAddCluster returns current with the cluster and its context added.
// Existing text is kept as it is; current-context is not touched. It
// refuses a name that is already used, a file that is not YAML and a
// layout it cannot extend line by line (e.g. `clusters: []`).
func PlanAddCluster(current []byte, a ClusterAdd) ([]byte, error) {
	if err := CheckClusterAdd(a); err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(current, &doc); err != nil {
		return nil, fmt.Errorf("the kubeconfig is not valid YAML: %w", err)
	}
	if listNames(doc, "clusters")[a.Name] {
		return nil, fmt.Errorf("a cluster named %s is already in the kubeconfig", a.Name)
	}
	if listNames(doc, "contexts")[a.Name] {
		return nil, fmt.Errorf("a context named %s is already in the kubeconfig", a.Name)
	}
	text := string(current)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if _, ok := doc["apiVersion"]; !ok {
		text = "apiVersion: v1\n" + text
	}
	if _, ok := doc["kind"]; !ok {
		text = "kind: Config\n" + text
	}
	cluster := []string{"- cluster:", "    server: " + scalar(a.Server)}
	if a.CAFile != "" {
		cluster = append(cluster, "    certificate-authority: "+scalar(a.CAFile))
	}
	cluster = append(cluster, "  name: "+scalar(a.Name))
	ctx := []string{"- context:", "    cluster: " + scalar(a.Name), "  name: " + scalar(a.Name)}
	var err error
	if text, err = insertEntry(text, "clusters", cluster); err != nil {
		return nil, err
	}
	if text, err = insertEntry(text, "contexts", ctx); err != nil {
		return nil, err
	}
	var check map[string]any
	if err := yaml.Unmarshal([]byte(text), &check); err != nil ||
		!listNames(check, "clusters")[a.Name] || !listNames(check, "contexts")[a.Name] {
		return nil, errors.New("the kubeconfig has a layout that cannot be extended safely; add the cluster with kubectl config set-cluster")
	}
	return []byte(text), nil
}

// insertEntry appends entry (written for a list at column 0) to the top
// level list `key`, or adds the list at the end when the key is missing.
func insertEntry(text, key string, entry []string) (string, error) {
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if text == "" {
		lines = nil
	}
	at := -1
	for i, l := range lines {
		if strings.TrimRight(l, " \t") == key+":" {
			at = i
			break
		}
		if strings.HasPrefix(l, key+":") {
			return "", errors.New("the kubeconfig writes " + key + " in a layout that cannot be extended safely (e.g. `" + key + ": []`)")
		}
	}
	if at < 0 {
		lines = append(lines, key+":")
		lines = append(lines, entry...)
		return strings.Join(lines, "\n") + "\n", nil
	}
	last, indent := at, ""
	for i := at + 1; i < len(lines); i++ {
		l := lines[i]
		if strings.TrimSpace(l) == "" || strings.HasPrefix(l, "#") {
			continue
		}
		if l[0] != ' ' && !strings.HasPrefix(l, "- ") {
			break
		}
		if last == at {
			indent = l[:len(l)-len(strings.TrimLeft(l, " "))]
		}
		last = i
	}
	add := make([]string, len(entry))
	for i, l := range entry {
		add[i] = indent + l
	}
	out := append(append(append([]string{}, lines[:last+1]...), add...), lines[last+1:]...)
	return strings.Join(out, "\n") + "\n", nil
}

// DiffText is DiffContext with 3 lines of context.
func DiffText(oldText, newText string) string { return DiffContext(oldText, newText, 3) }

// DiffContext is a unified-style diff of two texts with ctxN lines of
// context around each change ("" when they are the same).
func DiffContext(oldText, newText string, ctxN int) string {
	a, b := splitLines(oldText), splitLines(newText)
	type op struct {
		tag  byte
		text string
	}
	var ops []op
	if len(a)*len(b) > 4_000_000 {
		p := 0
		for p < len(a) && p < len(b) && a[p] == b[p] {
			p++
		}
		s := 0
		for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
			s++
		}
		for _, l := range a[:p] {
			ops = append(ops, op{' ', l})
		}
		for _, l := range a[p : len(a)-s] {
			ops = append(ops, op{'-', l})
		}
		for _, l := range b[p : len(b)-s] {
			ops = append(ops, op{'+', l})
		}
		for _, l := range a[len(a)-s:] {
			ops = append(ops, op{' ', l})
		}
	} else {
		n, m := len(a), len(b)
		lcs := make([][]int, n+1)
		for i := range lcs {
			lcs[i] = make([]int, m+1)
		}
		for i := n - 1; i >= 0; i-- {
			for j := m - 1; j >= 0; j-- {
				if a[i] == b[j] {
					lcs[i][j] = lcs[i+1][j+1] + 1
				} else {
					lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
				}
			}
		}
		i, j := 0, 0
		for i < n || j < m {
			switch {
			case i < n && j < m && a[i] == b[j]:
				ops = append(ops, op{' ', a[i]})
				i++
				j++
			case j < m && (i == n || lcs[i][j+1] >= lcs[i+1][j]):
				ops = append(ops, op{'+', b[j]})
				j++
			default:
				ops = append(ops, op{'-', a[i]})
				i++
			}
		}
	}
	show := make([]bool, len(ops))
	changed := false
	for i, o := range ops {
		if o.tag != ' ' {
			changed = true
			for k := max(0, i-ctxN); k <= min(len(ops)-1, i+ctxN); k++ {
				show[k] = true
			}
		}
	}
	if !changed {
		return ""
	}
	var sb strings.Builder
	for i, o := range ops {
		if !show[i] {
			if i > 0 && show[i-1] {
				sb.WriteString("@@\n")
			}
			continue
		}
		sb.WriteString(string(o.tag) + o.text + "\n")
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// WriteKubeconfig writes data to path. An existing file is first copied to
// path+".bak-YYYYMMDD-HHMMSS" (mode 0600); when the copy fails nothing is
// written. It returns the backup's path ("" for a new file).
func WriteKubeconfig(path string, data []byte, now time.Time) (string, error) {
	mode := os.FileMode(0o600)
	backup := ""
	if old, err := os.ReadFile(path); err == nil {
		if st, err := os.Stat(path); err == nil {
			mode = st.Mode().Perm()
		}
		stamp := path + ".bak-" + now.Format("20060102-150405")
		backup = stamp
		for n := 1; ; n++ {
			f, err := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err == nil {
				_, werr := f.Write(old)
				if cerr := f.Close(); werr == nil {
					werr = cerr
				}
				if werr != nil {
					return "", fmt.Errorf("backup failed, kubeconfig not changed: %w", werr)
				}
				break
			}
			if !errors.Is(err, os.ErrExist) {
				return "", fmt.Errorf("backup failed, kubeconfig not changed: %w", err)
			}
			backup = stamp + "-" + strconv.Itoa(n)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return backup, nil
}
