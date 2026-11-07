package kube

import (
	"context"
	"errors"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/services"
)

// copyTimeout bounds one `kubectl cp` (files can be large).
const copyTimeout = 5 * time.Minute

// DefaultDebugImage is the image of a debug container when none is given.
const DefaultDebugImage = "busybox:1.36"

// PodFile is one entry of a pod folder listing.
type PodFile struct {
	Name string
	Dir  bool
	Link string // symlink target ("" = not a link)
	Size int64
	Mode string // e.g. "-rw-r--r--"
}

func badArg(s string) bool {
	if s == "" || strings.HasPrefix(s, "-") {
		return true
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// checkCopy refuses names and paths kubectl would read as a flag or as the
// other side of the copy.
func checkCopy(pod, container, podPath, local string) error {
	switch {
	case badArg(pod) || strings.ContainsAny(pod, ":/"):
		return errors.New("not a pod name")
	case container != "" && badArg(container):
		return errors.New("not a container name")
	case badArg(podPath) || !path.IsAbs(podPath):
		return errors.New("the pod path must be absolute")
	case badArg(local) || !filepath.IsAbs(local) || strings.Contains(local, ":"):
		return errors.New("the local path must be absolute and have no ':'")
	}
	return nil
}

func (c Client) copy(ctx context.Context, src, dst, container string) error {
	if c.Timeout < copyTimeout {
		c.Timeout = copyTimeout
	}
	args := append([]string{"cp"}, c.scope(true)...)
	args = append(args, src, dst)
	if container != "" {
		args = append(args, "--container", container)
	}
	_, err := c.capture(ctx, args...)
	return err
}

// CopyFromPod is `kubectl cp pod:podPath local` (container "" = default).
func (c Client) CopyFromPod(ctx context.Context, pod, container, podPath, local string) error {
	if err := checkCopy(pod, container, podPath, local); err != nil {
		return err
	}
	return c.copy(ctx, pod+":"+podPath, local, container)
}

// CopyToPod is `kubectl cp local pod:podPath`: a write into the pod, so the
// app asks the user first; the AI never runs it.
func (c Client) CopyToPod(ctx context.Context, pod, container, local, podPath string) error {
	if err := checkCopy(pod, container, podPath, local); err != nil {
		return err
	}
	return c.copy(ctx, local, pod+":"+podPath, container)
}

// ListPodDir lists one folder of a pod with `ls -la` (read only). "." and
// ".." and lines it cannot read are left out.
func (c Client) ListPodDir(ctx context.Context, pod, container, dir string) ([]PodFile, error) {
	if badArg(pod) || (container != "" && badArg(container)) || badArg(dir) || !path.IsAbs(dir) {
		return nil, errors.New("the folder must be an absolute path in a named pod")
	}
	args := append([]string{"exec"}, c.scope(true)...)
	args = append(args, pod)
	if container != "" {
		args = append(args, "--container", container)
	}
	args = append(args, "--", "ls", "-la", "--", dir)
	out, err := c.capture(ctx, args...)
	if err != nil {
		return nil, err
	}
	return parseLs(out), nil
}

// parseLs reads `ls -la` lines: mode links owner group size month day
// time-or-year name (the name may have spaces; a link is "name -> target").
func parseLs(out string) []PodFile {
	var files []PodFile
	for _, line := range strings.Split(out, "\n") {
		rest := strings.TrimRight(line, "\r")
		var f [8]string
		ok := true
		for i := range f {
			rest = strings.TrimLeft(rest, " \t")
			j := strings.IndexAny(rest, " \t")
			if j < 0 {
				ok = false
				break
			}
			f[i], rest = rest[:j], rest[j:]
		}
		if !ok || len(f[0]) < 10 || !strings.ContainsRune("-dlcbps", rune(f[0][0])) {
			continue
		}
		size, err := strconv.ParseInt(f[4], 10, 64)
		if err != nil {
			continue
		}
		name := strings.TrimLeft(rest, " \t")
		pf := PodFile{Mode: f[0], Size: size, Dir: f[0][0] == 'd'}
		if f[0][0] == 'l' {
			if i := strings.Index(name, " -> "); i >= 0 {
				name, pf.Link = name[:i], name[i+4:]
			}
		}
		if name == "" || name == "." || name == ".." {
			continue
		}
		pf.Name = name
		files = append(files, pf)
	}
	return files
}

// DebugCommand opens a shell in a new ephemeral debug container of pod
// (`kubectl debug -it`) in a terminal tab. It changes the pod spec, so the
// app asks the user first. target "" shares no container's processes;
// image "" = DefaultDebugImage.
func (c Client) DebugCommand(pod, target, image string) (services.Command, error) {
	if image == "" {
		image = DefaultDebugImage
	}
	if badArg(pod) || (target != "" && badArg(target)) || badArg(image) || strings.ContainsAny(image, " \t") {
		return services.Command{}, errors.New("not a pod, container or image name")
	}
	argv := append([]string{c.Binary, "debug", "-it"}, c.scope(true)...)
	argv = append(argv, pod, "--image="+image)
	if target != "" {
		argv = append(argv, "--target="+target)
	}
	argv = append(argv, "--", "sh")
	return services.Command{Label: "debug: " + shown(pod, target), Argv: argv}, nil
}
