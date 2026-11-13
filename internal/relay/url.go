package relay

import (
	"errors"
	"net"
	"net/url"
)

// CheckURL checks a relay's base address (web_relay): https://host[:port]
// with no path, user, query or fragment; http only for a relay on
// loopback (a self-check or a test). The page's code comes from the
// relay, so anything that is not TLS could change it on the way.
func CheckURL(base string) (*url.URL, error) {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, errors.New("web_relay: want https://host[:port]")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if ip := net.ParseIP(u.Hostname()); u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return nil, errors.New("web_relay: plain http only on loopback; use https")
		}
	default:
		return nil, errors.New("web_relay: want https://host[:port]")
	}
	u.Path = ""
	return u, nil
}

// WSURL is the WebSocket address of room on the relay at base, for side
// (SideGateway or SidePage).
func WSURL(base, room, side string) (string, error) {
	u, err := CheckURL(base)
	if err != nil {
		return "", err
	}
	if !ValidRoom(room) {
		return "", errors.New("relay: bad room id")
	}
	u.Scheme = map[string]string{"https": "wss", "http": "ws"}[u.Scheme]
	u.Path = PathPrefix + room
	u.RawQuery = "side=" + side
	return u.String(), nil
}
