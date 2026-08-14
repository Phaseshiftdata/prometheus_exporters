// Package secrets provides utilities for reading credentials from files,
// keeping them out of process argument lists and environment variables.
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
