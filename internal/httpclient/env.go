package httpclient

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Environment file names (the VS Code / JetBrains convention).
const (
	EnvFile        = "http-client.env.json"
	PrivateEnvFile = "http-client.private.env.json"
	DotenvFile     = ".env"
	SharedEnv      = "$shared"
)

// Envs holds every environment file found between a `.http` file's
// folder and the workspace root. A nearer folder wins over a farther
// one; the private file wins over the public one.
type Envs struct {
	// Files lists the env files that were read, nearest first.
	Files []string
	// Errs holds one error per file that could not be read; the other
	// files still load (ADR 0005: config never breaks startup).
	Errs    []error
	public  map[string]map[string]string
	private map[string]map[string]string
	// Dotenv is the merged .env content for {{$dotenv NAME}}.
	Dotenv map[string]string
}

// LoadEnvs walks from dir up to root (both included; dir outside root
// reads only dir) and merges the env files it finds. The result is
// shared and must not be changed. It is kept until one of the files it
// looked at changes (size, time, inode), appears or goes away, so a send
// or a buffer refresh does not read and decode the env files each time.
func LoadEnvs(dir, root string) *Envs {
	key := [2]string{dir, root}
	paths := envPaths(dir, root)
	stamps, ok := statAll(paths)
	envMu.Lock()
	c := envCache[key]
	envMu.Unlock()
	if ok && c != nil && sameStamps(c.stamps, stamps) {
		return c.envs
	}
	e := loadEnvs(dir, root)
	if ok {
		envMu.Lock()
		if len(envCache) >= 64 {
			clear(envCache)
		}
		envCache[key] = &envEntry{stamps: stamps, envs: e}
		envMu.Unlock()
	}
	return e
}

type envEntry struct {
	stamps []os.FileInfo // per envPaths entry; nil = missing
	envs   *Envs
}

var (
	envMu    sync.Mutex
	envCache = map[[2]string]*envEntry{}
)

// envPaths lists every file LoadEnvs looks at.
func envPaths(dir, root string) []string {
	var out []string
	for _, d := range envDirs(dir, root) {
		out = append(out, filepath.Join(d, EnvFile), filepath.Join(d, PrivateEnvFile), filepath.Join(d, DotenvFile))
	}
	return out
}

// statAll stats paths; ok=false when one could not be checked (then
// nothing is cached).
func statAll(paths []string) ([]os.FileInfo, bool) {
	out := make([]os.FileInfo, len(paths))
	for i, p := range paths {
		st, err := os.Stat(p)
		switch {
		case err == nil:
			out[i] = st
		case errors.Is(err, os.ErrNotExist):
		default:
			return nil, false
		}
	}
	return out, true
}

func sameStamps(a, b []os.FileInfo) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if (x == nil) != (y == nil) {
			return false
		}
		if x != nil && !(os.SameFile(x, y) && x.Size() == y.Size() && x.ModTime().Equal(y.ModTime())) {
			return false
		}
	}
	return true
}

func loadEnvs(dir, root string) *Envs {
	e := &Envs{
		public:  map[string]map[string]string{},
		private: map[string]map[string]string{},
		Dotenv:  map[string]string{},
	}
	dirs := envDirs(dir, root)
	// Farthest first, so nearer folders overwrite.
	for i := len(dirs) - 1; i >= 0; i-- {
		d := dirs[i]
		e.loadJSON(filepath.Join(d, EnvFile), e.public)
		e.loadJSON(filepath.Join(d, PrivateEnvFile), e.private)
		e.loadDotenv(filepath.Join(d, DotenvFile))
	}
	// Report nearest first.
	for l, r := 0, len(e.Files)-1; l < r; l, r = l+1, r-1 {
		e.Files[l], e.Files[r] = e.Files[r], e.Files[l]
	}
	return e
}

func envDirs(dir, root string) []string {
	dir = filepath.Clean(dir)
	root = filepath.Clean(root)
	var out []string
	rel, err := filepath.Rel(root, dir)
	if root == "" || err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return []string{dir}
	}
	for d := dir; ; {
		out = append(out, d)
		if d == root {
			break
		}
		p := filepath.Dir(d)
		if p == d {
			break
		}
		d = p
	}
	return out
}

