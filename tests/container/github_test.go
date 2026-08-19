package container_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

	image := buildImage(t, githubDockerfile, githubImageTag)
	keyPath := generateTestRSAKey(t)
	hostPort := freePort(t)

	// Run in metrics-only mode (no database URL) with a test RSA key.
	// Use hostPath to translate the key path for Docker-in-Docker environments.
	containerID := runContainerWithOpts(t, image,
		[]string{
			"-v", hostPath(t, keyPath) + ":/tmp/test-key.pem:ro",
		},
		hostPort, githubPort,
		"--listen-address=0.0.0.0:"+githubPort,
		"--github-app-id=12345",
		"--github-install-id=67890",
		"--github-key-file=/tmp/test-key.pem",
		"--org=test-org",
	)

	base := baseURL(t, hostPort)
	waitForHealthy(t, base+"/metrics", 30*time.Second)

	t.Run("metrics_returns_200", func(t *testing.T) {
		status, _ := httpGet(t, base+"/metrics")
		if status != http.StatusOK {
			t.Fatalf("expected 200 from /metrics, got %d", status)
		}
	})

	t.Run("landing_page", func(t *testing.T) {
		status, body := httpGet(t, base+"/")
		if status != http.StatusOK {
			t.Fatalf("expected 200 from /, got %d", status)
		}
		if !strings.Contains(body, "GitHub Exporter") {
			t.Error("landing page does not contain 'GitHub Exporter'")
		}
	})

	t.Run("clean_shutdown", func(t *testing.T) {
		testCleanShutdown(t, containerID)
	})
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
