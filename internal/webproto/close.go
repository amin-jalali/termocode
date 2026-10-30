package webproto

// CloseCode is a WebSocket close code the gateway sends. The 4xxx codes are
// ours; the browser maps each one to a fixed, short message for the user.
type CloseCode int

// Close codes. The numbers are part of the protocol: never renumber one.
const (
	CloseNormal             CloseCode = 1000 // the page or the gateway said bye
	CloseUnsupportedData    CloseCode = 1003 // a binary frame where text was expected
	ClosePolicy             CloseCode = 1008 // a bad message or a message out of order
	CloseTooBig             CloseCode = 1009 // a frame over the read limit
	CloseAuthFailed         CloseCode = 4001 // missing or wrong token
	CloseTokenExpired       CloseCode = 4002 // the token is too old
	CloseDeviceRevoked      CloseCode = 4003 // the device was removed (v2)
	CloseTakenOver          CloseCode = 4004 // another place took over
	CloseTooManyViewers     CloseCode = 4005 // the viewer cap is reached
	CloseIdle               CloseCode = 4006 // idle timeout
	CloseMaxLifetime        CloseCode = 4007 // max lifetime reached
	CloseSlowClient         CloseCode = 4008 // the browser could not keep up
	CloseStopped            CloseCode = 4009 // the web side was stopped on the laptop
	CloseSessionEnded       CloseCode = 4010 // the termocode session ended
	CloseUnsupportedVersion CloseCode = 4011 // the page and the binary do not match
	CloseHubGone            CloseCode = 4012 // the UI process went away
)

var closeReasons = map[CloseCode]string{
	CloseNormal:             "bye",
	CloseUnsupportedData:    "text frames only",
	ClosePolicy:             "protocol error",
	CloseTooBig:             "message too big",
	CloseAuthFailed:         "auth failed",
	CloseTokenExpired:       "token expired",
	CloseDeviceRevoked:      "device removed",
	CloseTakenOver:          "taken over",
	CloseTooManyViewers:     "too many viewers",
	CloseIdle:               "idle timeout",
	CloseMaxLifetime:        "max lifetime reached",
	CloseSlowClient:         "too slow",
	CloseStopped:            "stopped on the laptop",
	CloseSessionEnded:       "session ended",
	CloseUnsupportedVersion: "unsupported version",
	CloseHubGone:            "termocode went away",
}

// CloseCodes lists every close code, in numeric order.
func CloseCodes() []CloseCode {
	return []CloseCode{
		CloseNormal, CloseUnsupportedData, ClosePolicy, CloseTooBig,
		CloseAuthFailed, CloseTokenExpired, CloseDeviceRevoked, CloseTakenOver,
		CloseTooManyViewers, CloseIdle, CloseMaxLifetime, CloseSlowClient,
		CloseStopped, CloseSessionEnded, CloseUnsupportedVersion, CloseHubGone,
	}
}

// Reason is the short close reason sent in the close frame ("" for a code
// this package does not know).
func (c CloseCode) Reason() string { return closeReasons[c] }
