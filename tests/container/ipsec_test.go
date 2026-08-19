package container_test

import (
	"strings"
	"testing"
	"time"
)

const (
	ipsecDockerfile = "cmd/ipsec_exporter/Dockerfile"
	ipsecImageTag   = "test-ipsec-exporter"
	ipsecPort       = "9100"
)

func TestIpsecExporter(t *testing.T) {
	skipIfNoDocker(t)

	image := buildImage(t, ipsecDockerfile, ipsecImageTag)
	hostPort := freePort(t)

	containerID := runContainer(t, image, hostPort, ipsecPort,
		"--listen-address=0.0.0.0:"+ipsecPort,
		"--proc-path=/proc",
		"--sys-path=/sys",
		"--vici-socket=/nonexistent",
	)

	baseURL := "http://127.0.0.1:" + hostPort
	waitForHealthy(t, baseURL+"/", 15*time.Second)

	t.Run("metrics_returns_200", func(t *testing.T) {
		status, body := httpGet(t, baseURL+"/metrics")
		if status != 200 {
			t.Fatalf("expected 200 from /metrics, got %d", status)
		}
		// With VICI unavailable, ipsec_up should be 0 but the metrics
		// endpoint must still respond (ContinueOnError).
		if !strings.Contains(body, "ipsec_up") {
			t.Error("/metrics does not contain ipsec_up metric")
		}
	})

	t.Run("metrics_contain_output_without_vici", func(t *testing.T) {
		_, body := httpGet(t, baseURL+"/metrics")
		// Even without VICI, the exporter should emit some metrics
		// (at minimum ipsec_up=0 and network collector metrics).
		if !strings.Contains(body, "ipsec_up 0") {
			t.Error("expected ipsec_up 0 when VICI socket is unavailable")
		}
	})

	t.Run("landing_page", func(t *testing.T) {
		status, body := httpGet(t, baseURL+"/")
		if status != 200 {
			t.Fatalf("expected 200 from /, got %d", status)
		}
		if !strings.Contains(body, "IPsec Exporter") {
			t.Error("landing page does not contain 'IPsec Exporter'")
		}
	})

	t.Run("clean_shutdown", func(t *testing.T) {
		testCleanShutdown(t, containerID)
	})
}

func TestIpsecExporterVersionFlag(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, ipsecDockerfile, ipsecImageTag)
	testVersionFlag(t, image, "ipsec_exporter")
}

func TestIpsecExporterHelpFlag(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, ipsecDockerfile, ipsecImageTag)
	testHelpFlag(t, image)
}

func TestIpsecExporterNoShell(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, ipsecDockerfile, ipsecImageTag)
	testNoShell(t, image)
}

func TestIpsecExporterUser(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, ipsecDockerfile, ipsecImageTag)
	testUser(t, image, "65532")
}
