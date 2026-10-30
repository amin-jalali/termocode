package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/evanw/esbuild/pkg/api"

	"github.com/amin-jalali/termocode/internal/webgw/assets/manifest"
)

// page is one HTML page and its bundle: src/<Entry> is bundled into
// <Name>.js (and <Name>.css when it imports CSS), src/<Template> becomes
// <HTML> with the SRI values filled in.
type page struct {
	Name     string
	Entry    string
	Template string
	HTML     string
	// CSSOnly: the entry only gathers stylesheets; only <Name>.css is kept.
	CSSOnly bool
	// With are more entries built in the same split build: the modules
	// they share with this one go to <Name>-chunk.js, loaded once, so a
	// module (Preact, the session, the strings) has one copy in the page.
	With []page
}

// workbenchChunk is the desktop workbench (workbench.js): its layout and
// the views only a wide screen shows (debug, testing, services, the
// commit graph, merge and rebase editors, pull requests, agent
// worktrees, the data grid, settings). app.js loads it with its second
// dynamic import (lib/lazy.ts) on a desktop only, so a phone never
// waits for it nor counts it in its budget. It shares app.js's modules
// through app-chunk.js, which index.html preloads with its SRI.
var workbenchChunk = page{Name: "workbench", Entry: "workbench.tsx"}

var appPage = page{Name: "app", Entry: "main.tsx", Template: "index.html", HTML: "index.html", With: []page{workbenchChunk}}

// editorChunk is the in-page editor (WEB-39): CodeMirror and its glue,
// built alone (no Preact) into editor.js. app.js loads it with its one
// dynamic import, only when the user starts editing.
var editorChunk = page{Name: "editor", Entry: "editor/main.ts"}

// monacoAMD and monacoFrame are the desktop editor (web workbench): the
// vendored editor runs in a frame of its own (monaco.html, with its own
// CSP); monaco-amd.js is the small module registry its plain scripts
// define themselves in, monaco-frame.js the glue that talks to the page.
var (
	monacoAMD   = page{Name: "monaco-amd", Entry: "monaco/amd.ts"}
	monacoFrame = page{Name: "monaco-frame", Entry: "monaco/frame.ts", Template: "monaco/frame.html", HTML: "monaco.html"}
)

// deskChunk is the desktop workbench's stylesheet (desk.css): index.html
// links it for wide screens only (media="(min-width: 1200px)"), so a
// phone neither waits for it nor counts it in its budget.
var deskChunk = page{Name: "desk", Entry: "desk.ts", CSSOnly: true}

// chunkPaths are the only dynamic imports a page may make.
var chunkPaths = map[string]bool{"/assets/" + editorChunk.Name + ".js": true, "/assets/" + workbenchChunk.Name + ".js": true}

// kitPage is the component kit (WEB-UX-1). It is always built, so CI keeps
// it fresh, but only a webdev build embeds and serves it.
var kitPage = page{Name: "kit", Entry: "kit/main.tsx", Template: "kit/kit.html", HTML: "kit.html"}

// Build bundles the sources under dir/src with the vendored libraries of
// dir/vendor and returns every output file by its name in dir, the lock
// included. Nothing is written. Bare imports resolve only to vendored
// files; anything else (a package that is not vendored, a URL) fails.
func Build(dir string) (map[string][]byte, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	vendorDir := filepath.Join(abs, "vendor")
	pkgs, err := manifest.VerifyVendor(vendorDir)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{"licenses.txt": licenses(vendorDir, pkgs)}
	if err := copyStatic(filepath.Join(abs, "src", "static"), out); err != nil {
		return nil, err
	}
	if err := copyFonts(filepath.Join(abs, "fonts"), out); err != nil {
		return nil, err
	}
	if err := copyServed(vendorDir, pkgs, out); err != nil {
		return nil, err
	}
	for _, p := range []page{deskChunk, appPage, editorChunk, monacoAMD, monacoFrame, kitPage} {
		if p.Name != appPage.Name {
			if _, err := os.Stat(filepath.Join(abs, "src", p.Entry)); errors.Is(err, os.ErrNotExist) {
				continue // a folder without the kit or the editor (test fixtures)
			}
		}
		if err := buildPage(abs, vendorDir, pkgs, p, out); err != nil {
			return nil, err
		}
	}
	out[manifest.LockFile] = []byte(manifest.FormatLock(out))
	return out, nil
}

