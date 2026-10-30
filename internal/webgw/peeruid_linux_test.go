//go:build linux

package webgw

import (
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"
)

const procHeader = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"

func procRow(local, remote string, uid int) string {
	return fmt.Sprintf("   0: %s %s 01 00000000:00000000 00:00000000 00000000 %5d        0 12345 1 0000000000000000 20 4 30 10 -1\n", local, remote, uid)
}

func TestProcNetTCPParse(t *testing.T) {
	us := netip.MustParseAddrPort("127.0.0.1:8080")
	peer := netip.MustParseAddrPort("127.0.0.1:41205") // port A0F5
	v4 := "0100007F"                                   // 127.0.0.1 on a little-endian host
	if !isLittleEndian() {
		v4 = "7F000001"
	}
	table := procHeader +
		procRow(v4+":1F90", v4+":A0F5", 1000) + // our side of the same link
		procRow(v4+":A0F5", v4+":1F90", 4242) // the peer's socket
	uid, found, err := findProcNetUID(strings.NewReader(table), peer, us)
	if err != nil || !found || uid != 4242 {
		t.Fatalf("uid %d found %v err %v, want 4242", uid, found, err)
	}
	if _, found, _ := findProcNetUID(strings.NewReader(table), netip.MustParseAddrPort("127.0.0.1:1"), us); found {
		t.Fatal("found a socket that is not there")
	}
	// An IPv4-mapped IPv6 row (a dual-stack client socket).
	mapped := "0000000000000000FFFF00000100007F"
	if !isLittleEndian() {
		mapped = "00000000000000000000FFFF7F000001"
	}
	table6 := procHeader + procRow(mapped+":A0F5", mapped+":1F90", 777)
	if uid, found, err := findProcNetUID(strings.NewReader(table6), peer, us); err != nil || !found || uid != 777 {
		t.Fatalf("tcp6: uid %d found %v err %v, want 777", uid, found, err)
	}
	for _, bad := range []string{"0100007F", "0100007F:1F9", "XX00007F:1F90", "0100007F:ZZZZ"} {
		if _, err := parseProcAddr(bad); err == nil {
			t.Errorf("parseProcAddr(%q): no error", bad)
		}
	}
}

func isLittleEndian() bool {
	ap, err := parseProcAddr("0100007F:0001")
	return err == nil && ap.Addr() == netip.MustParseAddr("127.0.0.1")
}

// fakeProcNet makes the peer check read table instead of /proc.
func fakeProcNet(t *testing.T, table func() string) {
	t.Helper()
	old := openProcNet
	openProcNet = func(name string) (io.ReadCloser, error) {
		if !strings.HasSuffix(name, "/tcp") {
			return nil, os.ErrNotExist
		}
		return io.NopCloser(strings.NewReader(table())), nil
	}
	t.Cleanup(func() { openProcNet = old })
}

// Spec (T16): a browser on 127.0.0.1 that runs as another user is closed
// before the gateway reads a byte; our own user gets through.
func TestOtherUIDRefused(t *testing.T) {
	ln, _, err := Listen(ListenConfig{})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	// The real table: this process is the peer, so it passes.
	c, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	sc, err := ln.Accept()
	if err != nil {
		t.Fatalf("own uid refused: %v", err)
	}
	_ = sc.Close()

	// A table that says another user owns the peer socket.
	var peerAddr string
	known := make(chan struct{})
	fakeProcNet(t, func() string {
		<-known
		us := ln.Addr().(*net.TCPAddr).AddrPort()
		p := netip.MustParseAddrPort(peerAddr)
		return procHeader + procRow(procHex(p), procHex(us), os.Getuid()+1)
	})
	accepted := make(chan net.Conn, 1)
	go func() {
		if c, err := ln.Accept(); err == nil {
			accepted <- c
		}
	}()
	other, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	peerAddr = other.LocalAddr().String()
	close(known)
	_ = other.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := other.Read(make([]byte, 1)); err == nil {
		t.Fatal("another user's connection was not closed")
	}
	select {
	case c := <-accepted:
		c.Close()
		t.Fatal("another user's connection was handed out")
	default:
	}
}

// procHex writes an IPv4 address and port as /proc/net/tcp does.
func procHex(ap netip.AddrPort) string {
	b := ap.Addr().As4()
	if isLittleEndian() {
		return fmt.Sprintf("%02X%02X%02X%02X:%04X", b[3], b[2], b[1], b[0], ap.Port())
	}
	return fmt.Sprintf("%02X%02X%02X%02X:%04X", b[0], b[1], b[2], b[3], ap.Port())
}

// An unreadable table refuses the connection (fail closed).
func TestPeerUIDUnknownRefused(t *testing.T) {
	old := openProcNet
	openProcNet = func(string) (io.ReadCloser, error) { return nil, os.ErrPermission }
	t.Cleanup(func() { openProcNet = old })
	a, b := tcpPair(t)
	defer a.Close()
	defer b.Close()
	if err := checkTCPPeer(b); err == nil {
		t.Fatal("no table, but the peer passed")
	}
}

func tcpPair(t *testing.T) (client, server net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	client, err = net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	server, err = ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	return client, server
}

func FuzzProcNetTCP(f *testing.F) {
	f.Add(procHeader + procRow("0100007F:A0F5", "0100007F:1F90", 1000))
	f.Add(procHeader + procRow("0000000000000000FFFF00000100007F:A0F5", "0000000000000000FFFF00000100007F:1F90", 1))
	f.Add("garbage\n1: 2 3 4 5 6 7 x\n")
	us := netip.MustParseAddrPort("127.0.0.1:8080")
	peer := netip.MustParseAddrPort("127.0.0.1:41205")
	f.Fuzz(func(t *testing.T, table string) {
		uid, found, _ := findProcNetUID(strings.NewReader(table), peer, us)
		if !found && uid != 0 {
			t.Fatalf("uid %d without a row", uid)
		}
	})
}

// In tailnet mode Tailscale Serve proxies from tailscaled, which runs as
// root: its connection passes there, never in plain loopback mode, and
// any other user stays refused in both (T16).
func TestTailnetAllowsTailscaledOnly(t *testing.T) {
	a, b := tcpPair(t)
	defer a.Close()
	defer b.Close()
	for _, tc := range []struct {
		uid     int
		tailnet bool
		ok      bool
	}{
		{os.Getuid(), false, true},
		{os.Getuid(), true, true},
		{0, true, true},
		{0, false, os.Getuid() == 0},
		{os.Getuid() + 1, true, os.Getuid()+1 == 0},
	} {
		uid := tc.uid
		fakeProcNet(t, func() string {
			return procHeader + procRow(procHex(a.LocalAddr().(*net.TCPAddr).AddrPort()), procHex(b.LocalAddr().(*net.TCPAddr).AddrPort()), uid)
		})
		err := peerCheck(tc.tailnet)(b)
		if (err == nil) != tc.ok {
			t.Errorf("uid %d tailnet %v: err %v, want ok=%v", tc.uid, tc.tailnet, err, tc.ok)
		}
	}
}
