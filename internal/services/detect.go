package services

import (
	"os"
	"path/filepath"
	"strings"
)

// ComposeFileNames is compose's own lookup order.
var ComposeFileNames = []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"}

// Detection is what a project declares.
type Detection struct {
	ComposeFiles []string // root-relative (or absolute from COMPOSE_FILE)
	Profiles     []string
	Project      string // from services.json; "" = compose decides
	Compose      bool   // the compose backend can run (docker on PATH, not Android)
	Problem      Problem
	Local        []Service
	ConfigErr    error // services.json does not parse
}

// Empty reports a project with nothing to show.
func (d Detection) Empty() bool {
	return len(d.ComposeFiles) == 0 && len(d.Local) == 0 && d.ConfigErr == nil
}

// Detect looks at root for services. env, lookPath and goos are passed in
// so tests choose them; it never starts a process.
func Detect(root string, env func(string) string, lookPath func(string) (string, error), goos string) Detection {
	var d Detection
	var fc FileConfig
	if b, err := os.ReadFile(filepath.Join(root, ConfigPath)); err == nil {
		fc, err = ParseFileConfig(b)
		if err != nil {
			d.ConfigErr = err
		}
		d.Local = fc.LocalServices()
		d.Profiles = fc.Compose.Profiles
		d.Project = fc.Compose.Project
	}
	for _, name := range []string{"Procfile", "Procfile.dev"} {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		for _, s := range ParseProcfile(string(b), name) {
			if !hasService(d.Local, s.Name) {
				d.Local = append(d.Local, s)
			}
		}
		break
	}
	if goos == "android" { // Termux: local only, no Docker nag
		return d
	}
	switch {
	case env("COMPOSE_FILE") != "":
		sep := env("COMPOSE_PATH_SEPARATOR")
		if sep == "" {
			sep = ":"
		}
		for _, f := range strings.Split(env("COMPOSE_FILE"), sep) {
			if f = strings.TrimSpace(f); f != "" {
				d.ComposeFiles = append(d.ComposeFiles, f)
			}
		}
	case fc.Compose.File != "":
		d.ComposeFiles = []string{fc.Compose.File}
	default:
		for _, name := range ComposeFileNames {
			if st, err := os.Stat(filepath.Join(root, name)); err == nil && !st.IsDir() {
				d.ComposeFiles = []string{name}
				break
			}
		}
	}
	if len(d.ComposeFiles) == 0 {
		return d
	}
	if _, err := lookPath("docker"); err != nil {
		d.Problem = Problem{Kind: ProblemNoDocker}
		return d
	}
	d.Compose = true
	return d
}

func hasService(list []Service, name string) bool {
	for _, s := range list {
		if s.Name == name {
			return true
		}
	}
	return false
}
