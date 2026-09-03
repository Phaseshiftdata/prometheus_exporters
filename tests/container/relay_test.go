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

	t.Run("metrics_proxy_private_unreachable", func(t *testing.T) {
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

	t.Run("metrics_reject_non_private", func(t *testing.T) {
		// RFC 5737 documentation address -- must be rejected.
		status, _ := httpGetStatus(t, base+"/metrics?ip=203.0.113.1&port=9100")
		if status != http.StatusBadRequest {
			t.Errorf("expected 400 for public IP, got %d", status)
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

	t.Run("metrics_missing_ip_param", func(t *testing.T) {
		// Providing port but no ip should return 400.
		status, _ := httpGetStatus(t, base+"/metrics?port=9100")
		if status != http.StatusBadRequest {
			t.Errorf("expected 400 for /metrics?port=9100 (missing ip), got %d", status)
		}
	})

	t.Run("metrics_invalid_port_param", func(t *testing.T) {
		// Non-numeric port should return 400.
		status, _ := httpGetStatus(t, base+"/metrics?ip=10.0.0.1&port=abc")
		if status != http.StatusBadRequest {
			t.Errorf("expected 400 for port=abc, got %d", status)
		}
	})

	t.Run("metrics_reject_public_ip", func(t *testing.T) {
		// Public IP 8.8.8.8 must be rejected as non-private.
		status, _ := httpGetStatus(t, base+"/metrics?ip=8.8.8.8&port=9100")
		if status != http.StatusBadRequest {
			t.Errorf("expected 400 for public IP 8.8.8.8, got %d", status)
		}
	})

	t.Run("host_endpoint_exists", func(t *testing.T) {
		// /host endpoint should exist and return relay metrics even when
		// the target is unreachable.
		status, body := httpGet(t, base+"/host?ip=10.0.0.1&port=9100")
		if status != http.StatusOK {
			t.Fatalf("expected 200 from /host?ip=10.0.0.1&port=9100, got %d", status)
		}
		if !strings.Contains(body, "relay_response 1") {
			t.Errorf("expected relay_response 1 in /host response:\n%s", body)
		}
	})

	t.Run("cadvisor_endpoint_exists", func(t *testing.T) {
		// /cadvisor endpoint should exist and return relay metrics even
		// when the target is unreachable.
		status, body := httpGet(t, base+"/cadvisor?ip=10.0.0.1&port=9100")
		if status != http.StatusOK {
			t.Fatalf("expected 200 from /cadvisor?ip=10.0.0.1&port=9100, got %d", status)
		}
		if !strings.Contains(body, "relay_response 1") {
			t.Errorf("expected relay_response 1 in /cadvisor response:\n%s", body)
		}
	})

	t.Run("source_ip_filtering", func(t *testing.T) {
		// Start a second container with a different --allowed-source so
		// that requests from the Docker host are rejected.
		hostPort2 := freePort(t)
		runContainer(t, image, hostPort2, relayPort,
			"--allowed-source=192.0.2.1",
			"--listen-address=0.0.0.0:"+relayPort,
			"--proxy-timeout=3s",
		)
		base2 := baseURL(t, hostPort2)
		waitForHealthy(t, base2+"/health", 30*time.Second)

		// Our request comes from the Docker gateway IP, which does not
		// match 192.0.2.1, so we expect 403 Forbidden.
		status, _ := httpGetStatus(t, base2+"/metrics?ip=10.0.0.1&port=9100")
		if status != http.StatusForbidden {
			t.Errorf("expected 403 from unauthorized source IP, got %d", status)
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
