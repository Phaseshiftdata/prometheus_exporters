// Package secrets provides utilities for reading credentials from files
// and from OpenBao/Vault KV v2 stores, keeping them out of process
// argument lists and environment variables.
package secrets

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// maxSecretFilePerms is the most permissive file mode allowed for secret
// files.  Group and other bits must be zero (owner-only access).
const maxSecretFilePerms fs.FileMode = 0o600

// ReadSecretFile reads a secret from a file path, trims trailing whitespace.
// Returns an error if the file is missing, unreadable, empty after trimming,
// a symlink, or has permissions more permissive than 0600.
func ReadSecretFile(path string) (string, error) {
	// Resolve to an absolute path to produce clear error messages.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving secret file path: %w", err)
	}

	// Reject symlinks: Lstat does not follow symlinks, so if the mode
	// includes ModeSymlink the path is a symlink.
	linfo, err := os.Lstat(absPath)
	if err != nil {
		return "", fmt.Errorf("reading secret file: %w", err)
	}
	if linfo.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("secret file %q is a symlink; use the real path for security", absPath)
	}

	// Check file permissions — must not be group- or world-readable.
	perm := linfo.Mode().Perm()
	if perm&^maxSecretFilePerms != 0 {
		return "", fmt.Errorf("secret file %q has mode %04o; must be %04o or stricter", absPath, perm, maxSecretFilePerms)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", fmt.Errorf("reading secret file: %w", err)
	}
	secret := strings.TrimRight(string(data), " \t\r\n")
	if secret == "" {
		return "", fmt.Errorf("secret file %q is empty after trimming whitespace", absPath)
	}
	return secret, nil
}

// ValidateSecretSources checks that at most one of the three secret
// sources (direct value, file, OpenBao reference) is set for a given
// configuration parameter. It returns an error if more than one is
// provided.
func ValidateSecretSources(flagName, value, file, openbaoRef string) error {
	count := 0
	if value != "" {
		count++
	}
	if file != "" {
		count++
	}
	if openbaoRef != "" {
		count++
	}
	if count > 1 {
		return fmt.Errorf("--%s, --%s-file, and --%s-openbao are mutually exclusive", flagName, flagName, flagName)
	}
	return nil
}
