package container_test

import (
	"strings"
	"testing"
	"time"
)

const (
	networkDockerfile = "cmd/network_exporter/Dockerfile"
	networkImageTag   = "test-network-exporter"
	networkPort       = "9100"
)

func TestNetworkExporter(t *testing.T) {
	skipIfNoDocker(t)

	image := buildImage(t, networkDockerfile, networkImageTag)
	hostPort := freePort(t)

	containerID := runContainerWithOpts(t, image,
		[]string{"--cap-add=NET_ADMIN", "--cap-add=DAC_READ_SEARCH"},
		hostPort, networkPort,
		"--listen-address=0.0.0.0:"+networkPort,
		"--proc-path=/proc",
		"--sys-path=/sys",
	)

	baseURL := "http://127.0.0.1:" + hostPort
	waitForHealthy(t, baseURL+"/", 15*time.Second)

	t.Run("metrics_returns_200", func(t *testing.T) {
		status, _ := httpGet(t, baseURL+"/metrics")
		if status != 200 {
			t.Fatalf("expected 200 from /metrics, got %d", status)
		}
	})

	t.Run("landing_page", func(t *testing.T) {
		status, body := httpGet(t, baseURL+"/")
		if status != 200 {
			t.Fatalf("expected 200 from /, got %d", status)
		}
		if !strings.Contains(body, "Network Exporter") {
			t.Error("landing page does not contain 'Network Exporter'")
		}
	})

	t.Run("metrics_content_type", func(t *testing.T) {
		contentType := httpGetContentType(t, baseURL+"/metrics")
		if !strings.HasPrefix(contentType, "text/plain") {
			t.Errorf("expected text/plain Content-Type from /metrics, got %q", contentType)
		}
	})

	t.Run("clean_shutdown", func(t *testing.T) {
		testCleanShutdown(t, containerID)
	})
}

func TestNetworkExporterVersionFlag(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, networkDockerfile, networkImageTag)
	testVersionFlag(t, image, "network_exporter", "--cap-add=NET_ADMIN", "--cap-add=DAC_READ_SEARCH")
}

func TestNetworkExporterHelpFlag(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, networkDockerfile, networkImageTag)
	testHelpFlag(t, image, "--cap-add=NET_ADMIN", "--cap-add=DAC_READ_SEARCH")
}

func TestNetworkExporterNoShell(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, networkDockerfile, networkImageTag)
	testNoShell(t, image)
}

func TestNetworkExporterUser(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, networkDockerfile, networkImageTag)
	testUser(t, image, "65532")
}

func TestNetworkExporterInvalidFlag(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, networkDockerfile, networkImageTag)
	testInvalidFlag(t, image, "--cap-add=NET_ADMIN", "--cap-add=DAC_READ_SEARCH")
}
