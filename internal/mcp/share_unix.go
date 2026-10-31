//go:build unix

package mcp

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// maxShareSocket is a safe unix socket path length (sun_path is 104
// bytes on macOS, 108 on Linux).
const maxShareSocket = 100

// shortShareBase holds the short socket folders (shortShareDir).
const shortShareBase = "/tmp"

// ShareSocketDir is the folder the sockets of dir really use: dir, or —
// when a socket path in dir would be too long (macOS's $TMPDIR is
// /var/folders/…/T/) — the short /tmp/termocode-<uid>-<hash of dir>.
// The hash keeps one short folder per long one, the same for every
// session. shareDir checks the folder is ours and private either way.
func ShareSocketDir(dir string) string {
	if len(dir)+len("/")+32+len(".sock") <= maxShareSocket {
		return dir
	}
	sum := sha256.Sum256([]byte(dir))
	return filepath.Join(shortShareBase, fmt.Sprintf("termocode-%d-%s", os.Getuid(), hex.EncodeToString(sum[:6])))
}

// shareDir returns the private socket folder (0700, ours, no symlink),
// creating it.
func shareDir(o *ShareOptions) (string, error) {
	dir := o.Dir
	if dir == "" {
		base, err := session.Dir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(base, "mcp")
	}
	dir = ShareSocketDir(dir)
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return "", err
	}
	if err := os.Mkdir(dir, 0o700); err == nil {
		if err := os.Chmod(dir, 0o700); err != nil { // undo the umask
			return "", err
		}
	} else if !errors.Is(err, os.ErrExist) {
		return "", err
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		return "", fmt.Errorf("%s is a symlink", dir)
	case !fi.IsDir():
		return "", fmt.Errorf("%s is not a folder", dir)
	case !ok || int(st.Uid) != os.Getuid():
		return "", fmt.Errorf("%s is not owned by this user", dir)
	case fi.Mode().Perm() != 0o700:
		return "", fmt.Errorf("%s has mode %o, want 700", dir, fi.Mode().Perm())
	}
	return dir, nil
}

