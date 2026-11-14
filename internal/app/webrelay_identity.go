package app

import (
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/amin-jalali/termocode/internal/relay"
	"github.com/amin-jalali/termocode/internal/relaytun"
	"github.com/amin-jalali/termocode/internal/secrets"
)

// The relay identity in the secret store (web_relay.go, ADR 0022). Kept
// outside the web_*.go files: the hub's files never import the secret
// store (TestWebFilesNeverImportSecrets), as with the TOTP bridge.

// webRelayKeyName is the secret store entry: {"key": <base64url private
// key>, "room": <room id>}.
const webRelayKeyName = "web.relay.key"

// webRelayStore is the secret store the identity lives in; tests swap it.
var webRelayStore = func() (secrets.Store, error) { return configureSecretStore() }

// webRelayID is the relay identity.
type webRelayID struct {
	Key  relaytun.Keypair
	Room string
}

type webRelayStored struct {
	Key  string `json:"key"`
	Room string `json:"room"`
}

// webRelayIdentity loads the relay identity, or makes and stores one.
func webRelayIdentity() (webRelayID, error) {
	s, err := webRelayStore()
	if err != nil {
		return webRelayID{}, err
	}
	if s == nil || s.Locked() {
		return webRelayID{}, errors.New("the secret store is locked")
	}
	if v, ok, err := s.Get(webRelayKeyName); err != nil {
		return webRelayID{}, err
	} else if ok {
		var st webRelayStored
		var b []byte
		derr := json.Unmarshal([]byte(v), &st)
		if derr == nil {
			b, derr = base64.RawURLEncoding.DecodeString(st.Key)
		}
		if derr == nil && len(b) == 32 && relay.ValidRoom(st.Room) {
			kp, err := relaytun.KeypairFromPrivate([32]byte(b))
			if err == nil {
				return webRelayID{Key: kp, Room: st.Room}, nil
			}
		}
		// A damaged entry: a new identity (paired phones pair again).
	}
	kp, err := relaytun.GenerateKeypair()
	if err != nil {
		return webRelayID{}, err
	}
	room, err := relay.NewRoomID()
	if err != nil {
		return webRelayID{}, err
	}
	b, _ := json.Marshal(webRelayStored{Key: base64.RawURLEncoding.EncodeToString(kp.Private[:]), Room: room})
	if err := s.Set(webRelayKeyName, string(b)); err != nil {
		return webRelayID{}, err
	}
	return webRelayID{Key: kp, Room: room}, nil
}
