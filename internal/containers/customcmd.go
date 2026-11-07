package containers

import (
	"errors"
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// CustomCommand is one user-defined action for a row of a view, read from
// config. It runs only after a card shows the expanded command; there is
// no shell: every argv entry stays one argument.
type CustomCommand struct {
	Name string   `json:"name"`
	View string   `json:"view"`          // containers, images, volumes, networks, compose, pods, deployments, services, jobs
	Key  string   `json:"key,omitempty"` // optional hotkey in that view
	Argv []string `json:"argv"`          // {{id}}, {{name}}, {{namespace}}, {{context}}, {{image}}
}

var customViews = map[string]bool{
	"containers": true, "images": true, "volumes": true, "networks": true, "compose": true,
	"pods": true, "deployments": true, "services": true, "jobs": true,
}

// keys the views already use; a custom command may not take them.
var reservedKeys = map[string]bool{
	"enter": true, "esc": true, "up": true, "down": true, "left": true, "right": true,
	"tab": true, "shift+tab": true, "pgup": true, "pgdown": true, "home": true, "end": true,
	"ctrl+c": true, "/": true, "?": true, "q": true,
}

var placeholderRe = lazyre.New(`\{\{\s*([a-z]+)\s*\}\}`)

// Expand fills the placeholders of c.Argv from the row's fields. A value
// that is empty, or that would make an argument start with "-", is refused.
func (c CustomCommand) Expand(row map[string]string) ([]string, error) {
	out := make([]string, len(c.Argv))
	for i, a := range c.Argv {
		var bad error
		s := placeholderRe.Regexp().ReplaceAllStringFunc(a, func(m string) string {
			key := placeholderRe.Regexp().FindStringSubmatch(m)[1]
			v, ok := row[key]
			switch {
			case !ok:
				bad = fmt.Errorf("unknown placeholder {{%s}} in %q", key, c.Name)
			case v == "":
				bad = fmt.Errorf("this row has no %s for %q", key, c.Name)
			}
			return v
		})
		if bad != nil {
			return nil, bad
		}
		if strings.HasPrefix(s, "-") && !strings.HasPrefix(a, "-") {
			return nil, fmt.Errorf("a row value would turn into an option in %q", c.Name)
		}
		out[i] = s
	}
	return out, nil
}

// ValidateCustomCommands checks a config's custom commands.
func ValidateCustomCommands(cmds []CustomCommand) error {
	keys := map[string]string{}
	for _, c := range cmds {
		if strings.TrimSpace(c.Name) == "" {
			return errors.New("a custom command has no name")
		}
		if !customViews[c.View] {
			return fmt.Errorf("custom command %q: unknown view %q", c.Name, c.View)
		}
		if len(c.Argv) == 0 || strings.TrimSpace(c.Argv[0]) == "" {
			return fmt.Errorf("custom command %q: no program", c.Name)
		}
		if placeholderRe.MatchString(c.Argv[0]) {
			return fmt.Errorf("custom command %q: the program cannot come from the row", c.Name)
		}
		if c.Key == "" {
			continue
		}
		k := strings.ToLower(c.Key)
		if reservedKeys[k] {
			return fmt.Errorf("custom command %q: key %s is taken by the view", c.Name, c.Key)
		}
		if other, ok := keys[c.View+"\x00"+k]; ok {
			return fmt.Errorf("custom commands %q and %q use the same key %s", other, c.Name, c.Key)
		}
		keys[c.View+"\x00"+k] = c.Name
	}
	return nil
}

// CustomCommandsFor are the commands of one view, in config order.
func CustomCommandsFor(cmds []CustomCommand, view string) []CustomCommand {
	var out []CustomCommand
	for _, c := range cmds {
		if c.View == view {
			out = append(out, c)
		}
	}
	return out
}

// CommandPreview is argv as a shell-like line for the card.
func CommandPreview(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		if a == "" || strings.ContainsAny(a, " \t\n'\"\\$`;&|<>*?()[]{}#~") {
			parts[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		} else {
			parts[i] = a
		}
	}
	return strings.Join(parts, " ")
}
