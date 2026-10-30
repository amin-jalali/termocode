// Package manifest reads and checks the two manifests of the web assets:
// vendor/vendor.json (the browser libraries we copy, pinned by checksum)
// and assets.lock (the files cmd/webbuild wrote, with their SRI hashes).
// It has no embedded files, so the build tool can use it before the
// assets exist.
package manifest

import (
	"bytes"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

// VendorFile is the name of the vendor manifest inside the vendor folder.
const VendorFile = "vendor.json"

// Package is one vendored npm package: exact version, where it came from,
// the bare import names it answers and the sha384 of each copied file.
type Package struct {
	Name      string            `json:"name"`
	Version   string            `json:"version"`
	Dir       string            `json:"dir"`
	Tarball   string            `json:"tarball"`
	Integrity string            `json:"integrity"`
	License   string            `json:"license"`
	Imports   map[string]string `json:"imports"`
	// Serve lists files the gateway serves as they are (not bundled), by
	// their name under /assets/ ("monaco/editor.js"): a library that is
	// loaded as plain scripts, such as the desktop editor.
	Serve map[string]string `json:"serve,omitempty"`
	Files map[string]string `json:"files"`
}

type vendorDoc struct {
	Comment  string    `json:"$comment"`
	Packages []Package `json:"packages"`
}

// SRI returns the Subresource Integrity value of b: "sha384-" + base64.
func SRI(b []byte) string {
	sum := sha512.Sum384(b)
	return "sha384-" + base64.StdEncoding.EncodeToString(sum[:])
}

// LoadVendor reads dir/vendor.json strictly (unknown fields are an error).
func LoadVendor(dir string) ([]Package, error) {
	b, err := os.ReadFile(filepath.Join(dir, VendorFile))
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var doc vendorDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%s: %w", VendorFile, err)
	}
	if len(doc.Packages) == 0 {
		return nil, fmt.Errorf("%s: no packages", VendorFile)
	}
	return doc.Packages, nil
}

// VerifyVendor loads the manifest and checks the folder against it: every
// listed file has its checksum, no package folder holds an unlisted file,
// every package carries its LICENSE, and every import names a listed file.
func VerifyVendor(dir string) ([]Package, error) {
	pkgs, err := LoadVendor(dir)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, p := range pkgs {
		if p.Name == "" || p.Dir == "" || filepath.Base(p.Dir) != p.Dir {
			return nil, fmt.Errorf("vendor: bad package row %q dir %q", p.Name, p.Dir)
		}
		if _, ok := p.Files["LICENSE"]; !ok {
			return nil, fmt.Errorf("vendor: %s has no LICENSE", p.Name)
		}
		for spec, file := range p.Imports {
			if _, ok := p.Files[file]; !ok {
				return nil, fmt.Errorf("vendor: %s import %q names unlisted file %q", p.Name, spec, file)
			}
			if seen["import:"+spec] {
				return nil, fmt.Errorf("vendor: import %q listed twice", spec)
			}
			seen["import:"+spec] = true
		}
		for name, file := range p.Serve {
			if _, ok := p.Files[file]; !ok {
				return nil, fmt.Errorf("vendor: %s serves %q from unlisted file %q", p.Name, name, file)
			}
			if !servedName.MatchString(name) || seen["serve:"+name] {
				return nil, fmt.Errorf("vendor: %s serves a bad or repeated name %q", p.Name, name)
			}
			seen["serve:"+name] = true
		}
		for _, file := range sortedKeys(p.Files) {
			b, err := os.ReadFile(filepath.Join(dir, p.Dir, filepath.FromSlash(file)))
			if err != nil {
				return nil, fmt.Errorf("vendor: %s/%s: %w", p.Dir, file, err)
			}
			if got := SRI(b); got != p.Files[file] {
				return nil, fmt.Errorf("vendor: %s/%s has %s, want %s", p.Dir, file, got, p.Files[file])
			}
		}
		var onDisk []string
		err := filepath.WalkDir(filepath.Join(dir, p.Dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(filepath.Join(dir, p.Dir), path)
			onDisk = append(onDisk, filepath.ToSlash(rel))
			return nil
		})
		if err != nil {
			return nil, err
		}
		for _, name := range onDisk {
			if _, ok := p.Files[name]; !ok {
				return nil, fmt.Errorf("vendor: %s/%s is not listed in %s", p.Dir, name, VendorFile)
			}
		}
		seen["dir:"+p.Dir] = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Name() != VendorFile && !seen["dir:"+e.Name()] {
			return nil, fmt.Errorf("vendor: %s is not listed in %s", e.Name(), VendorFile)
		}
	}
	return pkgs, nil
}

// servedName is a file name under /assets/: a folder, then a plain name.
var servedName = regexp.MustCompile(`^[a-z0-9-]+/[A-Za-z0-9._-]+\.(js|css)$`)

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
