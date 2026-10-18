package migrate

import (
	"fmt"
	"strconv"
	"strings"
)

// SettingMaps maps source setting names to termocode config keys.
var SettingMaps = map[string]map[string]string{
	"vscode":    {"editor.tabSize": "tab_size", "editor.wordWrap": "word_wrap", "files.autoSave": "auto_save", "files.exclude": "show_hidden", "search.maxResults": "search_max_results"},
	"vscodium":  {"editor.tabSize": "tab_size", "editor.wordWrap": "word_wrap", "files.autoSave": "auto_save", "files.exclude": "show_hidden", "search.maxResults": "search_max_results"},
	"cursor":    {"editor.tabSize": "tab_size", "editor.wordWrap": "word_wrap", "files.autoSave": "auto_save", "files.exclude": "show_hidden", "search.maxResults": "search_max_results"},
	"windsurf":  {"editor.tabSize": "tab_size", "editor.wordWrap": "word_wrap", "files.autoSave": "auto_save", "files.exclude": "show_hidden", "search.maxResults": "search_max_results"},
	"zed":       {"tab_size": "tab_size", "soft_wrap": "word_wrap", "autosave": "auto_save"},
	"helix":     {"editor.soft-wrap.enable": "word_wrap", "editor.auto-save": "auto_save"},
	"jetbrains": {"tabSize": "tab_size", "SAVE_FILES_ON_FRAME_DEACTIVATION": "auto_save"},
	"sublime":   {"tab_size": "tab_size", "word_wrap": "word_wrap", "save_on_focus_lost": "auto_save"},
}

func mapSetting(source string, s Setting) (string, any, string) {
	name := normalizeSettingName(source, s.Name)
	target := SettingMaps[source][name]
	if target == "" {
		return "", nil, "no equivalent"
	}
	value := s.Value
	switch target {
	case "tab_size", "search_max_results":
		if text, ok := value.(string); ok {
			if n, err := strconv.Atoi(strings.TrimSpace(text)); err == nil {
				value = float64(n)
			} else {
				return "", nil, "no equivalent"
			}
		} else if _, ok := value.(float64); !ok {
			if _, intOK := value.(int); !intOK {
				return "", nil, "no equivalent"
			}
		}
	case "show_hidden":
		excludes, ok := value.(map[string]any)
		if !ok {
			return "", nil, "no equivalent"
		}
		value = true
		for pattern, enabled := range excludes {
			if pattern == "**/.*" && enabled == true {
				value = false
			}
		}
	case "word_wrap":
		if v, ok := value.(string); ok {
			value = !strings.EqualFold(v, "off") && !strings.EqualFold(v, "none") && !strings.EqualFold(v, "false")
		} else if _, ok := value.(bool); !ok {
			return "", nil, "no equivalent"
		}
	case "auto_save":
		if _, ok := value.(map[string]any); ok {
			return "", nil, "no equivalent"
		}
		if v, ok := value.(string); ok {
			if b, err := strconv.ParseBool(v); err == nil {
				value = b
			} else {
				value = !strings.EqualFold(v, "off") && v != ""
			}
		} else if _, ok := value.(bool); !ok {
			return "", nil, "no equivalent"
		}
	}
	return target, value, fmt.Sprint(target, " = ", value)
}

func normalizeSettingName(source, name string) string {
	if source == "helix" && strings.HasPrefix(name, "[") {
		if i := strings.Index(name, "]."); i >= 0 {
			return strings.TrimPrefix(name[:i], "[") + "." + name[i+2:]
		}
	}
	return name
}
