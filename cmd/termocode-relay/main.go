// Command termocode-relay is the web panel's blind relay (ADR 0022,
// WEB-52): it pairs a laptop's gateway and a phone's page by a room id
// and copies their end-to-end encrypted messages. It holds no key and
// reads no payload. It lives in this repository as its own small binary
// until the hosting is decided; anyone can run their own:
//
//	go build -o termocode-relay ./cmd/termocode-relay
//	./termocode-relay -listen 127.0.0.1:8787 -trust-proxy   # behind your TLS proxy
//	./termocode-relay -listen :443 -tls-cert cert.pem -tls-key key.pem
//
// Then set "web_relay": "https://your.relay.host" in termocode's
// config.json. The log (stderr, JSON lines) has connects, ends, byte
// counts and a short room hash, never a room id or a payload; keep it at
// most 7 days.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/amin-jalali/termocode/internal/relay"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stderr, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "termocode-relay:", err)
		os.Exit(1)
	}
}

type options struct {
	listen     string
	cert, key  string
	trustProxy bool
	cfg        relay.Config
}

// parse reads the flags. A listener on anything but loopback needs TLS
// here (-tls-cert, -tls-key): the relay serves the page's code, and only
// TLS keeps that code whole. -trust-proxy says a TLS proxy of the
// operator's own is in front; the relay itself still listens on
// loopback then.
func parse(args []string, errw io.Writer) (options, error) {
	fs := flag.NewFlagSet("termocode-relay", flag.ContinueOnError)
	fs.SetOutput(errw)
	var o options
	fs.StringVar(&o.listen, "listen", "127.0.0.1:8787", "address to listen on")
	fs.StringVar(&o.cert, "tls-cert", "", "TLS certificate file (PEM)")
	fs.StringVar(&o.key, "tls-key", "", "TLS key file (PEM)")
	fs.BoolVar(&o.trustProxy, "trust-proxy", false, "a TLS proxy of yours is in front: take the client address from X-Forwarded-For")
	rate := fs.Int("rate", 4<<20, "bytes per second per pair")
	idle := fs.Duration("idle", 15*time.Minute, "a pair with no message ends after this")
	life := fs.Duration("max-life", 12*time.Hour, "a pair ends after this")
	rooms := fs.Int("max-rooms", 10000, "rooms at once")
	perIP := fs.Int("max-conns-per-ip", 20, "open connections per client address")
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	if fs.NArg() != 0 {
		return o, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if (o.cert == "") != (o.key == "") {
		return o, errors.New("-tls-cert and -tls-key go together")
	}
	host, port, err := net.SplitHostPort(o.listen)
	if err != nil {
		return o, fmt.Errorf("-listen: %w", err)
	}
	if n, err := strconv.Atoi(port); err != nil || n < 0 || n > 65535 {
		return o, fmt.Errorf("-listen: bad port %q", port)
	}
	ip := net.ParseIP(host)
	loop := host == "localhost" || (ip != nil && ip.IsLoopback())
	if !loop && o.cert == "" {
		return o, errors.New("listening beyond loopback needs -tls-cert and -tls-key (or listen on 127.0.0.1 behind your TLS proxy with -trust-proxy)")
	}
	o.cfg = relay.Config{BytesPerSecond: *rate, Idle: *idle, MaxLife: *life, MaxRooms: *rooms,
		MaxConnsPerIP: *perIP, TrustProxy: o.trustProxy}
	return o, nil
}

// run serves until ctx ends. The log goes to logw as JSON lines; the
// listening address goes to outw once the relay listens.
func run(ctx context.Context, args []string, logw, outw io.Writer) error {
	o, err := parse(args, logw)
	if err != nil {
		return err
	}
	var mu sync.Mutex
	enc := json.NewEncoder(logw)
	o.cfg.Log = func(e relay.Event) {
		mu.Lock()
		defer mu.Unlock()
		_ = enc.Encode(e)
	}
	rl := relay.New(o.cfg)
	srv := &http.Server{
		Handler:           secure(handler(rl, pageHandler(o.trustProxy))),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    8 << 10,
		ErrorLog:          log.New(io.Discard, "", 0), // its lines can quote request data
	}
	ln, err := net.Listen("tcp", o.listen)
	if err != nil {
		return err
	}
	fmt.Fprintf(outw, "termocode-relay: listening on %s\n", ln.Addr())
	errc := make(chan error, 1)
	go func() {
		if o.cert != "" {
			errc <- srv.ServeTLS(ln, o.cert, o.key)
		} else {
			errc <- srv.Serve(ln)
		}
	}()
	select {
	case <-ctx.Done():
	case err := <-errc:
		rl.Close()
		return err
	}
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx) // hijacked WebSockets are not waited for here
	rl.Close()
	_ = srv.Close()
	return nil
}

// handler routes the relay's paths: the WebSockets, a health check and
// the page (rl or page may be nil in tests).
func handler(rl *relay.Server, page http.Handler) http.Handler {
	mux := http.NewServeMux()
	if rl != nil {
		mux.Handle(relay.PathPrefix, rl)
	}
	if page != nil {
		mux.Handle("/", page)
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok\n")
	})
	return mux
}

// secure sets the relay's fixed security headers on every answer.
func secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cache-Control", "no-store")
		if r.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}
