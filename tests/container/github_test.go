package container_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

const (
	githubDockerfile = "cmd/github_exporter/Dockerfile"
	githubImageTag   = "test-github-exporter"
	githubPort       = "9102"
)

// generateTestRSAKey creates a temporary PEM-encoded RSA private key file
// under the project root (which is on a filesystem shared with the Docker
// daemon) and returns its path. The file is removed when the test completes.
func generateTestRSAKey(t *testing.T) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating RSA key: %v", err)
	}

	// Use the project root for temp files so they are accessible to the
	// Docker daemon in Docker-in-Docker environments.
	dir, err := os.MkdirTemp(projectRoot, ".molecule-tmp-")
	if err != nil {
		t.Fatalf("creating temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "test-key.pem")

	pemData := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})

	if err := os.WriteFile(path, pemData, 0600); err != nil {
		t.Fatalf("writing test RSA key: %v", err)
	}
	return path
}

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
