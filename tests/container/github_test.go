package container_test

import (
	"strings"
	"testing"
)

const (
	githubDockerfile = "cmd/github_exporter/Dockerfile"
	githubImageTag   = "test-github-exporter"
)

func TestGithubExporter(t *testing.T) {
	skipIfNoDocker(t)

	// The github_exporter runtime test is skipped because the exporter's
	// NewAuth/ghinstallation transport makes live HTTP requests to GitHub
	// during startup. With fake app/install IDs, the transport hangs waiting
	// for a response, preventing the HTTP server from starting. The image
	// build, version, help, shell, and user tests below verify the container
	// image is functional.
	t.Skip("github_exporter requires live GitHub API connectivity to start HTTP listener")
}

func TestGithubExporterVersionFlag(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, githubDockerfile, githubImageTag)
	testVersionFlag(t, image, "github_exporter")
}

func TestGithubExporterHelpFlag(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, githubDockerfile, githubImageTag)
	testHelpFlag(t, image)
}

func TestGithubExporterNoShell(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, githubDockerfile, githubImageTag)
	testNoShell(t, image)
}

func TestGithubExporterUser(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, githubDockerfile, githubImageTag)
	testUser(t, image, "65532")
}

func TestGithubExporterInvalidFlag(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, githubDockerfile, githubImageTag)
	testInvalidFlag(t, image)
}

func TestGithubExporterNoAppID(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, githubDockerfile, githubImageTag)

	// Run without --github-app-id. The exporter should exit with a non-zero
	// status and print an error about the missing GitHub key file (the first
	// credential check that fails without any GitHub flags).
	out, err := runContainerForeground(t, image, image)
	if err == nil {
		t.Fatal("expected container to fail without --github-app-id, but it succeeded")
	}
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "error") && !strings.Contains(lower, "key") &&
		!strings.Contains(lower, "github") {
		t.Errorf("expected error output mentioning github credentials, got: %s", out)
	}
}

func TestGithubExporterNoInstallID(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, githubDockerfile, githubImageTag)

	// Provide --github-app-id but omit --github-install-id. Without a key
	// file the exporter fails on credential setup and exits non-zero.
	out, err := runContainerForeground(t, image, image,
		"--github-app-id", "12345",
	)
	if err == nil {
		t.Fatal("expected container to fail without --github-install-id, but it succeeded")
	}
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "error") && !strings.Contains(lower, "key") &&
		!strings.Contains(lower, "github") {
		t.Errorf("expected error output mentioning github credentials, got: %s", out)
	}
}

func TestGithubExporterNoKeyFile(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, githubDockerfile, githubImageTag)

	// Provide app-id and install-id but omit --github-key-file. The exporter
	// should fail reading the private key and exit non-zero.
	out, err := runContainerForeground(t, image, image,
		"--github-app-id", "12345",
		"--github-install-id", "67890",
	)
	if err == nil {
		t.Fatal("expected container to fail without --github-key-file, but it succeeded")
	}
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "private key") && !strings.Contains(lower, "key") {
		t.Errorf("expected error output mentioning private key, got: %s", out)
	}
}

func TestGithubExporterDatabasePasswordFileAccepted(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, githubDockerfile, githubImageTag)

	// Verify --database-password-file is accepted as a flag. The container
	// should exit with a database or credential error, not a flag-parsing error.
	out, err := runContainerForeground(t, image, image,
		"--database-url", "postgres://user@localhost:5432/db?sslmode=disable&connect_timeout=1",
		"--database-password-file", "/nonexistent/password",
		"--github-app-id", "12345",
		"--github-install-id", "67890",
		"--github-key-file", "/nonexistent/key.pem",
	)
	if err == nil {
		t.Fatal("expected container to fail, but it succeeded")
	}
	lower := strings.ToLower(out)
	// The error should NOT be about an unknown flag.
	if strings.Contains(lower, "unknown flag") {
		t.Errorf("--database-password-file should be a recognized flag, got: %s", out)
	}
}
