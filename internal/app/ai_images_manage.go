package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services"
)

// images_manage: every image action of the Containers view (the image
// row, its menu and the palette's Containers: … Image rows) on any host.
//
// It asks where the GUI asks: push, run, remove, build and load are the
// GUI's cards and ask every time with no "Always allow"; tag, save and
// import ask with the containers class (the GUI's prompts; a tag that
// moves from another image and a save over a file are the GUI's cards
// and ask every time); history, used_by, scan, sbom, search, tags,
// check_updates, builds, build_detail and build_check read at once, as
// the GUI shows them at once. Push, run and build run in a terminal tab
// (the GUI's tab); the others are captured. An image is named by what a
// fresh listing of its host gives; ENV values of a history never cross.

var aiImageOps = []string{"push", "tag", "run", "remove", "build", "history", "used_by", "save", "load", "import",
	"scan", "sbom", "search", "tags", "check_updates", "builds", "build_detail", "build_check"}

// aiImageCards are the ops the GUI asks on a card every time.
var aiImageCards = map[string]bool{"push": true, "run": true, "remove": true, "build": true, "load": true}

// aiImageReads run at once.
var aiImageReads = map[string]bool{"history": true, "used_by": true, "scan": true, "sbom": true, "search": true, "tags": true,
	"check_updates": true, "builds": true, "build_detail": true, "build_check": true}

// aiImageScanWait bounds a scan or an SBOM (the scanner may download its database).
const aiImageScanWait = 15 * time.Minute

// aiImageReq is one checked images_manage call.
type aiImageReq struct {
	op      string
	h       aiContainersHost
	ref     string
	im      containers.Image
	list    []containers.Image
	cmd     services.Command // push, run, build, build_detail, build_check: a terminal tab
	argv    []string         // tag, remove, save, load, import: captured
	file    string           // save, load, import: the absolute workspace file
	replace bool             // tag moves from another image; save over a file
}

func aiImageFind(list []containers.Image, ref string) (containers.Image, bool) {
	id := strings.TrimPrefix(ref, "sha256:")
	for _, im := range list {
		if len(id) >= 4 && isHex(id) && strings.HasPrefix(strings.TrimPrefix(im.ID, "sha256:"), id) {
			return im, true
		}
		for _, t := range im.RepoTags {
			if t == ref || (!strings.Contains(ref, ":") && t == ref+":latest") {
				return im, true
			}
		}
	}
	return containers.Image{}, false
}

func aiImageTags(list []containers.Image) string {
	var tags []string
	for _, im := range list {
		for _, t := range im.RepoTags {
			if t != "<none>:<none>" {
				tags = append(tags, t)
			}
		}
	}
	if len(tags) > 20 {
		tags = append(tags[:20], "…")
	}
	if len(tags) == 0 {
		return "none"
	}
	return strings.Join(tags, ", ")
}