// shareSalt is the per-user HMAC key of shareKey: 32 random bytes in
// <dir>/salt (0600), made by the first session.
func shareSalt(dir string) ([]byte, error) {
	p := filepath.Join(dir, "salt")
	for range 3 {
		b, err := os.ReadFile(p)
		if err == nil {
			if len(b) != 32 {
				return nil, fmt.Errorf("%s: bad salt", p)
			}
			return b, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		salt := make([]byte, 32)
		if _, err := rand.Read(salt); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(p+".tmp", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			if errors.Is(err, os.ErrExist) { // another session writes it now
				_ = os.Remove(p + ".tmp")
				continue
			}
			return nil, err
		}
		_, werr := f.Write(salt)
		cerr := f.Close()
		if werr != nil || cerr != nil {
			_ = os.Remove(p + ".tmp")
			return nil, errors.Join(werr, cerr)
		}
		// Link fails when another session made the salt first: use theirs.
		lerr := os.Link(p+".tmp", p)
		_ = os.Remove(p + ".tmp")
		if lerr != nil && !errors.Is(lerr, os.ErrExist) {
			return nil, lerr
		}
	}
	return nil, errors.New("could not make the share salt")
}

// startShared connects to the broker of s (expanded), starting the broker
// first when none runs. errShareUnavailable means: run it yourself. Any
// other error is the server's own (not found, handshake failed) and is
// not tried again in this session.
func startShared(ctx context.Context, o *ShareOptions, s Server, roots []string, opts Options) (*Process, error) {
	unavailable := func(err error) error { return fmt.Errorf("%w: %v", errShareUnavailable, err) }
	dir, err := shareDir(o)
	if err != nil {
		return nil, unavailable(err)
	}
	salt, err := shareSalt(dir)
	if err != nil {
		return nil, unavailable(err)
	}
	cfg := shareConfig{Command: s.Command, Args: s.Args, Env: s.Env, Cwd: s.Cwd, Roots: roots, Source: s.Source}
	if cfg.Cwd == "" {
		// The server ran in our folder before; it still does.
		if cfg.Cwd, err = os.Getwd(); err != nil {
			return nil, unavailable(err)
		}
	}
	grace := o.Grace
	if grace <= 0 {
		grace = DefaultShareGrace
	}
	cfg.GraceMS = grace.Milliseconds()
	cfg.StartMS = opts.startTimeout().Milliseconds()
	key := shareKey(salt, cfg)
	sock := filepath.Join(dir, key+".sock")

	nc, err := dialShare(sock)
	if err != nil {
		unlock, lerr := lockShare(ctx, filepath.Join(dir, key+".lock"))
		if lerr != nil {
			return nil, unavailable(lerr)
		}
		nc, err = dialShare(sock) // another session started it meanwhile
		if err != nil {
			err = spawnBroker(ctx, o, sock, cfg)
			if err == nil {
				nc, err = dialShare(sock)
				if err != nil {
					err = unavailable(err)
				}
			}
		}
		unlock()
		if err != nil {
			return nil, err
		}
	}
	p := &Process{shared: true, done: make(chan struct{})}
	outer := opts.Shared
	opts.Shared = func(n, pid int) {
		p.sessions.Store(int64(n))
		p.sharedPid.Store(int64(pid))
		if outer != nil {
			outer(n, pid)
		}
	}
	p.Client = NewClient(nc, nc, opts)
	p.closeFn = func() { leaveShared(p.Client) }
	crash.Go("a shared MCP server", func() {
		<-p.Client.Done()
		close(p.done)
	})
	ictx, cancel := context.WithTimeout(ctx, opts.startTimeout())
	defer cancel()
	if err := p.Client.Initialize(ictx); err != nil {
		p.Close()
		return nil, err
	}
	// The broker sends the session count right after initialize; wait a
	// moment for it, so the first status already says "shared".
	for i := 0; i < 50 && p.Pid() == 0; i++ {
		select {
		case <-p.done:
			return nil, errors.New("the shared server closed the connection")
		case <-time.After(10 * time.Millisecond):
		}
	}
	opts.log("using the shared server (pid %d, %d sessions)", p.Pid(), p.Sessions())
	return p, nil
}

func dialShare(sock string) (net.Conn, error) {
	return net.DialTimeout("unix", sock, time.Second)
}

// lockShare holds <key>.lock while a session dials or starts the broker,
// so two sessions never start two brokers for one key.
func lockShare(ctx context.Context, path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(initTimeout + 5*time.Second)
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() { _ = f.Close() }, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) || time.Now().After(deadline) || ctx.Err() != nil {
			_ = f.Close()
			return nil, fmt.Errorf("lock %s: %w", filepath.Base(path), errors.Join(err, ctx.Err()))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// shareCommand is the broker's command line: the binary and its fixed
// args only. The socket path goes in the env; the config (with secrets)
// goes on stdin.
func shareCommand(o *ShareOptions, sock string) *exec.Cmd {
	cmd := spawn.Command(o.Exe, o.Args...)
	cmd.Env = append(os.Environ(), ShareEnv+"="+sock)
	cmd.Dir = "/"
	// Its own session: a terminal hangup or Ctrl+C in this session, or
	// this session's crash, does not stop the server other sessions use.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd
}

// spawnBroker starts the broker and waits for its "ok". A broker that
// does not run at all is errShareUnavailable; "error: …" is the server's
// error.
func spawnBroker(ctx context.Context, o *ShareOptions, sock string, cfg shareConfig) error {
	if o.Exe == "" {
		return fmt.Errorf("%w: no broker program", errShareUnavailable)
	}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("%w: %v", errShareUnavailable, err)
	}
	cmd := shareCommand(o, sock)
	// Our own pipes (not StdoutPipe): Wait runs on its own goroutine and
	// must not close the answer before we read it.
	inR, inW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("%w: %v", errShareUnavailable, err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		_, _ = inR.Close(), inW.Close()
		return fmt.Errorf("%w: %v", errShareUnavailable, err)
	}
	defer outR.Close()
	cmd.Stdin, cmd.Stdout = inR, outW
	err = cmd.Start()
	_, _ = inR.Close(), outW.Close()
	if err != nil {
		_ = inW.Close()
		return fmt.Errorf("%w: %v", errShareUnavailable, err)
	}
	crash.Go("a shared MCP server", func() { _ = cmd.Wait() }) // reap it when it ends
	crash.Go("a shared MCP server", func() {
		_, _ = inW.Write(append(cfgJSON, '\n'))
		_ = inW.Close()
	})
	stdout := outR
	line := make(chan string, 1)
	crash.Go("a shared MCP server", func() {
		l, _ := bufio.NewReader(stdout).ReadString('\n')
		line <- strings.TrimSpace(l)
	})
	select {
	case l := <-line:
		switch {
		case l == "ok":
			return nil
		case strings.HasPrefix(l, "unavailable: "):
			return fmt.Errorf("%w: %s", errShareUnavailable, strings.TrimPrefix(l, "unavailable: "))
		case strings.HasPrefix(l, "error: "):
			return errors.New(strings.TrimPrefix(l, "error: "))
		}
		_ = cmd.Process.Kill()
		return fmt.Errorf("%w: the broker did not start", errShareUnavailable)
	case <-time.After(cfg.startTimeout() + 5*time.Second):
	case <-ctx.Done():
	}
	_ = cmd.Process.Kill()
	return fmt.Errorf("%w: the broker did not answer", errShareUnavailable)
}
