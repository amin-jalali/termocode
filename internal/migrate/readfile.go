package migrate

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/jsonc"
)

// ReadFile reads one editor file the user points to (a settings.json or
// keybindings.json copied from another machine, a Zed keymap, a Helix
// config.toml, a Sublime settings or keymap file, a JetBrains keymap).
// The reader is chosen by the file name and content. It reads only that
// file (never its neighbours) and writes nothing next to it. A folder, a
// device, a file over 1 MB or a file that does not parse is an error.
func ReadFile(path string) (Source, Document, error) {
	var src Source
	if strings.TrimSpace(path) == "" {
		return src, Document{}, errors.New("no file chosen")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return src, Document{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return src, Document{}, fmt.Errorf("%s cannot be read: %s", path, reasonOf(err))
	}
	if info.IsDir() {
		return src, Document{}, fmt.Errorf("%s is a folder; pick a file", path)
	}
	if !info.Mode().IsRegular() {
		return src, Document{}, fmt.Errorf("%s is not a regular file", path)
	}
	if info.Size() > maxConfigFile {
		return src, Document{}, fmt.Errorf("%s is larger than 1 MB", path)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return src, Document{}, fmt.Errorf("%s cannot be read: %s", path, reasonOf(err))
	}
	base := filepath.Base(abs)
	id, product, layout, err := classifyFile(base, b)
	if err != nil {
		return src, Document{}, fmt.Errorf("%s: %w", base, err)
	}
	// The existing readers take a folder. Give them a private copy of just
	// this file, so neighbours of the chosen file are never read.
	root, err := os.MkdirTemp("", "termocode-import-")
	if err != nil {
		return src, Document{}, err
	}
	defer os.RemoveAll(root)
	dir := filepath.Join(root, filepath.FromSlash(layout.dir))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return src, Document{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, layout.name), b, 0o600); err != nil {
		return src, Document{}, err
	}
	src = Source{ID: id, Title: base + " (" + product + ")", Dir: filepath.Dir(abs), Files: []string{base}}
	var doc Document
	switch id {
	case "vscode":
		doc, err = ReadVSCode(dir)
	case "zed":
		doc, err = ReadZed(dir)
	case "helix":
		doc, err = ReadHelix(dir)
	case "sublime":
		doc, err = ReadSublime(dir)
	case "jetbrains":
		doc, err = ReadJetBrains(filepath.Dir(dir))
	}
	if err != nil {
		return src, Document{}, fmt.Errorf("%s: %w", base, err)
	}
	return src, doc, nil
}

// fileLayout is where classifyFile's copy goes below the private folder.
type fileLayout struct{ dir, name string }

// classifyFile decides which editor a file belongs to and checks that it
// parses. A name it does not know is an error.
func classifyFile(base string, b []byte) (id, product string, l fileLayout, err error) {
	lower := strings.ToLower(base)
	notValid := func(what string) error { return fmt.Errorf("not valid %s", what) }
	switch {
	case strings.HasSuffix(lower, ".sublime-settings"):
		var v map[string]any
		if json.Unmarshal(jsonc.Strip(trimBOM(b)), &v) != nil {
			return "", "", l, notValid("JSON")
		}
		name := "Preferences.sublime-settings"
		if lower == "package control.sublime-settings" {
			name = "Package Control.sublime-settings"
		}
		return "sublime", "Sublime Text", fileLayout{".config/sublime-text/Packages/User", name}, nil
	case strings.HasSuffix(lower, ".sublime-keymap"):
		var v []any
		if json.Unmarshal(jsonc.Strip(trimBOM(b)), &v) != nil {
			return "", "", l, notValid("JSON (a list of key bindings)")
		}
		return "sublime", "Sublime Text", fileLayout{".config/sublime-text/Packages/User", sublimeKeymapFiles[0]}, nil
	case strings.HasSuffix(lower, ".toml"):
		name := "config.toml"
		if lower == "languages.toml" {
			name = "languages.toml"
		}
		return "helix", "Helix", fileLayout{".config/helix", name}, nil
	case strings.HasSuffix(lower, ".xml"):
		var probe struct {
			XMLName xml.Name
		}
		if xml.Unmarshal(b, &probe) != nil {
			return "", "", l, notValid("XML")
		}
		if probe.XMLName.Local != "keymap" {
			return "", "", l, errors.New("not a JetBrains keymap (the XML does not start with <keymap>)")
		}
		return "jetbrains", "JetBrains", fileLayout{".config/JetBrains/Imported/keymaps", base}, nil
	case strings.HasSuffix(lower, ".json"):
		return classifyJSON(lower, b)
	}
	return "", "", l, errors.New("not a supported editor file (settings.json, keybindings.json, keymap.json, config.toml, *.sublime-settings, *.sublime-keymap or a JetBrains keymap .xml)")
}

func classifyJSON(lower string, b []byte) (id, product string, l fileLayout, err error) {
	var v any
	if json.Unmarshal(jsonc.Strip(trimBOM(b)), &v) != nil {
		return "", "", l, errors.New("not valid JSON")
	}
	vs := fileLayout{".config/Code/User", "settings.json"}
	vk := fileLayout{".config/Code/User", "keybindings.json"}
	zs := fileLayout{".config/zed", "settings.json"}
	zk := fileLayout{".config/zed", "keymap.json"}
	switch x := v.(type) {
	case []any:
		for _, it := range x {
			if o, ok := it.(map[string]any); ok {
				if _, ok := o["bindings"]; ok {
					return "zed", "Zed", zk, nil
				}
				if _, ok := o["command"]; ok {
					return "vscode", "VS Code", vk, nil
				}
			}
		}
		if len(x) == 0 {
			if strings.Contains(lower, "keymap") {
				return "zed", "Zed", zk, nil
			}
			return "vscode", "VS Code", vk, nil
		}
	case map[string]any:
		// VS Code setting names have dots ("editor.tabSize"); Zed's do not.
		if len(x) == 0 {
			return "vscode", "VS Code", vs, nil
		}
		for k := range x {
			if strings.Contains(k, ".") {
				return "vscode", "VS Code", vs, nil
			}
		}
		return "zed", "Zed", zs, nil
	}
	return "", "", l, errors.New("not a supported editor file (the JSON is not a settings object or a key binding list)")
}

// reasonOf is the short cause of a file error ("permission denied").
func reasonOf(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}
