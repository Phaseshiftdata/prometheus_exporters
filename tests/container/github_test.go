package container_test

import (
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
