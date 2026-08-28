package container_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryDockerfileHasContainerTest verifies that each exporter Dockerfile
// in cmd/ has at least one corresponding test file in tests/container/.
// This is the obligation coverage assertion for artifacts that cannot be
// instrumented with line coverage: every Dockerfile must be exercised by
// at least one molecule-style container test.
func TestEveryDockerfileHasContainerTest(t *testing.T) {
	cmdDir := filepath.Join(projectRoot, "cmd")
	entries, err := os.ReadDir(cmdDir)
	if err != nil {
		t.Fatalf("reading cmd/: %v", err)
	}

	testDir := filepath.Join(projectRoot, "tests", "container")
	testFiles, err := filepath.Glob(filepath.Join(testDir, "*_test.go"))
	if err != nil {
		t.Fatalf("listing test files: %v", err)
	}

	// Build a set of test file base names for lookup.
	testSet := make(map[string]bool, len(testFiles))
	for _, f := range testFiles {
		testSet[filepath.Base(f)] = true
	}

	var dockerfiles []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		df := filepath.Join(cmdDir, entry.Name(), "Dockerfile")
		if _, err := os.Stat(df); err == nil {
			dockerfiles = append(dockerfiles, entry.Name())
		}
	}

	if len(dockerfiles) == 0 {
		t.Fatal("found no Dockerfiles under cmd/")
	}

	for _, exporter := range dockerfiles {
		// Convention: the test file for "cloudflare_exporter" is
		// "cloudflare_test.go" (drop the _exporter suffix).
		base := strings.TrimSuffix(exporter, "_exporter")
		testFile := base + "_test.go"

		if !testSet[testFile] {
			t.Errorf("cmd/%s/Dockerfile has no container test (expected tests/container/%s)",
				exporter, testFile)
		}
	}

	t.Logf("verified %d Dockerfiles have corresponding container tests", len(dockerfiles))
}
