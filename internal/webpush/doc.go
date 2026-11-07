// Package webpush sends Web Push messages (MOB-30) to the paired phones
// of the web panel, with no cloud of ours: the laptop signs each push
// with its own VAPID key (RFC 8292) and encrypts it for the phone's
// browser (RFC 8291, aes128gcm), and the phone's own push service (the
// browser vendor's) carries the ciphertext.
//
// What a push may say is fixed and small: nothing, a count of the things
// that wait, or the count and a kind from a closed list (Payload). Never
// a title, a path, a command or a word the AI wrote, so a locked screen
// shows nothing private. The phone's service worker turns it into one of
// its own fixed texts.
//
// Pure Go, standard library only. The VAPID private key and the phone's
// subscription are kept by the caller in the secret store
// (internal/secrets); this package never stores, logs or returns them in
// an error. An endpoint is a capability URL: it goes only to the request.
package webpush
