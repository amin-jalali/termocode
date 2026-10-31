package ai

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ResolveFilePrompts reads file://repo/relative.md system prompts for this
// run. The source stays unchanged; the returned copy includes the file text
// in its trust hash. Even a user profile must ask before using repo text.
func ResolveFilePrompts(root string, p Profile) (Profile, error) {
	p.Steps = append([]ProfileStep(nil), p.Steps...)
	if p.Loop != nil {
		loop := *p.Loop
		p.Loop = &loop
	}
	resolve := func(st *ProfileStep) error {
		path, ok := strings.CutPrefix(st.System, "file://")
		if !ok {
			return nil
		}
		path = filepath.FromSlash(path)
		if path == "" || filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
			return fmt.Errorf("step %s: prompt file must be relative to the repository", st.ID)
		}
		realRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			return fmt.Errorf("step %s: cannot read prompt root", st.ID)
		}
		text, size, ok := readRulesFile(realRoot, filepath.Join(root, path))
		if !ok {
			return fmt.Errorf("step %s: prompt file is empty, missing, or outside the repository", st.ID)
		}
		if size > MaxPromptBytes || len(text) > MaxPromptBytes {
			return fmt.Errorf("step %s: prompt file is over 8 KB", st.ID)
		}
		st.System = text
		p.Source = ProfileSourceProject
		return nil
	}
	for i := range p.Steps {
		if err := resolve(&p.Steps[i]); err != nil {
			return Profile{}, err
		}
	}
	if p.Loop != nil {
		if err := resolve(&p.Loop.Fix); err != nil {
			return Profile{}, err
		}
	}
	return p, nil
}
