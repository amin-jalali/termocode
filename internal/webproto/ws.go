package webproto

// MOB-50: one phone, several projects. The paired gateway is the door for
// every running workspace of the same user on the machine: it keeps the
// connection to its own (home) hub and opens one more hub connection per
// other workspace the page names in a frame's "ws" field.
//
// Rules:
//   - ws is a workspace key (session.Key: 16 lower-case hex digits),
//     never a path; the phone cannot name a folder.
//   - no ws, or the home key, means the home hub (what every frame meant
//     before MOB-50).
//   - the gateway strips ws before a frame goes to a hub, and sets it on
//     every line that comes back from a hub that is not the home one (and
//     on the home hub's meta, so the page learns its home key). A hub
//     never sets ws itself.
//   - a hub that ends, stalls or cannot be reached ends only its project:
//     the page gets ev "ws.gone" with the ws set, its requests get err
//     "gone", and the home link stays up.

// WSLen is the length of a workspace key on the wire.
const WSLen = 16

// MaxHubs is how many other workspaces one browser connection may have
// open through the door at once (web_projects_max).
const MaxHubs = 4

// ValidWS reports whether s is a workspace key: 16 lower-case hex digits.
func ValidWS(s string) bool {
	if len(s) != WSLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// validWSField: an empty ws (the home hub) or a workspace key.
func validWSField(s string) bool { return s == "" || ValidWS(s) }

func init() {
	registerEvent(EventWSGone, WSGone{})
}

// EventWSGone is the ev kind, with ws set, of a project hub that ended
// for this connection. The page marks that project's topics stale and
// may subscribe again later.
const EventWSGone = "ws.gone"

// Why a project hub ended (WSGone.Why).
const (
	WSGoneClosed     = "closed"      // the hub said bye or closed the link
	WSGoneNotRunning = "not_running" // no such workspace runs now
	WSGoneBusy       = "busy"        // the hub did not take frames in time
	WSGoneLimit      = "limit"       // MaxHubs projects are open already
	WSGoneOff        = "off"         // this gateway is not a door
	WSGoneBad        = "bad"         // the hub broke the protocol
)

// WSGone is the data of ev "ws.gone".
type WSGone struct {
	Why string `json:"why" enum:"closed|not_running|busy|limit|off|bad"`
}
