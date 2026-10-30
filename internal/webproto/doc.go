// Package webproto is the wire protocol between the web UI in the browser,
// the web gateway process and the web hub inside the UI process (ADR 0020).
//
// It is pure data: message types, topic names, close codes, the per-op
// policy table and the limits. It does no I/O, so the gateway, the hub,
// the tests and the type generator can all import it.
//
// Browser messages are decoded strictly: an unknown type or field, a
// duplicate key, invalid UTF-8 or an oversized frame is refused. Server
// messages are decoded tolerantly: unknown fields are ignored, so new
// optional fields can be added without breaking older readers.
package webproto

// Version is the protocol version a browser sends in hello.v.
const Version = 2

// Subprotocol is the WebSocket subprotocol of this version.
const Subprotocol = "termocode.v2"
