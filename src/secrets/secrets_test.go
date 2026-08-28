package secrets

import (
	"os"
	"path/filepath"
	"strings"
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

func TestReadSecretFile_RejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	realPath := filepath.Join(dir, "real-secret")
	linkPath := filepath.Join(dir, "link-secret")

	if err := os.WriteFile(realPath, []byte("my-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realPath, linkPath); err != nil {
		t.Fatal(err)
	}

	_, err := ReadSecretFile(linkPath)
	if err == nil {
		t.Fatal("expected error when reading a symlink")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("error should mention symlink, got: %v", err)
	}

	// The real file should still work.
	got, err := ReadSecretFile(realPath)
	if err != nil {
		t.Fatalf("real file should succeed: %v", err)
	}
	if got != "my-secret" {
		t.Errorf("got %q, want %q", got, "my-secret")
	}
}

func TestReadSecretFile_RejectsWorldReadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("my-secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ReadSecretFile(path)
	if err == nil {
		t.Fatal("expected error for world-readable file")
	}
	if !strings.Contains(err.Error(), "mode") {
		t.Errorf("error should mention mode, got: %v", err)
	}
}

func TestReadSecretFile_RejectsGroupReadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("my-secret\n"), 0o640); err != nil {
		t.Fatal(err)
	}

	_, err := ReadSecretFile(path)
	if err == nil {
		t.Fatal("expected error for group-readable file")
	}
	if !strings.Contains(err.Error(), "mode") {
		t.Errorf("error should mention mode, got: %v", err)
	}
}

func TestReadSecretFile_AcceptsStricterPerms(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("my-secret\n"), 0o400); err != nil {
		t.Fatal(err)
	}

	got, err := ReadSecretFile(path)
	if err != nil {
		t.Fatalf("0400 should be accepted: %v", err)
	}
	if got != "my-secret" {
		t.Errorf("got %q, want %q", got, "my-secret")
	}
}

func TestReadSecretFile_Accepts0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret")
	if err := os.WriteFile(path, []byte("exact-perms\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ReadSecretFile(path)
	if err != nil {
		t.Fatalf("0600 should be accepted: %v", err)
	}
	if got != "exact-perms" {
		t.Errorf("got %q, want %q", got, "exact-perms")
	}
}