func aiImageParse(ctx context.Context, call aiIDECaller, a map[string]any) (aiImageReq, error) {
	r := aiImageReq{op: ai.ArgString(a, "op"), ref: strings.TrimSpace(ai.ArgString(a, "ref"))}
	if !contains(aiImageOps, r.op) {
		return r, errors.New("give op: " + strings.Join(aiImageOps, ", "))
	}
	if err := aiValidContextArg(a); err != nil {
		return r, err
	}
	needsRef := map[string]bool{"push": true, "tag": true, "run": true, "remove": true, "history": true, "used_by": true,
		"save": true, "scan": true, "sbom": true, "tags": true, "build_detail": true}[r.op]
	if needsRef {
		if err := aiValidInspectTarget("image", r.ref); err != nil {
			return r, fmt.Errorf("ref: %v", err)
		}
	}
	h, err := aiContainersHostAt(ctx, call, a)
	r.h = h
	if err != nil {
		return r, err
	}
	root := aiCtrRoot(h)
	listed := map[string]bool{"push": true, "tag": true, "run": true, "remove": true, "history": true, "used_by": true, "save": true}[r.op]
	if listed {
		if r.list, err = h.e.ListImages(ctx); err != nil {
			return r, errors.New(aiContainersRedact(err.Error()))
		}
		var ok bool
		if r.im, ok = aiImageFind(r.list, r.ref); !ok {
			return r, fmt.Errorf("no image %q on this host; images: %s (see images_list)", r.ref, aiImageTags(r.list))
		}
	}
	file := func(must bool) error {
		rel := strings.TrimSpace(ai.ArgString(a, "file"))
		if r.file, err = aiDBInside(root, rel); err != nil {
			return fmt.Errorf("file: %v", err)
		}
		if _, err := os.Stat(r.file); err == nil {
			r.replace = true
		} else if must {
			return fmt.Errorf("file: %s is not in the workspace", rel)
		}
		return nil
	}
	switch r.op {
	case "push":
		if !strings.Contains(r.ref, ":") || isHex(strings.TrimPrefix(r.ref, "sha256:")) {
			return r, errors.New("push needs a tagged image (repo:tag)")
		}
		r.cmd = h.e.PushCommand(r.ref)
	case "tag":
		tag := strings.TrimSpace(ai.ArgString(a, "tag"))
		if err := containers.ValidImageRef(tag); err != nil {
			return r, fmt.Errorf("tag: %v", err)
		}
		if other, ok := aiImageFind(r.list, tag); ok {
			if other.ID == r.im.ID {
				return r, fmt.Errorf("%s already has the tag %s (no change)", r.ref, tag)
			}
			r.replace = true // the GUI's "Replace image tag?" card
		}
		r.argv = []string{"tag", r.ref, tag}
	case "run":
		o := containers.RunOptions{Image: r.ref, Name: strings.TrimSpace(ai.ArgString(a, "name")), Remove: !ai.ArgBool(a, "keep"),
			Detach: ai.ArgBool(a, "detach"), Network: strings.TrimSpace(ai.ArgString(a, "network")), Restart: strings.TrimSpace(ai.ArgString(a, "restart"))}
		if o.Ports, err = aiContainersArgList(a, "ports"); err != nil {
			return r, err
		}
		if o.Volumes, err = aiContainersArgList(a, "volumes"); err != nil {
			return r, err
		}
		if o.Command, err = aiContainersArgList(a, "command"); err != nil {
			return r, err
		}
		env, err := aiContainersArgList(a, "env")
		if err != nil {
			return r, err
		}
		for _, kv := range env {
			k, v, ok := strings.Cut(kv, "=")
			if !ok || k == "" {
				return r, fmt.Errorf("env %q: write NAME=value", kv)
			}
			if o.Env == nil {
				o.Env = map[string]string{}
			}
			o.Env[k] = v
		}
		if r.cmd, err = h.e.RunOptionsCommand(o); err != nil {
			return r, err
		}
	case "remove":
		if users := containers.ImageUsedBy(r.im, aiContainersOn(ctx, h)); len(users) > 0 {
			var names []string
			for _, c := range users {
				names = append(names, aiContainerName(c))
			}
			return r, fmt.Errorf("the image stays while containers use it: %s (remove them first)", strings.Join(names, ", "))
		}
		r.argv = []string{"rmi", r.ref}
	case "build":
		rel := strings.TrimSpace(ai.ArgString(a, "file"))
		if rel == "" {
			rel = "Dockerfile"
		}
		df, err := aiDBInside(root, rel)
		if err != nil {
			return r, fmt.Errorf("file: %v", err)
		}
		if fi, err := os.Stat(df); err != nil || fi.IsDir() {
			return r, fmt.Errorf("file: no Dockerfile %s in the workspace", rel)
		}
		o := containers.BuildOptions{Dockerfile: df, Tag: strings.TrimSpace(ai.ArgString(a, "tag")), Target: strings.TrimSpace(ai.ArgString(a, "target"))}
		if dir := strings.TrimSpace(ai.ArgString(a, "dir")); dir != "" {
			if o.Context, err = aiDBInside(root, dir); err != nil {
				return r, fmt.Errorf("dir: %v", err)
			}
		}
		if o.Tag != "" {
			if err := containers.ValidImageRef(o.Tag); err != nil {
				return r, fmt.Errorf("tag: %v", err)
			}
		}
		if o.Args, err = aiContainersArgList(a, "build_args"); err != nil {
			return r, err
		}
		for _, v := range append([]string{o.Target}, o.Args...) {
			if strings.HasPrefix(v, "-") || strings.ContainsAny(v, "\n\x00") {
				return r, fmt.Errorf("%q is not valid", v)
			}
		}
		r.cmd = h.e.BuildCommand(o)
	case "save":
		if err := file(false); err != nil {
			return r, err
		}
		if r.argv, err = containers.SaveArgs(r.file, r.ref); err != nil {
			return r, err
		}
	case "load":
		if err := file(true); err != nil {
			return r, err
		}
		r.argv, _ = containers.LoadArgs(r.file)
	case "import":
		if err := file(true); err != nil {
			return r, err
		}
		if r.ref != "" {
			if err := containers.ValidImageRef(r.ref); err != nil {
				return r, fmt.Errorf("ref: %v", err)
			}
		}
		if r.argv, err = containers.ImportArgs(r.file, r.ref); err != nil {
			return r, err
		}
	case "search":
		if err := containers.ValidSearchTerm(strings.TrimSpace(ai.ArgString(a, "term"))); err != nil {
			return r, err
		}
	case "tags":
		if _, err := containers.ParseRemoteRepo(r.ref); err != nil {
			return r, err
		}
	case "build_detail":
		if r.cmd, err = h.e.BuildDetailCommand(r.ref); err != nil {
			return r, err
		}
	case "build_check":
		rel := strings.TrimSpace(ai.ArgString(a, "file"))
		dir := root
		if rel != "" {
			p, err := aiDBInside(root, rel)
			if err != nil {
				return r, fmt.Errorf("file: %v", err)
			}
			dir, rel = filepath.Dir(p), filepath.Base(p)
		}
		r.cmd = h.e.BuildCheckCommand(dir, rel)
	}
	if r.cmd.Argv != nil {
		r.cmd = h.termCommand(r.cmd)
	}
	return r, nil
}

