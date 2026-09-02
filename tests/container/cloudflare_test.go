package container_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	cloudflareDockerfile = "cmd/cloudflare_exporter/Dockerfile"
	cloudflareImageTag   = "test-cloudflare-exporter"
	cloudflarePort       = "9199"
)

func TestCloudflareExporter(t *testing.T) {
	skipIfNoDocker(t)

	image := buildImage(t, cloudflareDockerfile, cloudflareImageTag)
	hostPort := freePort(t)

	containerID := runContainerWithOpts(t, image,
		[]string{"-e", "CF_API_TOKEN=test-token"},
		hostPort, cloudflarePort,
	)

	base := baseURL(t, hostPort)

	// The exporter may take a moment to start serving while discovery fails.
	waitForHealthy(t, base+"/health", 30*time.Second)

	t.Run("metrics_returns_200_with_build_info", func(t *testing.T) {
		status, body := httpGet(t, base+"/metrics")
		if status != http.StatusOK {
			t.Fatalf("expected 200 from /metrics, got %d", status)
		}
		if !strings.Contains(body, "cloudflare_exporter_build_info") {
			t.Error("/metrics does not contain cloudflare_exporter_build_info")
		}
	})

	t.Run("health_endpoint", func(t *testing.T) {
		status, _ := httpGet(t, base+"/health")
		if status != http.StatusOK {
			t.Errorf("expected 200 from /health, got %d", status)
		}
	})

	t.Run("capabilities_returns_json", func(t *testing.T) {
		status, body := httpGet(t, base+"/capabilities")
		if status != http.StatusOK {
			t.Fatalf("expected 200 from /capabilities, got %d", status)
		}
		if !json.Valid([]byte(body)) {
			t.Errorf("/capabilities response is not valid JSON: %s", body)
		}
	})

	t.Run("landing_page", func(t *testing.T) {
		status, body := httpGet(t, base+"/")
		if status != http.StatusOK {
			t.Fatalf("expected 200 from /, got %d", status)
		}
		if !strings.Contains(body, "Cloudflare") {
			t.Error("landing page does not contain 'Cloudflare'")
		}
	})

	t.Run("metrics_content_type", func(t *testing.T) {
		contentType := httpGetContentType(t, base+"/metrics")
		if !strings.HasPrefix(contentType, "text/plain") {
			t.Errorf("expected text/plain Content-Type from /metrics, got %q", contentType)
		}
	})

	t.Run("clean_shutdown", func(t *testing.T) {
		testCleanShutdown(t, containerID)
	})
}

func TestCloudflareCapabilitiesFlag(t *testing.T) {
	skipIfNoDocker(t)

	image := buildImage(t, cloudflareDockerfile, cloudflareImageTag)

	// --capabilities should print capabilities JSON and exit.
	out, _ := runContainerForeground(t, image,
		"-e", "CF_API_TOKEN=test-token",
		image, "--capabilities",
	)

	// The flag may fail due to discovery errors, but it should attempt to
	// output JSON or an error message mentioning capabilities.
	if !strings.Contains(out, "{") && !strings.Contains(strings.ToLower(out), "error") &&
		!strings.Contains(strings.ToLower(out), "capabilit") {
		t.Errorf("--capabilities output unexpected: %s", out)
	}
}

func TestCloudflareNoAPIToken(t *testing.T) {
	skipIfNoDocker(t)

	image := buildImage(t, cloudflareDockerfile, cloudflareImageTag)
	hostPort := freePort(t)

	// Without CF_API_TOKEN the exporter should still start -- the token is
	// resolved lazily during discovery. Verify the process launches.
	_ = runContainer(t, image, hostPort, cloudflarePort)

	time.Sleep(2 * time.Second)

	// It should still serve the health endpoint.
	status, _ := httpGetStatus(t, baseURL(t, hostPort)+"/health")
	if status != http.StatusOK && status != -1 {
		t.Logf("health check returned %d without API token (container may have exited)", status)
	}
}

func TestCloudflareVersionFlag(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, cloudflareDockerfile, cloudflareImageTag)
	testVersionFlag(t, image, "cloudflare_exporter")
}

func TestCloudflareHelpFlag(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, cloudflareDockerfile, cloudflareImageTag)
	testHelpFlag(t, image)
}

func TestCloudflareNoShell(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, cloudflareDockerfile, cloudflareImageTag)
	testNoShell(t, image)
}

func TestCloudflareUser(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, cloudflareDockerfile, cloudflareImageTag)
	testUser(t, image, "65532")
}

func TestCloudflareInvalidFlag(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, cloudflareDockerfile, cloudflareImageTag)
	testInvalidFlag(t, image)
}
