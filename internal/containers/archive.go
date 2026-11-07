package containers

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Image and container archive (CTX-31): engine argv for save, load,
// import, commit, export and diff, plus pure helpers for the views
// (diff list, layer sizes, which containers use an image). Nothing here
// runs the engine.

// DiffEntry is one line of `docker diff`.
type DiffEntry struct {
	Kind string // added, changed or deleted
	Path string
}

// ParseDiff reads `docker diff` output ("A /path", "C /path", "D /path").
// Unknown lines are skipped.
func ParseDiff(out string) []DiffEntry {
	var res []DiffEntry
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		code, path, ok := strings.Cut(line, " ")
		if !ok || path == "" {
			continue
		}
		var kind string
		switch code {
		case "A":
			kind = "added"
		case "C":
			kind = "changed"
		case "D":
			kind = "deleted"
		default:
			continue
		}
		res = append(res, DiffEntry{Kind: kind, Path: strings.TrimSpace(path)})
	}
	return res
}

// Layer is one image layer with its share of the image size.
type Layer struct {
	ID        string
	CreatedBy string
	Bytes     float64
	Percent   float64 // of Total, one decimal
}

// LayerReport lists the layers that add bytes, largest first.
type LayerReport struct {
	Layers []Layer
	Total  float64
	Empty  int // layers of 0 bytes (ENV, LABEL …), left out of Layers
}

// LayerSizes builds the layer size analysis from `docker history`.
func LayerSizes(h []HistoryEntry) LayerReport {
	var r LayerReport
	for _, e := range h {
		b, ok := ParseBytes(e.Size)
		if !ok || b <= 0 {
			r.Empty++
			continue
		}
		r.Total += b
		r.Layers = append(r.Layers, Layer{ID: e.ID, CreatedBy: e.CreatedBy, Bytes: b})
	}
	sort.SliceStable(r.Layers, func(i, j int) bool { return r.Layers[i].Bytes > r.Layers[j].Bytes })
	if r.Total > 0 {
		for i := range r.Layers {
			r.Layers[i].Percent = math.Round(r.Layers[i].Bytes*100/r.Total*10) / 10
		}
	}
	return r
}

// ImageUsedBy returns the containers made from img (by image id, tag,
// or a short id), in the given order.
func ImageUsedBy(img Image, cs []Container) []Container {
	full := img.ID
	short := strings.TrimPrefix(full, "sha256:")
	var out []Container
	for _, c := range cs {
		if usesImage(img, full, short, c) {
			out = append(out, c)
		}
	}
	return out
}

func usesImage(img Image, full, short string, c Container) bool {
	if full != "" && (c.ImageID == full || strings.TrimPrefix(c.ImageID, "sha256:") == short) {
		return true
	}
	for _, t := range img.RepoTags {
		if c.Image == t {
			return true
		}
	}
	ref := strings.TrimPrefix(c.Image, "sha256:")
	return short != "" && len(ref) >= 6 && strings.HasPrefix(short, ref)
}

var errNoPath = errors.New("a file path is needed")

// argOK refuses empty values and values that the engine would read as a flag.
func argOK(what, v string) error {
	if strings.TrimSpace(v) == "" {
		return fmt.Errorf("%s is empty", what)
	}
	if strings.HasPrefix(v, "-") {
		return fmt.Errorf("%s may not start with -: %q", what, v)
	}
	return nil
}

// SaveArgs: save images to a tar file.
func SaveArgs(path string, refs ...string) ([]string, error) {
	if path == "" {
		return nil, errNoPath
	}
	if len(refs) == 0 {
		return nil, errors.New("pick at least one image")
	}
	for _, r := range refs {
		if err := argOK("image", r); err != nil {
			return nil, err
		}
	}
	return append([]string{"save", "-o", path}, refs...), nil
}

// LoadArgs: load images from a tar file made by save.
func LoadArgs(path string) ([]string, error) {
	if path == "" {
		return nil, errNoPath
	}
	return []string{"load", "-i", path}, nil
}

// ImportArgs: make an image from a file system tar (ref may be empty).
func ImportArgs(path, ref string) ([]string, error) {
	if err := argOK("file", path); err != nil {
		return nil, err
	}
	args := []string{"import", path}
	if ref != "" {
		if err := argOK("image", ref); err != nil {
			return nil, err
		}
		args = append(args, ref)
	}
	return args, nil
}

// CommitArgs: make an image from a container (ref and message may be empty).
func CommitArgs(id, ref, msg string) ([]string, error) {
	if err := argOK("container", id); err != nil {
		return nil, err
	}
	args := []string{"commit"}
	if msg != "" {
		args = append(args, "-m", msg)
	}
	args = append(args, id)
	if ref != "" {
		if err := argOK("image", ref); err != nil {
			return nil, err
		}
		args = append(args, ref)
	}
	return args, nil
}

// ExportArgs: write a container's file system to a tar file.
func ExportArgs(id, path string) ([]string, error) {
	if err := argOK("container", id); err != nil {
		return nil, err
	}
	if path == "" {
		return nil, errNoPath
	}
	return []string{"export", "-o", path, id}, nil
}

// DiffArgs: list the files a container changed.
func DiffArgs(id string) ([]string, error) {
	if err := argOK("container", id); err != nil {
		return nil, err
	}
	return []string{"diff", id}, nil
}