// aiContainersOn is h's containers (nil when the list fails).
func aiContainersOn(ctx context.Context, h aiContainersHost) []containers.Container {
	list, _ := h.e.ListContainers(ctx)
	return list
}

func aiImageDescribe(call aiIDECaller, a map[string]any) string {
	op, ref := ai.ArgString(a, "op"), strings.TrimSpace(ai.ArgString(a, "ref"))
	ctx, cancel := aiContainersDescribeCtx()
	defer cancel()
	r, err := aiImageParse(ctx, call, a)
	if err != nil {
		return aiTitle(strings.ReplaceAll(op+" ", "_", " ")) + "image " + ref + " (refused: " + aiOneLineClip(err.Error(), 120) + ")"
	}
	where := r.h.contextLabel(ctx)
	line := func(argv []string) string { return svcShellLine(argv) }
	switch r.op {
	case "push":
		return fmt.Sprintf("Push image %s (%s) in a terminal tab · %s · it leaves this machine for its registry", r.ref, line(r.cmd.Argv), where)
	case "run":
		return fmt.Sprintf("Run image %s (%s) in a terminal tab · %s · a new container starts", r.ref, line(r.cmd.Argv), where)
	case "build":
		return fmt.Sprintf("Build image (%s) in a terminal tab · %s", line(r.cmd.Argv), where)
	case "remove":
		return fmt.Sprintf("Remove image %s (%s rmi %s) · %s · you can pull it again", r.ref, r.h.bin(), r.ref, where)
	case "tag":
		s := fmt.Sprintf("Tag image %s (%s) · %s", r.ref, line(append([]string{r.h.bin()}, r.argv...)), where)
		if r.replace {
			s += " · this moves the existing tag " + r.argv[2] + " to this image"
		}
		return s
	case "save", "load", "import":
		s := fmt.Sprintf("%s (%s) · %s", aiTitle(r.op)+" images", line(append([]string{r.h.bin()}, r.argv...)), where)
		if r.op == "save" && r.replace {
			s += " · " + filepath.Base(r.file) + " exists; saving replaces it"
		}
		if r.op == "load" {
			s += " · an image with the same tag is replaced"
		}
		return s
	}
	return aiTitle(strings.ReplaceAll(r.op, "_", " ")) + " " + ref
}

