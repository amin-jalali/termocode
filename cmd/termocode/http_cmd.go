package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// `termocode http run` (API-9): run the requests of .http / .rest files
// without the UI, for CI.

const httpUsage = `termocode http: run .http files without the UI

  termocode http run [options] <file|folder>…
        send every request in order and print a report
        (exit 0 = all passed, 1 = a request failed, 2 = usage error)

Options:
  --env NAME          the environment from http-client.env.json
  --iterations N      run the whole set N times (default 1, or once per data row)
  --data FILE         a .csv or .json data file; each iteration uses one row
  --delay DURATION    wait between two requests (e.g. 200ms)
  --rate N            at most N requests per second (e.g. 2 or 0.5)
  --stop-on-fail      stop at the first failed request
  --json FILE         also write the report as JSON
  --junit FILE        also write the report as JUnit XML
  --timeout DURATION  the timeout per request (default 30s)

A request passes when it was sent and answered with a status below 400.
Folders are searched for .http and .rest files. Placeholders that stay
unknown fail the request without sending it.
`

func runHTTP(args []string) int {
	root, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "termocode http: no current folder: %v\n", err)
		return 1
	}
	return runHTTPIn(args, os.Stdout, os.Stderr, root)
}

func runHTTPIn(args []string, out, errw io.Writer, root string) int {
	if len(args) == 0 {
		fmt.Fprint(errw, httpUsage)
		return 2
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(out, httpUsage)
		return 0
	case "run":
	default:
		fmt.Fprintf(errw, "termocode http: unknown command %q\n\n%s", args[0], httpUsage)
		return 2
	}
	fs := flag.NewFlagSet("termocode http run", flag.ContinueOnError)
	fs.SetOutput(errw)
	fs.Usage = func() { fmt.Fprint(errw, httpUsage) }
	env := fs.String("env", "", "")
	iters := fs.Int("iterations", 0, "")
	dataPath := fs.String("data", "", "")
	delay := fs.Duration("delay", 0, "")
	rate := fs.Float64("rate", 0, "")
	stop := fs.Bool("stop-on-fail", false, "")
	jsonPath := fs.String("json", "", "")
	junitPath := fs.String("junit", "", "")
	timeout := fs.Duration("timeout", 0, "")
	if err := fs.Parse(httpReorderFlags(args[1:])); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if fs.NArg() == 0 || *iters < 0 || *delay < 0 || *rate < 0 {
		fmt.Fprint(errw, "termocode http run: give at least one .http file or folder\n\n"+httpUsage)
		return 2
	}
	fail := func(f string, a ...any) int {
		fmt.Fprintf(errw, "termocode http run: "+f+"\n", a...)
		return 1
	}
	paths, err := httpclient.FindHTTPFiles(fs.Args())
	if err != nil {
		return fail("%v", err)
	}
	var files []httpclient.RunFile
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			return fail("%v", err)
		}
		files = append(files, httpclient.RunFile{Path: displayPath(root, p), File: httpclient.Parse(string(src))})
	}
	opts := httpclient.RunOptions{Iterations: *iters, Delay: *delay, Rate: *rate, StopOnFail: *stop}
	if *dataPath != "" {
		if opts.Data, err = httpclient.LoadRunData(*dataPath); err != nil {
			return fail("%v", err)
		}
	}
	abs := map[string]string{}
	for i, p := range paths {
		a, err := filepath.Abs(p)
		if err != nil {
			a = p
		}
		abs[files[i].Path] = a
	}
	opts.NewResolver = func(rf httpclient.RunFile) *httpclient.Resolver {
		return &httpclient.Resolver{FileVars: rf.File.Vars, Env: *env, Envs: httpclient.LoadEnvs(filepath.Dir(abs[rf.Path]), root)}
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	c := httpclient.New(httpclient.Options{Timeout: *timeout, CookieJar: true, UserAgent: "termocode/" + versionString()})
	defer c.Close()
	rep := httpclient.Run(ctx, c, files, opts)
	fmt.Fprint(out, rep.Text())
	if *jsonPath != "" {
		b, err := rep.JSON()
		if err == nil {
			err = os.WriteFile(*jsonPath, append(b, '\n'), 0o644)
		}
		if err != nil {
			return fail("json report: %v", err)
		}
	}
	if *junitPath != "" {
		b, err := rep.JUnit()
		if err == nil {
			err = os.WriteFile(*junitPath, b, 0o644)
		}
		if err != nil {
			return fail("junit report: %v", err)
		}
	}
	if rep.Failed > 0 || rep.Stopped {
		return 1
	}
	return 0
}

// httpReorderFlags moves the flags before the paths, so
// `run a.http --env dev` works like `run --env dev a.http`.
func httpReorderFlags(args []string) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			rest = append(rest, a)
			continue
		}
		flags = append(flags, a)
		name := strings.TrimLeft(a, "-")
		if !strings.Contains(name, "=") && name != "stop-on-fail" && name != "h" && name != "help" && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, rest...)
}

func displayPath(root, p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	if rel, err := filepath.Rel(root, a); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return p
}
