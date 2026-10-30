package webproto

import "testing"

func TestCloseCodesKnownAndShort(t *testing.T) {
	want := map[CloseCode]bool{
		CloseNormal: true, CloseUnsupportedData: true, ClosePolicy: true, CloseTooBig: true,
		CloseAuthFailed: true, CloseTokenExpired: true, CloseDeviceRevoked: true,
		CloseTakenOver: true, CloseTooManyViewers: true, CloseIdle: true, CloseMaxLifetime: true,
		CloseSlowClient: true, CloseStopped: true, CloseSessionEnded: true,
		CloseUnsupportedVersion: true, CloseHubGone: true,
	}
	seen := map[CloseCode]bool{}
	for _, c := range CloseCodes() {
		if seen[c] {
			t.Fatalf("code %d listed twice", c)
		}
		seen[c] = true
		if !want[c] {
			t.Errorf("code %d is not expected", c)
		}
		r := c.Reason()
		if r == "" {
			t.Errorf("code %d has no reason", c)
		}
		// A WebSocket close reason must fit in 123 bytes.
		if len(r) > 123 {
			t.Errorf("code %d reason is %d bytes", c, len(r))
		}
		if c > 4999 {
			t.Errorf("code %d outside the private range", c)
		}
	}
	for c := range want {
		if !seen[c] {
			t.Errorf("code %d missing from CloseCodes()", c)
		}
	}
	if CloseHubGone != 4012 || CloseUnsupportedVersion != 4011 || CloseSlowClient != 4008 {
		t.Fatal("close code numbers changed; they are part of the protocol")
	}
	if CloseCode(4999).Reason() != "" {
		t.Fatal("unknown code must have no reason")
	}
}