func aiImageRun(ctx context.Context, call aiIDECaller, a map[string]any) (string, error) {
	r, err := aiImageParse(ctx, call, a)
	if err != nil {
		return "", err
	}
	h, where := r.h, r.h.contextLabel(ctx)
	switch r.op {
	case "push", "run", "build", "build_detail", "build_check":
		key := containerTabPrefix + r.op + ":" + h.tabKey(r.ref+r.cmd.Label)
		return call(ctx, func(m *Model) (string, tea.Cmd, error) {
			if _, running := m.svcTabRunning(key); running && r.op != "run" {
				m.servicesFocusTab(key)
				return "already running in its tab \"" + r.cmd.Label + "\" (no change)", nil, nil
			}
			if err := aiCtrTerminal(m, r.cmd, key); err != nil {
				return "", nil, fmt.Errorf("cannot open the %s tab: %v", r.op, err)
			}
			return fmt.Sprintf("started `%s` in the terminal tab %q · %s; terminal_read reads it", svcShellLine(r.cmd.Argv), r.cmd.Label, where), nil, nil
		})
	case "tag", "remove", "save", "load", "import":
		wait := aiContainersCallWait
		if r.op != "tag" && r.op != "remove" {
			wait = archiveJobTimeout
			if r.op == "save" {
				if err := os.MkdirAll(filepath.Dir(r.file), 0o755); err != nil {
					return "", err
				}
			}
		}
		out, err := aiEngineRun(ctx, h, wait, r.argv...)
		_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) { return "", m.containersRefresh("images"), nil })
		if err != nil {
			return "", fmt.Errorf("%s failed: %v", r.op, err)
		}
		res := fmt.Sprintf("ran `%s` · %s", svcShellLine(append([]string{h.bin()}, r.argv...)), where)
		if l := sanitizeOutputLine(errLine(lastLine(out))); l != "" && r.op != "save" {
			res += ": " + l
		}
		return aiContainersRedact(res), nil
	case "history":
		hist, err := h.e.History(ctx, r.ref)
		if err != nil {
			return "", errors.New(aiContainersRedact(err.Error()))
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s\nHistory of %s: %d layers (ENV values hidden)\n", h.header(ctx), r.ref, len(hist))
		for _, e := range hist {
			fmt.Fprintf(&b, "- %s · %s · %s\n", aiShortID(e.ID), e.Size, aiOneLineClip(webMaskEnvStep(e.CreatedBy), 300))
		}
		return aiContainersRedact(strings.TrimRight(b.String(), "\n")), nil
	case "used_by":
		users := containers.ImageUsedBy(r.im, aiContainersOn(ctx, h))
		var b strings.Builder
		fmt.Fprintf(&b, "%s\nContainers using %s: %d\n", h.header(ctx), r.ref, len(users))
		for _, c := range users {
			b.WriteString(aiContainerLine(c) + "\n")
		}
		return aiContainersRedact(strings.TrimRight(b.String(), "\n")), nil
	case "search":
		res, err := h.e.Search(ctx, strings.TrimSpace(ai.ArgString(a, "term")), containers.DefaultSearchLimit)
		if err != nil {
			return "", errors.New(aiContainersRedact(err.Error()))
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Docker Hub: %d results (images_pull pulls one)\n", len(res))
		for _, s := range res {
			official := ""
			if s.Official {
				official = " · official"
			}
			fmt.Fprintf(&b, "- %s · %d stars%s · %s\n", s.Name, s.Stars, official, s.Description)
		}
		return strings.TrimRight(b.String(), "\n"), nil
	case "tags":
		return aiImageRemoteTags(ctx, call, r.ref)
	case "check_updates":
		return aiImageUpdates(ctx, call, h)
	case "builds":
		recs, err := h.e.BuildHistory(ctx)
		if err != nil {
			return "", errors.New(aiContainersRedact(err.Error()))
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s\nBuilds: %d\n", h.header(ctx), len(recs))
		for i, rec := range recs {
			if i == aiContainersListLimit {
				break
			}
			fmt.Fprintf(&b, "- %s · %s · %s · %d/%d steps (%d cached) · %s", rec.Ref, rec.Name, rec.Status, rec.Done, rec.Steps, rec.Cached, rec.Duration)
			if rec.Error != "" {
				b.WriteString(" · error: " + aiOneLineClip(rec.Error, 160))
			}
			b.WriteString("\n")
		}
		return aiContainersRedact(strings.TrimRight(b.String(), "\n")), nil
	}
	return aiImageScanRun(ctx, call, h, r)
}

// aiImageScanRun scans an image (or makes its SBOM) with the scanner the
// GUI finds and returns the report.
func aiImageScanRun(ctx context.Context, call aiIDECaller, h aiContainersHost, r aiImageReq) (string, error) {
	var found []containers.Scanner
	_, _ = call(ctx, func(m *Model) (string, tea.Cmd, error) { found = m.imageScanners(); return "", nil, nil })
	x := h.e.X
	if x == nil {
		x = servicesExec
	}
	if env := h.e.ContextEnv(); env != "" {
		x = services.WithEnv(x, env)
	}
	sctx, cancel := context.WithTimeout(ctx, aiImageScanWait)
	defer cancel()
	if r.op == "sbom" {
		out, err := containers.GenerateSBOM(sctx, x, found, r.ref)
		if err != nil {
			return "", fmt.Errorf("cannot make an SBOM: %s", errLine(aiContainersRedact(err.Error())))
		}
		return "SBOM of " + r.ref + "\n" + aiCap(aiContainersRedact(out), loadServicesSettings().MaxInspectKB, false), nil
	}
	s, ok := containers.PickScanner(found, "")
	if !ok {
		return "", errors.New("no image scanner: " + containers.NoScannerHint)
	}
	v, err := containers.ScanImage(sctx, x, s, r.ref)
	if err != nil {
		return "", fmt.Errorf("the scan failed: %s", errLine(aiContainersRedact(err.Error())))
	}
	containers.SortVulns(v)
	return aiCap(aiContainersRedact(containers.FormatVulnReport(r.ref, s.Name, v)), loadServicesSettings().MaxInspectKB, false), nil
}

// aiImageRemoteTags lists a repository's tags with the GUI's lister (the
// saved login only when the user opted in for the host).
func aiImageRemoteTags(ctx context.Context, call aiIDECaller, ref string) (string, error) {
	repo, err := containers.ParseRemoteRepo(ref)
	if err != nil {
		return "", err
	}
	mk, err := aiRegistryAPI(ctx, call, repo.Host)
	if err != nil {
		return "", err
	}
	api, err := mk()
	if err != nil {
		return "", err
	}
	tctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	list, err := api.Tags(tctx, repo)
	if err != nil {
		return "", fmt.Errorf("cannot list the tags of %s: %s", repo.Ref(), errLine(ai.Redact(err.Error())))
	}
	more := ""
	if list.Truncated {
		more = " (more on the registry)"
	}
	return fmt.Sprintf("Tags of %s: %d%s\n%s", repo.Ref(), len(list.Tags), more, strings.Join(list.Tags, ", ")), nil
}

// aiRegistryAPI is the GUI's lister maker for host, read on the UI thread.
func aiRegistryAPI(ctx context.Context, call aiIDECaller, host string) (func() (*containers.RegistryAPI, error), error) {
	var mk func() (*containers.RegistryAPI, error)
	_, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		m.webServicesState()
		mk = m.registryAPIFor(host)
		return "", nil, nil
	})
	return mk, err
}

