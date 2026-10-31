package update

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/blake2b"
)

// SignatureName is the release file with the minisign signature of
// checksums.txt (made by the maintainer's offline key).
const SignatureName = ChecksumsName + ".minisig"

// releasePublicKey is the minisign public key of the releases: the
// base64 line of the .pub file ("RWQ..."). PLACEHOLDER: it is empty
// until the maintainer sets the key, and an empty key switches the
// check off, so updates keep working. Once a key is set, an update is
// refused unless checksums.txt.minisig verifies. See
// docs/reference/release-signing.md. A var so tests can set a key.
var releasePublicKey = ""

const maxSignature = 4096

var errNoSignature = errors.New("this release has no signature (" + SignatureName + "); the update is refused")

// verifySignature checks the signature of the checksums when a release
// key is set. Any missing or bad signature refuses the update.
func (c Client) verifySignature(ctx context.Context, rel Release, sums []byte) error {
	if releasePublicKey == "" {
		return nil
	}
	var sig *Asset
	for i := range rel.Assets {
		if rel.Assets[i].Name == SignatureName {
			sig = &rel.Assets[i]
		}
	}
	if sig == nil {
		return errNoSignature
	}
	data, err := c.readSmall(ctx, sig.URL, maxSignature)
	if err != nil {
		return fmt.Errorf("could not download the signature: %w", err)
	}
	return VerifyMinisign(releasePublicKey, sums, data)
}

// VerifyMinisign checks a minisign signature file (legacy "Ed" or
// pre-hashed "ED") of msg against a base64 public key.
func VerifyMinisign(pubKey string, msg, sigFile []byte) error {
	pk, err := base64.StdEncoding.DecodeString(strings.TrimSpace(pubKey))
	if err != nil || len(pk) != 42 || pk[0] != 'E' || pk[1] != 'd' {
		return errors.New("the release public key built into termocode is not valid")
	}
	lines := strings.Split(strings.ReplaceAll(string(sigFile), "\r", ""), "\n")
	if len(lines) < 2 {
		return errors.New("the release signature is not valid")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil || len(raw) != 74 {
		return errors.New("the release signature is not valid")
	}
	if !bytes.Equal(raw[2:10], pk[2:10]) {
		return errors.New("the release was signed with another key; the update is refused")
	}
	signed := msg
	switch string(raw[:2]) {
	case "Ed":
	case "ED":
		h := blake2b.Sum512(msg)
		signed = h[:]
	default:
		return errors.New("the release signature uses an unknown method")
	}
	if !ed25519.Verify(ed25519.PublicKey(pk[10:]), signed, raw[10:]) {
		return errors.New("the release signature does not match checksums.txt; the update is refused")
	}
	return nil
}
