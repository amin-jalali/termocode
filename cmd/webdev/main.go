//go:build unix

// Command webdev serves the web UI on 127.0.0.1 with the real guard,
// headers and CSP, for looking at the page while building it. It prints
// a one-time link. There is no hub behind it, so the page shows its
// offline and loading states; a build with -tags webdev also serves the
// component kit at /dev/kit.
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/amin-jalali/termocode/internal/webgw"
)

func main() {
	port := flag.Int("port", 0, "port on 127.0.0.1 (0: any free port)")
	flag.Parse()
	ln, site, err := webgw.Listen(webgw.ListenConfig{Port: *port})
	if err != nil {
		fmt.Fprintln(os.Stderr, "webdev:", err)
		os.Exit(1)
	}
	auth := webgw.NewAuth(webgw.NewTokens(15*time.Minute, 12*time.Hour), time.Now, nil)
	secret, err := auth.NewSecret()
	if err != nil {
		fmt.Fprintln(os.Stderr, "webdev:", err)
		os.Exit(1)
	}
	mux := http.NewServeMux()
	mux.Handle("/auth", auth)
	mux.Handle("/", webgw.Assets())
	base := site.Scheme + "://" + site.Hosts[0]
	fmt.Printf("page: %s/#s=%s\n", base, secret)
	if webgw.KitEnabled {
		fmt.Printf("kit:  %s/dev/kit\n", base)
	} else {
		fmt.Println("kit:  build with -tags webdev to serve /dev/kit")
	}
	srv := webgw.NewServer(site, mux, webgw.DefaultLimits, nil)
	if err := srv.Serve(ln); err != nil {
		fmt.Fprintln(os.Stderr, "webdev:", err)
		os.Exit(1)
	}
}
