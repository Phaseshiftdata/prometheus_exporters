package version

import "testing"

func TestDefaults(t *testing.T) {
	if Version != "dev" {
		t.Errorf("expected default Version 'dev', got %q", Version)
	}
	if GitCommit != "unknown" {
		t.Errorf("expected default GitCommit 'unknown', got %q", GitCommit)
	}
	if BuildDate != "unknown" {
		t.Errorf("expected default BuildDate 'unknown', got %q", BuildDate)
	}
}
