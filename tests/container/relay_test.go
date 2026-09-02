package container_test

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

const (
	relayDockerfile = "cmd/relay_exporter/Dockerfile"
	relayImageTag   = "test-relay-exporter"
	relayPort       = "9100"
)

func TestRelayExporter(t *testing.T) {
	skipIfNoDocker(t)

	image := buildImage(t, relayDockerfile, relayImageTag)
	hostPort := freePort(t)

	// The relay checks r.RemoteAddr against --allowed-source. When using
	// Docker port mapping, the container sees requests from the Docker
	// gateway IP (e.g. 172.17.0.1), so we set --allowed-source accordingly.
	gwIP := detectDockerHost(t)

	containerID := runContainer(t, image, hostPort, relayPort,
		"--allowed-source="+gwIP,
		"--listen-address=0.0.0.0:"+relayPort,
		"--proxy-timeout=3s",
	)

	base := baseURL(t, hostPort)
	waitForHealthy(t, base+"/health", 30*time.Second)

	t.Run("health_endpoint", func(t *testing.T) {
		status, body := httpGet(t, base+"/health")
		if status != http.StatusOK {
			t.Fatalf("expected 200 from /health, got %d", status)
		}
		if !strings.Contains(body, "ok") {
			t.Errorf("expected 'ok' in /health response, got %q", body)
		}
	})

	t.Run("landing_page", func(t *testing.T) {
		status, body := httpGet(t, base+"/")
		if status != http.StatusOK {
			t.Fatalf("expected 200 from /, got %d", status)
		}
		if !strings.Contains(body, "Relay Exporter") {
			t.Error("landing page does not contain 'Relay Exporter'")
		}
	})

	t.Run("metrics_content_type", func(t *testing.T) {
		// The relay requires ip and port params for /metrics. With valid
		// params it returns text/plain even when the target is unreachable.
		contentType := httpGetContentType(t, base+"/metrics?ip=10.0.0.1&port=9100")
		if !strings.HasPrefix(contentType, "text/plain") {
			t.Errorf("expected text/plain Content-Type from /metrics, got %q", contentType)
		}
	})

	t.Run("metrics_proxy_rfc1918_unreachable", func(t *testing.T) {
		// Target 10.0.0.1:9100 is unreachable, but the relay should still
		// return 200 with relay_target_response=0.
		status, body := httpGet(t, base+"/metrics?ip=10.0.0.1&port=9100")
		if status != http.StatusOK {
			t.Fatalf("expected 200 from /metrics?ip=10.0.0.1&port=9100, got %d", status)
		}
		if !strings.Contains(body, "relay_target_response 0") {
			t.Errorf("expected relay_target_response 0 in body:\n%s", body)
		}
		if !strings.Contains(body, "relay_response 1") {
			t.Errorf("expected relay_response 1 in body:\n%s", body)
		}
	})

	t.Run("metrics_reject_non_rfc1918", func(t *testing.T) {
		// RFC 5737 documentation address -- must be rejected.
		status, _ := httpGetStatus(t, base+"/metrics?ip=203.0.113.1&port=9100")
		if status != http.StatusBadRequest {
			t.Errorf("expected 400 for non-RFC1918 IP, got %d", status)
		}
	})

	t.Run("metrics_reject_port_zero", func(t *testing.T) {
		status, _ := httpGetStatus(t, base+"/metrics?ip=10.0.0.1&port=0")
		if status != http.StatusBadRequest {
			t.Errorf("expected 400 for port=0, got %d", status)
		}
	})

	t.Run("metrics_no_params", func(t *testing.T) {
		status, _ := httpGetStatus(t, base+"/metrics")
		if status != http.StatusBadRequest {
			t.Errorf("expected 400 for /metrics without params, got %d", status)
		}
	})

	t.Run("clean_shutdown", func(t *testing.T) {
		testCleanShutdown(t, containerID)
	})
}

func TestRelayExporterNoAllowedSource(t *testing.T) {
	skipIfNoDocker(t)

	image := buildImage(t, relayDockerfile, relayImageTag)

	// Without --allowed-source the exporter should refuse to start.
	out, err := runContainerForeground(t, image, image)
	if err == nil {
		t.Errorf("expected container to fail without --allowed-source, but it succeeded: %s", out)
	}
	if !strings.Contains(strings.ToLower(out), "allowed-source") {
		t.Errorf("error output should mention --allowed-source: %s", out)
	}
}

func TestRelayExporterVersionFlag(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, relayDockerfile, relayImageTag)
	testVersionFlag(t, image, "relay_exporter")
}

func TestRelayExporterHelpFlag(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, relayDockerfile, relayImageTag)
	testHelpFlag(t, image)
}

func TestRelayExporterNoShell(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, relayDockerfile, relayImageTag)
	testNoShell(t, image)
}

func TestRelayExporterUser(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, relayDockerfile, relayImageTag)
	testUser(t, image, "65532")
}

func TestRelayExporterInvalidFlag(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, relayDockerfile, relayImageTag)
	testInvalidFlag(t, image)
}
