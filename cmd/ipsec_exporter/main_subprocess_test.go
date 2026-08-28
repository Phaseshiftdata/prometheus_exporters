package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestMainHelp exercises main() via the --help flag by re-executing the test
// binary as a subprocess. When the subprocess detects BE_MAIN=1 it calls
// main() directly, so coverage is recorded by the Go test instrumentation.
func TestMainHelp(t *testing.T) {
	if os.Getenv("BE_MAIN") == "1" {
		os.Args = []string{"ipsec_exporter", "--help"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainHelp$")
	cmd.Env = append(os.Environ(), "BE_MAIN=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--help subprocess failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ipsec_exporter") {
		t.Errorf("expected 'ipsec_exporter' in --help output, got:\n%s", out)
	}
}

// TestMainVersion exercises main() via the --version flag.
func TestMainVersion(t *testing.T) {
	if os.Getenv("BE_MAIN") == "1" {
		os.Args = []string{"ipsec_exporter", "--version"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainVersion$")
	cmd.Env = append(os.Environ(), "BE_MAIN=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--version subprocess failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ipsec_exporter") {
		t.Errorf("expected 'ipsec_exporter' in --version output, got:\n%s", out)
	}
}

// TestMainInvalidFlag exercises main() with an unknown flag, expecting a
// non-zero exit code.
func TestMainInvalidFlag(t *testing.T) {
	if os.Getenv("BE_MAIN") == "1" {
		os.Args = []string{"ipsec_exporter", "--bogus-flag-that-does-not-exist"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainInvalidFlag$")
	cmd.Env = append(os.Environ(), "BE_MAIN=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected non-zero exit for invalid flag, got:\n%s", out)
	}
}