// aiImageUpdates asks each registry whether the running images are old,
// as Check Running Images for Updates does.
func aiImageUpdates(ctx context.Context, call aiIDECaller, h aiContainersHost) (string, error) {
	list, err := h.e.ListContainers(ctx)
	if err != nil {
		return "", errors.New(aiContainersRedact(err.Error()))
	}
	seen := map[string]bool{}
	var b strings.Builder
	n := 0
	for _, c := range list {
		ref := c.Image
		if !strings.EqualFold(c.State, "running") || seen[ref] || strings.HasPrefix(ref, "sha256:") || isHex(ref) {
			continue
		}
		seen[ref] = true
		repo, err := containers.ParseRemoteRepo(ref)
		if err != nil {
			continue
		}
		tag := "latest"
		if i := strings.LastIndexByte(ref, ':'); i > strings.LastIndexByte(ref, '/') {
			tag = ref[i+1:]
		}
		n++
		state := "unknown"
		mk, err := aiRegistryAPI(ctx, call, repo.Host)
		if err == nil {
			cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			var raw []byte
			raw, err = h.e.InspectRaw(cctx, ref)
			var remote string
			if err == nil {
				var api *containers.RegistryAPI
				if api, err = mk(); err == nil {
					remote, err = api.ManifestDigest(cctx, repo, tag)
				}
			}
			cancel()
			if err == nil {
				if upd, known := containers.ImageHasUpdate(containers.ImageRepoDigests(raw), ref, remote); known {
					state = map[bool]string{true: "a newer image is on the registry", false: "up to date"}[upd]
				}
			} else {
				state = "unknown: " + aiOneLineClip(errLine(ai.Redact(err.Error())), 120)
			}
		}
		fmt.Fprintf(&b, "- %s · %s\n", ref, state)
	}
	return fmt.Sprintf("%s\nRunning images checked: %d\n%s", h.header(ctx), n, strings.TrimRight(b.String(), "\n")), nil
}

