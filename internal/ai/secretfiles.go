package ai

import (
	"path/filepath"
	"strings"
)

// SecretFile reports whether path names a file that holds the user's
// secrets: the private HTTP env file, .env files (database passwords are
// read from them), git and network credentials and SSH private keys. The
// AI's file tools do not read them, so a value never reaches the model;
// the user opens them. Example files (.env.example …) are not secret.
func SecretFile(path string) bool {
	name := strings.ToLower(filepath.Base(filepath.ToSlash(path)))
	switch name {
	case "http-client.private.env.json", ".env", ".git-credentials", ".netrc", ".pgpass":
		return true
	}
	if rest, ok := strings.CutPrefix(name, ".env."); ok {
		switch rest {
		case "example", "sample", "template", "dist":
			return false
		}
		return true
	}
	for _, k := range []string{"id_rsa", "id_ed25519", "id_ecdsa", "id_dsa"} {
		if name == k || (strings.HasPrefix(name, k+"_") && !strings.HasSuffix(name, ".pub")) {
			return true
		}
	}
	return false
}

// errSecretFile is what read_file says about a SecretFile.
func errSecretFile(rel string) error {
	return &secretFileError{rel}
}

type secretFileError struct{ rel string }

func (e *secretFileError) Error() string {
	return e.rel + " holds the user's secrets, so it is not read for the AI; ask the user for what you need (names, not values)"
}
