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
	"os"
	"path/filepath"
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
	Files     map[string]string `json:"files"`
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
		for _, file := range sortedKeys(p.Files) {
			b, err := os.ReadFile(filepath.Join(dir, p.Dir, file))
			if err != nil {
				return nil, fmt.Errorf("vendor: %s/%s: %w", p.Dir, file, err)
			}
			if got := SRI(b); got != p.Files[file] {
				return nil, fmt.Errorf("vendor: %s/%s has %s, want %s", p.Dir, file, got, p.Files[file])
			}
		}
		entries, err := os.ReadDir(filepath.Join(dir, p.Dir))
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if _, ok := p.Files[e.Name()]; !ok {
				return nil, fmt.Errorf("vendor: %s/%s is not listed in %s", p.Dir, e.Name(), VendorFile)
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

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
