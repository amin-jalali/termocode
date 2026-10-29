package update

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// wantSum finds name's SHA-256 in sha256sum output ("<hex>  <name>" or
// "<hex> *<name>"); "" when it is not listed.
func wantSum(sums []byte, name string) string {
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		if strings.TrimPrefix(fields[1], "*") != name || len(fields[0]) != sha256.Size*2 {
			continue
		}
		// Only a real hex sum counts; anything else could never match.
		if _, err := hex.DecodeString(fields[0]); err == nil {
			return strings.ToLower(fields[0])
		}
	}
	return ""
}

// VerifyChecksum checks the SHA-256 of the file at path against name's
// line in sums (the release's checksums.txt). A missing line is an
// error, never a pass.
func VerifyChecksum(path, name string, sums []byte) error {
	want := wantSum(sums, name)
	if want == "" {
		return fmt.Errorf("%s is not listed in %s", name, ChecksumsName)
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		return fmt.Errorf("checksum mismatch for %s: the download does not match %s", name, ChecksumsName)
	}
	return nil
}
