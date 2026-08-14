// Package secrets provides utilities for reading credentials from files
// and from OpenBao/Vault KV v2 stores, keeping them out of process
// argument lists and environment variables.
package secrets

import (
	"fmt"
	"os"
	"strings"
)

// ReadSecretFile reads a secret from a file path, trims trailing whitespace.
// Returns an error if the file is missing, unreadable, or empty after trimming.
func ReadSecretFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading secret file: %w", err)
	}
	secret := strings.TrimRight(string(data), " \t\r\n")
	if secret == "" {
		return "", fmt.Errorf("secret file %q is empty after trimming whitespace", path)
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
