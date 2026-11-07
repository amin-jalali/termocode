package containers

import (
	"context"
	"errors"
	"net"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// VolumeHelperImage is the small image that browses, backs up, restores
// and clones volumes. It runs with no network.
const VolumeHelperImage = "busybox:stable"

var (
	engineNamePattern = lazyre.New(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	optKeyPattern     = lazyre.New(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
)

// VolumeCreateOptions are the fields of a new volume.
type VolumeCreateOptions struct {
	Name    string
	Driver  string
	Options map[string]string // --opt key=value (driver options)
	Labels  map[string]string
}

// NetworkCreateOptions are the fields of a new network.
type NetworkCreateOptions struct {
	Name       string
	Driver     string
	Subnet     string
	Gateway    string
	IPRange    string
	Internal   bool
	Attachable bool
	IPv6       bool
	Labels     map[string]string
}

// VolumeEntry is one file or folder inside a volume.
type VolumeEntry struct {
	Name string
	Dir  bool
}

func checkEngineName(what, name string) error {
	if !engineNamePattern.MatchString(name) {
		return errors.New("invalid " + what + " name")
	}
	return nil
}

func keyValueArgs(flag string, m map[string]string) ([]string, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		if !optKeyPattern.MatchString(k) {
			return nil, errors.New("invalid " + strings.TrimPrefix(flag, "--") + " key")
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var out []string
	for _, k := range keys {
		out = append(out, flag, k+"="+m[k])
	}
	return out, nil
}

// VolumeCreateArgs is the engine argv (without the binary) that creates o.
func VolumeCreateArgs(o VolumeCreateOptions) ([]string, error) {
	if err := checkEngineName("volume", o.Name); err != nil {
		return nil, err
	}
	args := []string{"volume", "create"}
	if o.Driver != "" {
		if err := checkEngineName("driver", o.Driver); err != nil {
			return nil, err
		}
		args = append(args, "--driver", o.Driver)
	}
	labels, err := keyValueArgs("--label", o.Labels)
	if err != nil {
		return nil, err
	}
	opts, err := keyValueArgs("--opt", o.Options)
	if err != nil {
		return nil, err
	}
	args = append(args, labels...)
	args = append(args, opts...)
	return append(args, "--", o.Name), nil
}

// NetworkCreateArgs is the engine argv (without the binary) that creates o.
func NetworkCreateArgs(o NetworkCreateOptions) ([]string, error) {
	if err := checkEngineName("network", o.Name); err != nil {
		return nil, err
	}
	args := []string{"network", "create"}
	if o.Driver != "" {
		if err := checkEngineName("driver", o.Driver); err != nil {
			return nil, err
		}
		args = append(args, "--driver", o.Driver)
	}
	if o.Subnet != "" {
		if _, _, err := net.ParseCIDR(o.Subnet); err != nil {
			return nil, errors.New("invalid subnet")
		}
		args = append(args, "--subnet", o.Subnet)
	}
	if o.Gateway != "" {
		if net.ParseIP(o.Gateway) == nil {
			return nil, errors.New("invalid gateway")
		}
		args = append(args, "--gateway", o.Gateway)
	}
	if o.IPRange != "" {
		if _, _, err := net.ParseCIDR(o.IPRange); err != nil {
			return nil, errors.New("invalid IP range")
		}
		args = append(args, "--ip-range", o.IPRange)
	}
	if o.Internal {
		args = append(args, "--internal")
	}
	if o.Attachable {
		args = append(args, "--attachable")
	}
	if o.IPv6 {
		args = append(args, "--ipv6")
	}
	labels, err := keyValueArgs("--label", o.Labels)
	if err != nil {
		return nil, err
	}
	args = append(args, labels...)
	return append(args, "--", o.Name), nil
}

func helperRun(mounts ...string) []string {
	args := []string{"run", "--rm", "--network", "none"}
	for _, m := range mounts {
		args = append(args, "-v", m)
	}
	return append(args, VolumeHelperImage)
}

// VolumeBrowseArgs lists dir inside the volume (read-only mount). dir is
// cleaned so it never leaves the volume.
func VolumeBrowseArgs(volume, dir string) ([]string, error) {
	if err := checkEngineName("volume", volume); err != nil {
		return nil, err
	}
	p := path.Join("/v", path.Clean("/"+dir))
	return append(helperRun(volume+":/v:ro"), "ls", "-1Ap", "--", p), nil
}

// ParseVolumeListing reads the output of the browse command.
func ParseVolumeListing(out string) []VolumeEntry {
	var entries []VolumeEntry
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		if strings.HasSuffix(line, "/") {
			entries = append(entries, VolumeEntry{Name: strings.TrimSuffix(line, "/"), Dir: true})
			continue
		}
		entries = append(entries, VolumeEntry{Name: line})
	}
	return entries
}

func checkHostDir(dir string) error {
	if !filepath.IsAbs(dir) || strings.ContainsAny(dir, ":,") {
		return errors.New("backup folder must be an absolute path without ':' or ','")
	}
	return nil
}

func checkArchiveName(file string) error {
	if file == "" || strings.ContainsAny(file, `/\:`) || strings.HasPrefix(file, "-") || strings.HasPrefix(file, ".") {
		return errors.New("invalid archive file name")
	}
	return nil
}

func archiveArgs(volume, hostDir, file string) error {
	if err := checkEngineName("volume", volume); err != nil {
		return err
	}
	if err := checkHostDir(hostDir); err != nil {
		return err
	}
	return checkArchiveName(file)
}

// VolumeBackupArgs writes the volume as hostDir/file (tar.gz).
func VolumeBackupArgs(volume, hostDir, file string) ([]string, error) {
	if err := archiveArgs(volume, hostDir, file); err != nil {
		return nil, err
	}
	args := helperRun(volume+":/v:ro", filepath.Clean(hostDir)+":/backup")
	return append(args, "tar", "-czf", "/backup/"+file, "-C", "/v", "."), nil
}

// VolumeRestoreArgs unpacks hostDir/file into the volume. It overwrites
// files, so the UI asks first.
func VolumeRestoreArgs(volume, hostDir, file string) ([]string, error) {
	if err := archiveArgs(volume, hostDir, file); err != nil {
		return nil, err
	}
	args := helperRun(volume+":/v", filepath.Clean(hostDir)+":/backup:ro")
	return append(args, "tar", "-xzf", "/backup/"+file, "-C", "/v"), nil
}

// VolumeCloneArgs copies every file of src into dst (dst is created by
// the engine when it does not exist).
func VolumeCloneArgs(src, dst string) ([]string, error) {
	if err := checkEngineName("volume", src); err != nil {
		return nil, err
	}
	if err := checkEngineName("volume", dst); err != nil {
		return nil, err
	}
	if src == dst {
		return nil, errors.New("clone target must be another volume")
	}
	return append(helperRun(src+":/from:ro", dst+":/to"), "cp", "-a", "/from/.", "/to/"), nil
}

const volumeJobTimeout = 30 * time.Minute

// CreateVolumeWith creates a volume with a driver, options and labels.
func (e *Engine) CreateVolumeWith(ctx context.Context, o VolumeCreateOptions) error {
	args, err := VolumeCreateArgs(o)
	if err != nil {
		return err
	}
	return e.action(ctx, 60*time.Second, args...)
}

// CreateNetworkWith creates a network with a driver, IP settings and labels.
func (e *Engine) CreateNetworkWith(ctx context.Context, o NetworkCreateOptions) error {
	args, err := NetworkCreateArgs(o)
	if err != nil {
		return err
	}
	return e.action(ctx, 60*time.Second, args...)
}

// BrowseVolume lists dir inside the volume.
func (e *Engine) BrowseVolume(ctx context.Context, volume, dir string) ([]VolumeEntry, error) {
	args, err := VolumeBrowseArgs(volume, dir)
	if err != nil {
		return nil, err
	}
	out, stderr, code, err := e.captureTimeout(ctx, 2*time.Minute, args...)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, actionError(e, stderr, code)
	}
	return ParseVolumeListing(out), nil
}

// BackupVolume writes the volume to hostDir/file.
func (e *Engine) BackupVolume(ctx context.Context, volume, hostDir, file string) error {
	args, err := VolumeBackupArgs(volume, hostDir, file)
	if err != nil {
		return err
	}
	return e.action(ctx, volumeJobTimeout, args...)
}

// RestoreVolume unpacks hostDir/file into the volume (after the card).
func (e *Engine) RestoreVolume(ctx context.Context, volume, hostDir, file string) error {
	args, err := VolumeRestoreArgs(volume, hostDir, file)
	if err != nil {
		return err
	}
	return e.action(ctx, volumeJobTimeout, args...)
}

// CloneVolume copies src into dst (after the card).
func (e *Engine) CloneVolume(ctx context.Context, src, dst string) error {
	args, err := VolumeCloneArgs(src, dst)
	if err != nil {
		return err
	}
	return e.action(ctx, volumeJobTimeout, args...)
}
