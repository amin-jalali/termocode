package migrate

import (
	"archive/zip"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// jetBrainsProducts maps the config folder prefix to a product title.
var jetBrainsProducts = map[string]string{
	"IntelliJIdea": "IntelliJ IDEA", "IdeaIC": "IntelliJ IDEA Community", "PyCharm": "PyCharm", "PyCharmCE": "PyCharm Community",
	"GoLand": "GoLand", "WebStorm": "WebStorm", "PhpStorm": "PhpStorm", "CLion": "CLion", "RubyMine": "RubyMine",
	"Rider": "Rider", "DataGrip": "DataGrip", "DataSpell": "DataSpell", "RustRover": "RustRover", "AppCode": "AppCode", "Aqua": "Aqua",
}

var jetBrainsDir = lazyre.New(`^([A-Za-z]+)(\d{4}(?:\.\d+)+)$`)

// detectJetBrains returns the newest config folder of each JetBrains product
// that has options or keymaps in it.
func detectJetBrains(home, goos, config string) []Source {
	root := filepath.Join(config, "JetBrains")
	switch goos {
	case "darwin":
		root = filepath.Join(home, "Library", "Application Support", "JetBrains")
	case "windows":
		root = filepath.Join(home, "AppData", "Roaming", "JetBrains")
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	type found struct {
		product, version string
		dir              string
	}
	newest := map[string]found{}
	for _, e := range ents {
		m := jetBrainsDir.FindStringSubmatch(e.Name())
		if m == nil || !e.IsDir() || jetBrainsProducts[m[1]] == "" {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if !isDir(filepath.Join(dir, "options")) && !isDir(filepath.Join(dir, "keymaps")) {
			continue
		}
		if old, ok := newest[m[1]]; ok && compareVersions(old.version, m[2]) >= 0 {
			continue
		}
		newest[m[1]] = found{m[1], m[2], dir}
	}
	var out []Source
	for _, f := range newest {
		out = append(out, Source{"jetbrains", jetBrainsProducts[f.product] + " " + f.version, f.dir,
			[]string{"options/keymap.xml", "keymaps/*.xml", "options/laf.xml", "options/colors.scheme.xml", "options/editor.xml", "plugins/"}})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out
}

func isDir(p string) bool {
	i, err := os.Stat(p)
	return err == nil && i.IsDir()
}

// compareVersions compares dotted numbers such as 2024.1 and 2024.10.
func compareVersions(a, b string) int {
	x, y := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(x) || i < len(y); i++ {
		var p, q int
		if i < len(x) {
			p, _ = strconv.Atoi(x[i])
		}
		if i < len(y) {
			q, _ = strconv.Atoi(y[i])
		}
		if p != q {
			if p < q {
				return -1
			}
			return 1
		}
	}
	return 0
}

type jbOption struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

type jbShortcut struct {
	First  string `xml:"first-keystroke,attr"`
	Second string `xml:"second-keystroke,attr"`
}

type jbKeymap struct {
	Name    string `xml:"name,attr"`
	Parent  string `xml:"parent,attr"`
	Actions []struct {
		ID        string       `xml:"id,attr"`
		Shortcuts []jbShortcut `xml:"keyboard-shortcut"`
	} `xml:"action"`
}

// jetBrainsBuiltIn reports whether a keymap name is one JetBrains ships.
func jetBrainsBuiltIn(name string) bool {
	switch {
	case name == "$default", strings.HasPrefix(name, "Default for "), strings.HasPrefix(name, "Mac OS X"), strings.HasPrefix(name, "Eclipse"),
		strings.HasPrefix(name, "Visual Studio"), strings.HasPrefix(name, "Sublime Text"), strings.HasPrefix(name, "NetBeans"),
		name == "Emacs", name == "Xcode", name == "Default", name == "macOS", name == "GNOME", name == "KDE", name == "XWin":
		return true
	}
	return false
}

// ReadJetBrains reads one JetBrains config folder (keymap with its parents,
// editor settings, theme, plugins). It never writes and never leaves dir.
func ReadJetBrains(dir string) (Document, error) {
	d := Document{}
	skip := func(kind RowKind, source, reason string) {
		d.Skipped = append(d.Skipped, Skipped{kind, source, reason})
	}
	// xmlFile reads and decodes one XML file; a bad file becomes a skipped row.
	xmlFile := func(kind RowKind, rel string, dst any) bool {
		b, found, reason := readInside(dir, rel)
		if reason != "" {
			skip(kind, rel, reason)
		}
		if !found {
			return false
		}
		if err := xml.Unmarshal(b, dst); err != nil {
			skip(kind, rel, "not valid XML; skipped")
			return false
		}
		return true
	}

	// Keymaps.
	var active struct {
		Active struct {
			Name string `xml:"name,attr"`
		} `xml:"component>active_keymap"`
	}
	xmlFile(RowKey, "options/keymap.xml", &active)
	byName := map[string]jbKeymap{}
	var order []string
	files, links := listInside(dir, "keymaps")
	for _, l := range links {
		skip(RowKey, "keymaps/"+l, "is a link; skipped")
	}
	sort.Strings(files)
	for _, f := range files {
		if !strings.HasSuffix(f, ".xml") {
			continue
		}
		var km jbKeymap
		if xmlFile(RowKey, "keymaps/"+f, &km) {
			if km.Name == "" {
				km.Name = strings.TrimSuffix(f, ".xml")
			}
			byName[km.Name] = km
			order = append(order, km.Name)
		}
	}
	var chain []string // active keymap first, base last
	if name := active.Active.Name; name != "" {
		seen := map[string]bool{}
		for cur := name; cur != ""; {
			km, ok := byName[cur]
			if !ok {
				if jetBrainsBuiltIn(cur) {
					skip(RowKey, "base keymap "+cur, "is built in; only your changes are read, termocode keeps its own keys")
				} else {
					skip(RowKey, "base keymap "+cur, "was not found in the keymaps folder; its shortcuts are not imported")
				}
				break
			}
			if seen[cur] {
				skip(RowKey, "keymap "+cur, "has a parent loop; the loop was cut")
				break
			}
			seen[cur] = true
			chain = append(chain, cur)
			cur = km.Parent
		}
	} else {
		// No active keymap named: read every keymap file, in name order.
		for i := len(order) - 1; i >= 0; i-- {
			chain = append(chain, order[i])
		}
	}
	effective := map[string][]jbShortcut{}
	for i := len(chain) - 1; i >= 0; i-- { // base first, child last
		for _, a := range byName[chain[i]].Actions {
			if len(a.Shortcuts) == 0 {
				delete(effective, a.ID) // the child unbinds it
				continue
			}
			effective[a.ID] = a.Shortcuts
		}
	}
	ids := make([]string, 0, len(effective))
	for id := range effective {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		for _, s := range effective[id] {
			key := s.First
			if s.Second != "" {
				key += ", " + s.Second
			}
			d.Keys = append(d.Keys, KeyBinding{key, id})
		}
	}

	// Editor settings.
	var editor struct {
		Options []jbOption `xml:"component>option"`
	}
	if xmlFile(RowSetting, "options/editor.xml", &editor) {
		for _, o := range editor.Options {
			if o.Name == "USE_SOFT_WRAPS" {
				// A list of places; only the main editor counts.
				on := o.Value == "true" || strings.Contains(o.Value, "MAIN_EDITOR")
				d.Settings = append(d.Settings, Setting{o.Name, on})
				continue
			}
			d.Settings = append(d.Settings, Setting{o.Name, o.Value})
		}
	}
	var general struct {
		Options []jbOption `xml:"component>option"`
	}
	if xmlFile(RowSetting, "options/ide.general.xml", &general) {
		for _, o := range general.Options {
			switch o.Name {
			case "SAVE_FILES_ON_FRAME_DEACTIVATION", "saveOnFrameDeactivation", "autoSaveIfInactive":
				d.Settings = append(d.Settings, Setting{o.Name, o.Value})
			}
		}
	}
	var font struct {
		Options []jbOption `xml:"component>option"`
	}
	if xmlFile(RowSetting, "options/editor-font.xml", &font) {
		for _, o := range font.Options {
			if o.Name == "FONT_SIZE" || o.Name == "FONT_FAMILY" {
				d.Settings = append(d.Settings, Setting{o.Name, o.Value})
			}
		}
	}
	var style struct {
		Options []struct {
			Name  string `xml:"name,attr"`
			Value struct {
				Options []jbOption `xml:"option"`
			} `xml:"value"`
		} `xml:"option"`
		Languages []struct {
			Language string     `xml:"language,attr"`
			Options  []jbOption `xml:"indentOptions>option"`
		} `xml:"codeStyleSettings"`
	}
	if xmlFile(RowSetting, "codestyles/Default.xml", &style) {
		for _, o := range style.Options {
			if o.Name != "OTHER_INDENT_OPTIONS" {
				continue
			}
			for _, v := range o.Value.Options {
				if v.Name == "TAB_SIZE" {
					d.Settings = append(d.Settings, Setting{"TAB_SIZE", v.Value})
				}
			}
		}
		for _, l := range style.Languages {
			for _, v := range l.Options {
				if v.Name == "TAB_SIZE" && l.Language != "" {
					d.Settings = append(d.Settings, Setting{"TAB_SIZE (" + l.Language + ")", v.Value})
				}
			}
		}
	}

	// Theme: the editor colour scheme first, then the UI theme.
	var colors struct {
		Global struct {
			Name string `xml:"name,attr"`
		} `xml:"component>global_color_scheme"`
	}
	if xmlFile(RowTheme, "options/colors.scheme.xml", &colors) {
		d.Theme = strings.TrimPrefix(colors.Global.Name, "_@user_")
	}
	var laf struct {
		Laf struct {
			ThemeID   string `xml:"themeId,attr"`
			ClassName string `xml:"class-name,attr"`
		} `xml:"component>laf"`
	}
	if xmlFile(RowTheme, "options/laf.xml", &laf) && d.Theme == "" {
		switch {
		case laf.Laf.ThemeID != "":
			d.Theme = laf.Laf.ThemeID
		case laf.Laf.ClassName != "":
			name := laf.Laf.ClassName[strings.LastIndex(laf.Laf.ClassName, ".")+1:]
			name = strings.TrimSuffix(name, "Laf")
			if name == "IntelliJ" {
				name = "IntelliJ Light"
			}
			d.Theme = name
		}
	}

	// Plugins.
	disabled := map[string]bool{}
	if b, found, reason := readInside(dir, "disabled_plugins.txt"); found {
		for _, line := range strings.Split(string(b), "\n") {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
				disabled[line] = true
			}
		}
	} else if reason != "" {
		skip(RowExtension, "disabled_plugins.txt", reason)
	}
	names, plinks := listInside(dir, "plugins")
	for _, l := range plinks {
		skip(RowExtension, "plugins/"+l, "is a link; skipped")
	}
	sort.Strings(names)
	for _, n := range names {
		rel := "plugins/" + n
		if p, _ := resolveInside(dir, rel); p == "" || !isDir(p) {
			continue
		}
		id := pluginID(dir, rel)
		if id == "" {
			id = n
		}
		if disabled[id] {
			skip(RowExtension, id, "is turned off in the source editor; not imported")
			continue
		}
		d.Extensions = append(d.Extensions, id)
	}
	sort.Strings(d.Extensions)
	return d, nil
}

// pluginID returns the <id> of a plugin folder from META-INF/plugin.xml or a
// jar in lib/. It returns "" when none is found.
func pluginID(dir, rel string) string {
	read := func(b []byte) string {
		var x struct {
			ID   string `xml:"id"`
			Name string `xml:"name"`
		}
		if xml.Unmarshal(b, &x) != nil {
			return ""
		}
		return strings.TrimSpace(x.ID)
	}
	if b, found, _ := readInside(dir, rel+"/META-INF/plugin.xml"); found {
		if id := read(b); id != "" {
			return id
		}
	}
	jars, _ := listInside(dir, rel+"/lib")
	sort.Strings(jars)
	tried := 0
	for _, j := range jars {
		if !strings.HasSuffix(j, ".jar") || tried >= 8 {
			continue
		}
		tried++
		p, _ := resolveInside(dir, rel+"/lib/"+j)
		if p == "" {
			continue
		}
		if id := jarPluginID(p, read); id != "" {
			return id
		}
	}
	return ""
}

func jarPluginID(path string, read func([]byte) string) string {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return ""
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "META-INF/plugin.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return ""
		}
		defer rc.Close()
		b, err := io.ReadAll(io.LimitReader(rc, maxConfigFile))
		if err != nil {
			return ""
		}
		return read(b)
	}
	return ""
}
