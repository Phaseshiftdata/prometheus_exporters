package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadSecretFile_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("my-secret-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ReadSecretFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "my-secret-token" {
		t.Errorf("got %q, want %q", got, "my-secret-token")
	}
}

func TestReadSecretFile_TrailingWhitespace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("  token-value  \r\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ReadSecretFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only trailing whitespace is trimmed; leading spaces are preserved.
	if got != "  token-value" {
		t.Errorf("got %q, want %q", got, "  token-value")
	}
}

func TestReadSecretFile_MissingFile(t *testing.T) {
	_, err := ReadSecretFile("/nonexistent/path/to/secret")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadSecretFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := ReadSecretFile(path)
	if err == nil {
		t.Fatal("expected error for empty file")
	}
}

func TestReadSecretFile_WhitespaceOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("  \n\t\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := ReadSecretFile(path)
	if err == nil {
		t.Fatal("expected error for whitespace-only file")
	}
}
