// Package container_test provides coverage collection support for molecule
// tests. When COVERAGE_MODE=1 is set, these helpers build coverage-instrumented
// container images and mount a GOCOVERDIR volume so that coverage data is
// written when the container exits cleanly via SIGTERM.
package container_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// coverageEnabled returns true when COVERAGE_MODE=1 is set.
func coverageEnabled() bool {
	return os.Getenv("COVERAGE_MODE") == "1"
}

// coverageHostDir returns the host directory where molecule coverage data
// should be written. Defaults to coverage/molecule under the project root.
// Honors GOCOVERDIR_HOST if set.
func coverageHostDir(t *testing.T) string {
	t.Helper()
	if d := os.Getenv("GOCOVERDIR_HOST"); d != "" {
		return d
	}
	return filepath.Join(projectRoot, "coverage", "molecule")
}

// buildCoverageImage builds a Docker image with COVERAGE=1, producing a
// coverage-instrumented binary. The image is tagged with a "-coverage" suffix.
func buildCoverageImage(t *testing.T, dockerfile, baseTag string) string {
	t.Helper()

	tag := baseTag + "-coverage"

	cmd := exec.Command("docker", "build",
		"-f", projectRoot+"/"+dockerfile,
		"-t", tag,
		"--build-arg", "VERSION=test",
		"--build-arg", "COMMIT=test123",
		"--build-arg", "COVERAGE=1",
		projectRoot,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker build (coverage) failed for %s:\n%s\n%v", dockerfile, out, err)
	}

	t.Cleanup(func() {
		_ = exec.Command("docker", "rmi", "-f", tag).Run()
	})
	return tag
}

// runContainerWithCoverage starts a container with GOCOVERDIR mounted so that
// coverage data is flushed on clean shutdown. Returns the container ID and the
// host-side coverage directory path.
func runContainerWithCoverage(t *testing.T, image string, dockerArgs []string, hostPort, containerPort string, entrypointArgs ...string) (string, string) {
	t.Helper()

	covDir := coverageHostDir(t)
	if err := os.MkdirAll(covDir, 0o755); err != nil {
		t.Fatalf("creating coverage directory %s: %v", covDir, err)
	}

	// Append coverage-specific Docker arguments.
	args := make([]string, 0, len(dockerArgs)+4)
	args = append(args, dockerArgs...)
	args = append(args,
		"-v", covDir+":/covdata",
		"-e", "GOCOVERDIR=/covdata",
	)

	containerID := runContainerWithOpts(t, image, args, hostPort, containerPort, entrypointArgs...)
	return containerID, covDir
}

// verifyCoverageData checks that at least one coverage data file was written
// to the given directory. Files produced by go build -cover are named with
// the pattern cov*.
func verifyCoverageData(t *testing.T, covDir string) {
	t.Helper()

	entries, err := os.ReadDir(covDir)
	if err != nil {
		t.Fatalf("reading coverage directory %s: %v", covDir, err)
	}

	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "cov") {
			t.Logf("coverage data found: %s", e.Name())
			return
		}
	}
	t.Errorf("no coverage data files found in %s (entries: %d)", covDir, len(entries))
}