// envReadFile reads an env file (a test hook).
var envReadFile = os.ReadFile

func (e *Envs) loadJSON(path string, into map[string]map[string]string) {
	b, err := envReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			e.Errs = append(e.Errs, fmt.Errorf("%s: %w", path, err))
		}
		return
	}
	e.Files = append(e.Files, path)
	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		e.Errs = append(e.Errs, fmt.Errorf("%s: %w", path, err))
		return
	}
	for env, vars := range raw {
		m := into[env]
		if m == nil {
			m = map[string]string{}
			into[env] = m
		}
		for k, v := range vars {
			m[k] = jsonScalar(v)
		}
	}
}

// jsonScalar turns a JSON value into the text that is substituted:
// strings unquoted, everything else as written.
func jsonScalar(v json.RawMessage) string {
	var s string
	if err := json.Unmarshal(v, &s); err == nil {
		return s
	}
	return strings.TrimSpace(string(v))
}

func (e *Envs) loadDotenv(path string) {
	b, err := envReadFile(path)
	if err != nil {
		return
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			if v[0] == '"' {
				if u, err := strconv.Unquote(v); err == nil {
					v = u
				} else {
					v = v[1 : len(v)-1]
				}
			} else {
				v = v[1 : len(v)-1]
			}
		}
		e.Dotenv[k] = v
	}
}

// Names lists the environment names (not $shared), sorted.
func (e *Envs) Names() []string {
	if e == nil {
		return nil
	}
	seen := map[string]bool{}
	for _, m := range []map[string]map[string]string{e.public, e.private} {
		for k := range m {
			if k != SharedEnv {
				seen[k] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Lookup finds name in env: private env, public env, private $shared,
// public $shared. private reports a value from the private file.
func (e *Envs) Lookup(env, name string) (val string, private, ok bool) {
	if e == nil {
		return "", false, false
	}
	type src struct {
		m       map[string]map[string]string
		env     string
		private bool
	}
	srcs := []src{{e.private, env, true}, {e.public, env, false}, {e.private, SharedEnv, true}, {e.public, SharedEnv, false}}
	for _, s := range srcs {
		if s.env == "" {
			continue
		}
		if v, ok := s.m[s.env][name]; ok {
			return v, s.private, true
		}
	}
	return "", false, false
}

// IsPrivate reports whether name in env comes from a private file.
func (e *Envs) IsPrivate(env, name string) bool {
	_, p, ok := e.Lookup(env, name)
	return ok && p
}

// VarNames lists the variable names visible in env (with $shared),
// sorted.
func (e *Envs) VarNames(env string) []string {
	if e == nil {
		return nil
	}
	seen := map[string]bool{}
	for _, m := range []map[string]map[string]string{e.public, e.private} {
		for _, en := range []string{env, SharedEnv} {
			for k := range m[en] {
				seen[k] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// State is the per-workspace HTTP state kept in http/state.json.
type State struct {
	// Envs maps a workspace root to its selected environment.
	Envs map[string]string `json:"envs,omitempty"`
}

// Env returns the environment selected for root ("" = none).
func (s *State) Env(root string) string { return s.Envs[root] }

// SetEnv selects env for root ("" clears it).
func (s *State) SetEnv(root, env string) {
	if env == "" {
		delete(s.Envs, root)
		return
	}
	if s.Envs == nil {
		s.Envs = map[string]string{}
	}
	s.Envs[root] = env
}

// LoadState reads state.json; a missing file is an empty state.
func LoadState(path string) (State, error) {
	var s State
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}
		return s, err
	}
	err = json.Unmarshal(b, &s)
	return s, err
}

// Save writes state.json (folder 0700, file 0600, atomic).
func (s State) Save(path string) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writePrivate(path, append(b, '\n'))
}

// writePrivate writes data via a temp file + rename; the folder is
// 0700 and the file 0600.
func writePrivate(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}
