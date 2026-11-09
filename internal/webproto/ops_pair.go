package webproto

import (
	"strings"
	"time"
)

// Pairing a device from the web panel (desktop web): Pair a Device… and
// Devices… as on the laptop, with no step on the laptop. A controller
// page opens a pairing link (web.pair_open): the hub makes the one-time
// secret, sends the gateway only its digest, and answers this page alone
// with the link and its QR code; the link works once, for the laptop's
// own time (webgw.PairTTL). When the phone asks, the page that opened
// the link gets the pair.ask ev (the device's name and its 6-digit
// code), the laptop its card as before; the first answer counts
// (web.pair_answer here, the card there) and the other closes
// (pair.end). Devices… lists the paired devices (web.devices); Remove
// asks first (web.device_remove, a laptop card: the page asks, Cancel
// first). A viewer may call none of them; the link never reaches a
// viewer, a log, the audit or a file.

// Pairing op names.
const (
	OpWebPairOpen     = "web.pair_open"
	OpWebPairClose    = "web.pair_close"
	OpWebPairAnswer   = "web.pair_answer"
	OpWebDevices      = "web.devices"
	OpWebDeviceRemove = "web.device_remove"
)

// PairLink is the result of web.pair_open: the link (its secret is in
// it; only the page that asked gets it), its QR code as the SVG path of
// its dark modules in module units (Size modules a side, no quiet zone),
// how long it works and the panel's address for a person to read.
type PairLink struct {
	Link string `json:"link"`
	QR   string `json:"qr"`
	Size int    `json:"size"`
	TTL  int    `json:"ttl_s"`
	Addr string `json:"addr"`
}

// PairAnswerArgs is the page's answer to the pairing question: viewer or
// control pairs with that role; none is Not now.
type PairAnswerArgs struct {
	Role string `json:"role,omitempty" enum:"viewer|control"`
}

func (a *PairAnswerArgs) validate(int) error {
	if a.Role != "" && a.Role != "viewer" && a.Role != "control" {
		return invalid("bad role")
	}
	return nil
}

// MaxDeviceID caps a device id.
const MaxDeviceID = 64

// DeviceArgs names one paired device by its id.
type DeviceArgs struct {
	ID string `json:"id"`
}

func (a *DeviceArgs) validate(int) error {
	rest, ok := strings.CutPrefix(a.ID, "dev-")
	if !ok || rest == "" || len(a.ID) > MaxDeviceID {
		return invalid("bad device id")
	}
	for _, r := range rest {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return invalid("bad device id")
		}
	}
	return nil
}

// WebDevice is one paired device as the page lists it. LastSeen and
// Created are Unix seconds (0: never); Live: a browser of it is
// connected now; Self: it is the device of this page; Repair: it must
// pair again (its passkey looked copied).
type WebDevice struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Role     string `json:"role" enum:"viewer|control"`
	Guest    bool   `json:"guest,omitempty"`
	Bookmark bool   `json:"bookmark,omitempty"`
	Live     bool   `json:"live,omitempty"`
	Self     bool   `json:"self,omitempty"`
	Repair   bool   `json:"repair,omitempty"`
	Created  int64  `json:"created"`
	LastSeen int64  `json:"last_seen,omitempty"`
}

// WebDeviceList is the result of web.devices, in the store's order.
type WebDeviceList struct {
	Items []WebDevice `json:"items"`
}

// EventPairAsk is the ev kind sent only to the page that opened the
// pairing link when the phone asks: its name and code. Laptop: this page
// may not answer (web_approve_on_laptop is on); the laptop's card does.
const EventPairAsk = "pair.ask"

// PairAsk is the data of a pair.ask ev.
type PairAsk struct {
	Name   string `json:"name"`
	Code   string `json:"code"`
	Laptop bool   `json:"laptop,omitempty"`
}

// EventPairEnd is the ev kind sent to that page when its pairing ended
// elsewhere: answered on the laptop (Role: the role given, none: Not
// now), or the link closed because the laptop made a new one.
const EventPairEnd = "pair.end"

// PairEnd is the data of a pair.end ev.
type PairEnd struct {
	Why  string `json:"why" enum:"answered|closed"`
	Role string `json:"role,omitempty" enum:"viewer|control"`
}

var opPairRows = []Policy{
	{Op: OpWebPairOpen, New: newOf[EmptyArgs](), Role: RoleController, Rate: Rate{N: 1, Per: 2 * time.Second}, MaxBytes: 1 * kib, Live: 1, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpWebPairClose, New: newOf[EmptyArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpWebPairAnswer, New: newOf[PairAnswerArgs](), Role: RoleController, Rate: perSec(1), MaxBytes: 1 * kib, Audit: AuditYes, Phase: PhaseV2},
	{Op: OpWebDevices, New: newOf[EmptyArgs](), Role: RoleController, Rate: perSec(2), MaxBytes: 1 * kib, Audit: AuditNo, Phase: PhaseV2},
	{Op: OpWebDeviceRemove, New: newOf[DeviceArgs](), Role: RoleController, Rate: slow, MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2},
}

var _ = func() bool { opTable = append(opTable, opPairRows...); return true }()

func init() {
	registerResult(OpWebPairOpen, PairLink{})
	registerResult(OpWebDevices, WebDeviceList{})
	registerResult(OpWebDeviceRemove, AskRef{})
	registerEvent(EventPairAsk, PairAsk{})
	registerEvent(EventPairEnd, PairEnd{})
}
