package main

import (
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
}

var appPage = page{Name: "app", Entry: "main.tsx", Template: "index.html", HTML: "index.html"}

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
	for _, p := range []page{appPage} {
		if err := buildPage(abs, vendorDir, pkgs, p, out); err != nil {
			return nil, err
		}
	}
	out[manifest.LockFile] = []byte(manifest.FormatLock(out))
	return out, nil
}

func buildPage(abs, vendorDir string, pkgs []manifest.Package, p page, out map[string][]byte) error {
	res := api.Build(api.BuildOptions{
		AbsWorkingDir:       abs,
		EntryPointsAdvanced: []api.EntryPoint{{InputPath: filepath.Join("src", p.Entry), OutputPath: p.Name}},
		Outdir:              filepath.Join(abs, "out"),
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
		Banner:              map[string]string{"js": banner(pkgs), "css": "/* termocode web UI */"},
		LogLevel:            api.LogLevelSilent,
		Plugins:             []api.Plugin{vendorOnly(vendorDir, pkgs)},
	})
	if len(res.Errors) > 0 {
		msgs := api.FormatMessages(res.Errors, api.FormatMessagesOptions{Kind: api.ErrorMessage})
		return errors.New(strings.Join(msgs, ""))
	}
	for _, f := range res.OutputFiles {
		name := filepath.Base(f.Path)
		if _, dup := out[name]; dup {
			return fmt.Errorf("webbuild: two outputs named %s", name)
		}
		out[name] = f.Contents
	}
	if _, ok := out[p.Name+".js"]; !ok {
		return fmt.Errorf("webbuild: %s built no %s.js", p.Entry, p.Name)
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

// copyStatic copies the files of src/static (icons) to the output as they
// are. Only SVG is allowed, and the lint test keeps it free of scripts.
func copyStatic(dir string, out map[string][]byte) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".svg" {
			return fmt.Errorf("webbuild: src/static/%s: only .svg files may be static", e.Name())
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		out[e.Name()] = b
	}
	return nil
}

var sriMark = regexp.MustCompile(`\{\{sri ([a-z0-9._-]+)\}\}`)

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
				if f, ok := files[a.Path]; ok {
					return api.OnResolveResult{Path: f}, nil
				}
				return api.OnResolveResult{}, fmt.Errorf("import %q is not vendored (see vendor/vendor.json)", a.Path)
			})
		},
	}
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
	}
	return []byte(b.String())
}
