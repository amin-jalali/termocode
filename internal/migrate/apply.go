package migrate

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/amin-jalali/termocode/internal/keymap"
)

// FileContents are complete replacement contents; this package never writes them.
type FileContents struct {
	Config []byte
	Keymap []byte
	Tools  []string
	Theme  string
}

// Apply renders selected rows as complete config and keymap JSON contents.
// Callers must pass Current.KeymapRaw unchanged when the keymap file exists;
// this preserves entries that the importer does not understand.
func Apply(plan Plan) (FileContents, error) {
	config := make(map[string]any, len(plan.Current.Config))
	for k, v := range plan.Current.Config {
		config[k] = v
	}
	keys := make(map[string]json.RawMessage, len(plan.Current.Overrides))
	if plan.Current.KeymapExists && len(plan.Current.KeymapRaw) == 0 {
		return FileContents{}, fmt.Errorf("raw keymap.json is required when the keymap file exists")
	}
	if len(plan.Current.KeymapRaw) > 0 {
		if err := json.Unmarshal(plan.Current.KeymapRaw, &keys); err != nil {
			return FileContents{}, fmt.Errorf("decode keymap: %w", err)
		}
	}
	for k, action := range plan.Current.Overrides {
		if _, exists := keys[k]; !exists {
			b, _ := json.Marshal(keymap.ActionName(action))
			keys[k] = b
		}
	}
	out := FileContents{}
	for _, row := range plan.Rows {
		if !row.Apply {
			continue
		}
		switch row.Kind {
		case RowSetting:
			if row.Target != "" {
				config[row.Target] = row.Value
			}
		case RowKey:
			if row.Key != "" && row.Action != "" {
				b, _ := json.Marshal(row.Action)
				keys[row.Key] = b
			}
		case RowTheme:
			out.Theme = row.Target
		case RowExtension:
			out.Tools = append(out.Tools, row.Tools...)
		}
	}
	if len(out.Tools) > 1 {
		seen := map[string]bool{}
		unique := out.Tools[:0]
		for _, tool := range out.Tools {
			if !seen[tool] {
				seen[tool] = true
				unique = append(unique, tool)
			}
		}
		out.Tools = unique
		sort.Strings(out.Tools)
	}
	var err error
	if len(config) > 0 {
		out.Config, err = json.MarshalIndent(config, "", "  ")
		if err != nil {
			return FileContents{}, fmt.Errorf("encode config: %w", err)
		}
	}
	if len(keys) > 0 {
		out.Keymap, err = json.MarshalIndent(keys, "", "  ")
		if err != nil {
			return FileContents{}, fmt.Errorf("encode keymap: %w", err)
		}
	}
	return out, nil
}