func buildPage(abs, vendorDir string, pkgs []manifest.Package, p page, out map[string][]byte) error {
	entries := []api.EntryPoint{{InputPath: filepath.Join("src", p.Entry), OutputPath: p.Name}}
	var with []page
	for _, w := range p.With {
		if _, err := os.Stat(filepath.Join(abs, "src", w.Entry)); errors.Is(err, os.ErrNotExist) {
			continue // a folder without the workbench (test fixtures)
		}
		with = append(with, w)
		entries = append(entries, api.EntryPoint{InputPath: filepath.Join("src", w.Entry), OutputPath: w.Name})
	}
	p.With = with
	res := api.Build(api.BuildOptions{
		AbsWorkingDir:       abs,
		EntryPointsAdvanced: entries,
		Outdir:              filepath.Join(abs, "out"),
		Splitting:           len(p.With) > 0,
		ChunkNames:          p.Name + "-[name]", // one shared chunk: app-chunk.js
		Bundle:              true,
		Write:               false,
		Format:              api.FormatESModule,
		Platform:            api.PlatformBrowser,
		Target:              api.ES2020,
		MinifyWhitespace:    true,
		MinifyIdentifiers:   true,
		MinifySyntax:        true,
		TreeShaking:         api.TreeShakingTrue,
		Charset:             api.CharsetUTF8,
		LegalComments:       api.LegalCommentsNone,
		Sourcemap:           api.SourceMapNone,
		JSX:                 api.JSXTransform,
		JSXFactory:          "h",
		JSXFragment:         "Fragment",
		Banner:              map[string]string{"css": "/* termocode web UI */"},
		Metafile:            true,
		LogLevel:            api.LogLevelSilent,
		Plugins:             []api.Plugin{vendorOnly(vendorDir, pkgs)},
	})
	if len(res.Errors) > 0 {
		msgs := api.FormatMessages(res.Errors, api.FormatMessagesOptions{Kind: api.ErrorMessage})
		return errors.New(strings.Join(msgs, ""))
	}
	used, err := usedPackages(res.Metafile, vendorDir, pkgs)
	if err != nil {
		return err
	}
	for _, f := range res.OutputFiles {
		name := filepath.Base(f.Path)
		if _, dup := out[name]; dup {
			return fmt.Errorf("webbuild: two outputs named %s", name)
		}
		body := f.Contents
		if filepath.Ext(name) == ".js" {
			body = append([]byte(banner(used)+"\n"), body...)
		}
		out[name] = body
	}
	if p.CSSOnly {
		delete(out, p.Name+".js")
		if _, ok := out[p.Name+".css"]; !ok {
			return fmt.Errorf("webbuild: %s built no %s.css", p.Entry, p.Name)
		}
		return nil
	}
	if _, ok := out[p.Name+".js"]; !ok {
		return fmt.Errorf("webbuild: %s built no %s.js", p.Entry, p.Name)
	}
	for _, w := range p.With {
		if _, ok := out[w.Name+".js"]; !ok {
			return fmt.Errorf("webbuild: %s built no %s.js", w.Entry, w.Name)
		}
		if _, ok := out[w.Name+".css"]; ok {
			return fmt.Errorf("webbuild: %s imports a style sheet; desk.ts gathers the desktop's", w.Entry)
		}
	}
	if p.Template == "" {
		return nil // a chunk: no page of its own
	}
	tpl, err := os.ReadFile(filepath.Join(abs, "src", p.Template))
	if err != nil {
		return err
	}
	html, err := fillSRI(string(tpl), out)
	if err != nil {
		return fmt.Errorf("webbuild: %s: %w", p.Template, err)
	}
	out[p.HTML] = []byte(html)
	return nil
}

