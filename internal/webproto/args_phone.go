package webproto

// This phone (MOBUX-20): what a paired phone sees and changes of its own
// record on the laptop. The gateway answers these ops itself (it holds
// the device list); nothing here is a secret.

// PhonePrefsArgs is what the phone chose on its This phone screen: OS
// alerts on or off, quiet hours (both empty: none), the phone's offset
// from UTC so the laptop can tell when the quiet hours are (a push sender
// honours them too), and the app lock (the phone asks for its own
// fingerprint, face or PIN when the app opens).
type PhonePrefsArgs struct {
	Alerts       bool   `json:"alerts"`
	QuietFrom    string `json:"quiet_from,omitempty"`
	QuietTo      string `json:"quiet_to,omitempty"`
	UTCOffsetMin int    `json:"utc_offset_min,omitempty"`
	Lock         bool   `json:"lock,omitempty"`
}

// MaxUTCOffsetMin bounds PhonePrefsArgs.UTCOffsetMin (UTC−14:00..+14:00).
const MaxUTCOffsetMin = 14 * 60

// validClock is "HH:MM" on a 24-hour clock.
func validClock(s string) bool {
	if len(s) != 5 || s[2] != ':' {
		return false
	}
	for _, i := range []int{0, 1, 3, 4} {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	h, m := int(s[0]-'0')*10+int(s[1]-'0'), int(s[3]-'0')*10+int(s[4]-'0')
	return h < 24 && m < 60
}

func (a *PhonePrefsArgs) validate(int) error {
	if (a.QuietFrom == "") != (a.QuietTo == "") {
		return invalid("quiet hours need a start and an end")
	}
	if a.QuietFrom != "" && (!validClock(a.QuietFrom) || !validClock(a.QuietTo) || a.QuietFrom == a.QuietTo) {
		return invalid("bad quiet hours")
	}
	if a.UTCOffsetMin < -MaxUTCOffsetMin || a.UTCOffsetMin > MaxUTCOffsetMin {
		return invalid("bad utc offset")
	}
	return nil
}

// PhoneLaptop is one laptop this phone is paired with. The page knows
// only the laptop that serves it; the app (MOB-21) keeps the others.
type PhoneLaptop struct {
	Name string `json:"name"`
	This bool   `json:"this,omitempty"`
}

// PhoneInfo is the answer to phone.get: this device's own record.
// Times are unix milliseconds (0: never).
type PhoneInfo struct {
	Device      string         `json:"device"`
	Name        string         `json:"name"`
	Role        Role           `json:"role"`
	Guest       bool           `json:"guest,omitempty"`
	Passkey     bool           `json:"passkey"`
	NeedsRepair bool           `json:"needs_repair,omitempty"`
	Remembered  bool           `json:"remembered,omitempty"`
	PairedMs    int64          `json:"paired_ms"`
	LastSeenMs  int64          `json:"last_seen_ms,omitempty"`
	Prefs       PhonePrefsArgs `json:"prefs"`
	Laptops     []PhoneLaptop  `json:"laptops"`
}

func init() { registerResult(OpPhoneGet, PhoneInfo{}) }
