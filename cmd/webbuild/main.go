// Command webbuild builds the browser assets of the web gateway with the
// esbuild Go API: no node, no npm, the same bytes on every machine. It is
// run by `go generate ./internal/webgw/assets` and writes app.js, app.css,
// index.html, licenses.txt and assets.lock (path, sha384, size) into the
// assets folder. CI runs it again and fails on any diff.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func main() {
	dir := flag.String("dir", ".", "the assets folder (holds src/ and vendor/)")
	check := flag.Bool("check", false, "only report files that differ from a fresh build")
	flag.Parse()
	if err := run(*dir, *check); err != nil {
		fmt.Fprintln(os.Stderr, "webbuild:", err)
		os.Exit(1)
	}
}

func run(dir string, check bool) error {
	out, err := Build(dir)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(out))
	for n := range out {
		names = append(names, n)
	}
	sort.Strings(names)
	stale := 0
	for _, n := range names {
		path := filepath.Join(dir, n)
		old, _ := os.ReadFile(path)
		if bytes.Equal(old, out[n]) {
			continue
		}
		if check {
			fmt.Fprintln(os.Stderr, "stale:", n)
			stale++
			continue
		}
		if err := os.WriteFile(path, out[n], 0o644); err != nil {
			return err
		}
	}
	if stale > 0 {
		return fmt.Errorf("%d file(s) stale; run go generate ./internal/webgw/assets", stale)
	}
	return nil
}