// copyServed copies the vendored files a package serves as they are
// (vendor.json "serve") to the output under their served names.
func copyServed(vendorDir string, pkgs []manifest.Package, out map[string][]byte) error {
	for _, p := range pkgs {
		for name, file := range p.Serve {
			b, err := os.ReadFile(filepath.Join(vendorDir, p.Dir, filepath.FromSlash(file)))
			if err != nil {
				return err
			}
			if _, dup := out[name]; dup {
				return fmt.Errorf("webbuild: two outputs named %s", name)
			}
			out[name] = b
		}
	}
	return nil
}

// copyStatic copies the files of src/static (icons, the app manifest) to
// the output as they are. Only SVG and the manifest are allowed; the lint
// test keeps the SVG free of scripts, and the manifest must be JSON that
// names no other site.
func copyStatic(dir string, out map[string][]byte) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		if e.IsDir() || (ext != ".svg" && e.Name() != "manifest.webmanifest") {
			return fmt.Errorf("webbuild: src/static/%s: only .svg files and manifest.webmanifest may be static", e.Name())
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		if ext == ".webmanifest" {
			if err := checkManifest(b); err != nil {
				return fmt.Errorf("webbuild: src/static/%s: %w", e.Name(), err)
			}
		}
		out[e.Name()] = b
	}
	return nil
}

// checkManifest accepts JSON only, with no URL that leaves the site.
func checkManifest(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	if regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://|"//`).Match(b) {
		return errors.New("names another site")
	}
	return nil
}

// fontsRecord is fonts/fonts.json: the symbol fonts of the terminal view
// (WEB-58) and the hash of each file.
type fontsRecord struct {
	Family string `json:"family"`
	Faces  []struct {
		File   string `json:"file"`
		SHA384 string `json:"sha384"`
	} `json:"faces"`
}

var fontFile = regexp.MustCompile(`^nf-[a-z0-9-]+\.woff2$`)

