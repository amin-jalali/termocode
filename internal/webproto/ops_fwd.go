package webproto

// Port preview from the page (WEB-54). A click on a localhost link in
// the page (terminal or chat output) asks web.forward with the port only:
// there is no host field, the target is always 127.0.0.1 of the computer
// the UI runs on. It is a LaptopCard op: the same Risky card as the
// laptop's Web: Forward Port… (the page approves it, or the laptop with
// web_approve_on_laptop). After the yes, the page that asked gets
// fwd.ready with the address to open, which the hub builds (never from
// input): its own origin, never a path under the panel.

// OpWebForward asks to forward one port of the computer's loopback.
const OpWebForward = "web.forward"

// WebForwardArgs is web.forward's args: a port and nothing else.
type WebForwardArgs struct {
	Port int `json:"port"`
}

func (a *WebForwardArgs) validate(int) error {
	if a.Port < 1 || a.Port > 65535 {
		return invalid("bad port")
	}
	return nil
}

// EventFwdReady is the ev kind sent only to the page that asked
// web.forward, once the forward is open.
const EventFwdReady = "fwd.ready"

// FwdReady is the data of a fwd.ready ev. Mode says where it opens:
// tailnet (URL: the port's own https address on the tailnet), local (URL:
// http://127.0.0.1:<port>/, nothing forwarded) or ssh (no URL: termocode
// ssh on the user's computer opens the tab there itself).
type FwdReady struct {
	Port int    `json:"port"`
	Mode string `json:"mode" enum:"tailnet|local|ssh"`
	URL  string `json:"url,omitempty"`
}

var _ = func() bool {
	opTable = append(opTable, Policy{Op: OpWebForward, New: newOf[WebForwardArgs](), Role: RoleController, Rate: slow,
		MaxBytes: 1 * kib, LaptopCard: true, Audit: AuditYes, Phase: PhaseV2})
	return true
}()

func init() {
	registerResult(OpWebForward, AskRef{})
	registerEvent(EventFwdReady, FwdReady{})
}