// aiImagesManageTool is images_manage.
func (m *Model) aiImagesManageTool(call aiIDECaller) ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	list := func(d string) map[string]any {
		return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": d}
	}
	session := aiContainersGate(m.services)
	return ai.Tool{
		Name: "images_manage",
		Description: "Every image action of the Containers view on a host. push, run (name, ports like [\"8080:80\"], volumes, env " +
			"[\"NAME=value\"], network, restart, detach, keep, command) and build (file: a Dockerfile in the workspace, dir, tag, " +
			"target, build_args) run in a terminal tab; remove; tag (tag: the new reference); save (file), load (file), import " +
			"(file, ref) with workspace files; history (ENV values hidden), used_by, scan and sbom (the installed scanner), search " +
			"(term: Docker Hub), tags (a repository's remote tags), check_updates (running images against their registries), builds, " +
			"build_detail (ref of a build) and build_check (the Dockerfile checks). push, run, remove, build and load ask the user " +
			"every time; tag, save and import ask; the reads run at once. images_pull pulls; containers_prune prunes.",
		Params: map[string]any{
			"op":      map[string]any{"type": "string", "enum": aiImageOps, "description": "what to do"},
			"ref":     str("the image: repo:tag or id (images_list); import: the new image's name; tags: a repository; build_detail: a build ref"),
			"context": aiContextParam(),
			"tag":     str("tag: the new reference; build: the image tag"),
			"name":    str("run: the container name"), "ports": list("run: published ports like 8080:80"),
			"volumes": list("run: volumes like data:/var/lib/data"), "env": list("run: NAME=value"),
			"network": str("run: the network"), "restart": str("run: the restart policy"),
			"detach":  map[string]any{"type": "boolean", "description": "run: in the background (default: attached in the tab)"},
			"keep":    map[string]any{"type": "boolean", "description": "run: keep the container when it stops (default --rm)"},
			"command": list("run: the command"),
			"file":    str("build: the Dockerfile; save / load / import / build_check: a file, relative to the workspace"),
			"dir":     str("build: the build context folder (default the Dockerfile's)"), "target": str("build: the stage"),
			"build_args": list("build: KEY=value"), "term": str("search: what to look for on Docker Hub"),
		},
		Required: []string{"op"},
		Mutating: true,
		Gate: func(a map[string]any) (bool, string) {
			op := ai.ArgString(a, "op")
			switch {
			case aiImageReads[op]:
				return false, ""
			case aiImageCards[op] || !contains(aiImageOps, op):
				return true, ""
			}
			if call != nil && (op == "tag" || op == "save") {
				ctx, cancel := aiContainersDescribeCtx()
				defer cancel()
				if r, err := aiImageParse(ctx, call, a); err == nil && r.replace {
					return true, "" // Replace image tag? / Replace file? are cards
				}
			}
			return session(a)
		},
		Validate: func(a map[string]any) error {
			ctx, cancel := context.WithTimeout(context.Background(), aiContainersCallWait)
			defer cancel()
			_, err := aiImageParse(ctx, call, a)
			return err
		},
		Describe: func(a map[string]any) string { return aiImageDescribe(call, a) },
		Run:      func(ctx context.Context, a map[string]any) (string, error) { return aiImageRun(ctx, call, a) },
	}
}