// copyFonts checks each font of dir/fonts.json against its hash and
// copies it to the output as fonts/<name>; dir/LICENSE.txt joins
// licenses.txt. A folder without fonts.json has no fonts.
func copyFonts(dir string, out map[string][]byte) error {
	b, err := os.ReadFile(filepath.Join(dir, "fonts.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var rec fontsRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		return fmt.Errorf("webbuild: fonts/fonts.json: %w", err)
	}
	for _, f := range rec.Faces {
		if !fontFile.MatchString(f.File) {
			return fmt.Errorf("webbuild: fonts/fonts.json: bad file name %q", f.File)
		}
		body, err := os.ReadFile(filepath.Join(dir, f.File))
		if err != nil {
			return err
		}
		if manifest.SRI(body) != f.SHA384 {
			return fmt.Errorf("webbuild: fonts/%s does not match its sha384 in fonts.json", f.File)
		}
		out["fonts/"+f.File] = body
	}
	lic, err := os.ReadFile(filepath.Join(dir, "LICENSE.txt"))
	if err != nil {
		return fmt.Errorf("webbuild: the symbol fonts need fonts/LICENSE.txt: %w", err)
	}
	out["licenses.txt"] = append(append(out["licenses.txt"], "\n"...), lic...)
	return nil
}

var sriMark = regexp.MustCompile(`\{\{sri ([a-z0-9][a-z0-9/._-]*)\}\}`)

// fillSRI replaces each {{sri name}} with the SRI value of the built file.
func fillSRI(tpl string, out map[string][]byte) (string, error) {
	var missing []string
	html := sriMark.ReplaceAllStringFunc(tpl, func(m string) string {
		name := sriMark.FindStringSubmatch(m)[1]
		body, ok := out[name]
		if !ok {
			missing = append(missing, name)
			return m
		}
		return manifest.SRI(body)
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("no built file for {{sri %s}}", strings.Join(missing, ", "))
	}
	if strings.Contains(html, "{{") {
		return "", errors.New("unknown template mark")
	}
	return html, nil
}

// ownAsset is a file the gateway serves next to the page, as a style
// sheet's url() names it (the symbol fonts).
var ownAsset = regexp.MustCompile(`^/assets/[a-z0-9][a-z0-9/._-]*$`)

// vendorOnly resolves bare imports to the vendored files and refuses any
// other import that is not a relative path inside the sources.
func vendorOnly(vendorDir string, pkgs []manifest.Package) api.Plugin {
	files := map[string]string{}
	for _, p := range pkgs {
		for spec, file := range p.Imports {
			files[spec] = filepath.Join(vendorDir, p.Dir, file)
		}
	}
	return api.Plugin{
		Name: "vendor-only",
		Setup: func(b api.PluginBuild) {
			b.OnResolve(api.OnResolveOptions{Filter: `.*`}, func(a api.OnResolveArgs) (api.OnResolveResult, error) {
				if strings.HasPrefix(a.Path, "./") || strings.HasPrefix(a.Path, "../") {
					return api.OnResolveResult{}, nil // esbuild resolves it
				}
				if a.Kind == api.ResolveEntryPoint {
					return api.OnResolveResult{}, nil
				}
				if a.Kind == api.ResolveCSSURLToken && ownAsset.MatchString(a.Path) {
					return api.OnResolveResult{Path: a.Path, External: true}, nil // served next to the page
				}
				if a.Kind == api.ResolveJSDynamicImport && chunkPaths[a.Path] {
					return api.OnResolveResult{Path: a.Path, External: true}, nil // a chunk built beside
				}
				if f, ok := files[a.Path]; ok {
					return api.OnResolveResult{Path: f}, nil
				}
				return api.OnResolveResult{}, fmt.Errorf("import %q is not vendored (see vendor/vendor.json)", a.Path)
			})
		},
	}
}

// usedPackages lists the vendored packages a build bundled, from its
// metafile, so each file's banner names only what it holds.
func usedPackages(meta, vendorDir string, pkgs []manifest.Package) ([]manifest.Package, error) {
	var m struct {
		Inputs map[string]json.RawMessage `json:"inputs"`
	}
	if err := json.Unmarshal([]byte(meta), &m); err != nil {
		return nil, fmt.Errorf("webbuild: metafile: %w", err)
	}
	var used []manifest.Package
	for _, p := range pkgs {
		dir := filepath.ToSlash(filepath.Join(filepath.Base(vendorDir), p.Dir)) + "/"
		for in := range m.Inputs {
			if strings.HasPrefix(in, dir) {
				used = append(used, p)
				break
			}
		}
	}
	return used, nil
}

func banner(pkgs []manifest.Package) string {
	parts := make([]string, 0, len(pkgs))
	for _, p := range pkgs {
		parts = append(parts, p.Name+" "+p.Version)
	}
	sort.Strings(parts)
	return "/* termocode web UI. Includes " + strings.Join(parts, ", ") + " (MIT); see /assets/licenses.txt */"
}

// licenses joins the vendored LICENSE files, one section per package.
func licenses(vendorDir string, pkgs []manifest.Package) []byte {
	var b strings.Builder
	b.WriteString("Third-party software in the termocode web UI\n")
	for _, p := range pkgs {
		text, _ := os.ReadFile(filepath.Join(vendorDir, p.Dir, "LICENSE")) // checked by VerifyVendor
		fmt.Fprintf(&b, "\n==== %s %s (%s) ====\n%s\n\n%s", p.Name, p.Version, p.License, p.Tarball, text)
		if !strings.HasSuffix(string(text), "\n") {
			b.WriteString("\n")
		}
		// The code licence of a package whose files are under another one.
		if _, ok := p.Files["LICENSE-CODE"]; ok {
			code, _ := os.ReadFile(filepath.Join(vendorDir, p.Dir, "LICENSE-CODE"))
			fmt.Fprintf(&b, "\n---- code licence of %s ----\n%s", p.Name, code)
			if !strings.HasSuffix(string(code), "\n") {
				b.WriteString("\n")
			}
		}
		// A package that bundles others ships their notices too.
		if _, ok := p.Files["ThirdPartyNotices.txt"]; ok {
			notes, _ := os.ReadFile(filepath.Join(vendorDir, p.Dir, "ThirdPartyNotices.txt"))
			fmt.Fprintf(&b, "\n---- third-party notices of %s ----\n%s", p.Name, notes)
			if !strings.HasSuffix(string(notes), "\n") {
				b.WriteString("\n")
			}
		}
	}
	return []byte(b.String())
}
