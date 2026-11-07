package dbclient

import (
	"context"
	"io"
	"net"
	"sync"

	"github.com/amin-jalali/termocode/internal/crash"
)

// localRelay listens on 127.0.0.1 and pipes every accepted connection to
// target through dial (the SSH tunnel). It is for drivers that dial the
// network themselves and take no dial hook (DBX-71): they connect to the
// returned address instead. stop closes the listener and every open pipe.
func localRelay(dial DialFunc, target string) (addr string, stop func(), err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", func() {}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	open := map[net.Conn]struct{}{}
	track := func(c net.Conn) bool {
		mu.Lock()
		defer mu.Unlock()
		if ctx.Err() != nil {
			return false
		}
		open[c] = struct{}{}
		return true
	}
	untrack := func(c net.Conn) {
		mu.Lock()
		delete(open, c)
		mu.Unlock()
	}
	crash.Go("dbclient.localRelay", func() {
		for {
			local, err := ln.Accept()
			if err != nil {
				return
			}
			crash.Go("dbclient.localRelay", func() {
				if !track(local) {
					_ = local.Close()
					return
				}
				defer untrack(local)
				defer local.Close()
				remote, err := dial(ctx, "tcp", target)
				if err != nil {
					return
				}
				if !track(remote) {
					_ = remote.Close()
					return
				}
				defer untrack(remote)
				defer remote.Close()
				done := make(chan struct{}, 2)
				pipe := func(dst, src net.Conn) {
					_, _ = io.Copy(dst, src)
					if cw, ok := dst.(interface{ CloseWrite() error }); ok {
						_ = cw.CloseWrite()
					}
					done <- struct{}{}
				}
				crash.Go("dbclient.localRelay", func() { pipe(remote, local) })
				crash.Go("dbclient.localRelay", func() { pipe(local, remote) })
				<-done
				<-done
			})
		}
	})
	var once sync.Once
	stop = func() {
		once.Do(func() {
			cancel()
			_ = ln.Close()
			mu.Lock()
			for c := range open {
				_ = c.Close()
			}
			mu.Unlock()
		})
	}
	return ln.Addr().String(), stop, nil
}
