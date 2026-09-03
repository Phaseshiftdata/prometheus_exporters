package container_test

import (
	"os/exec"
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

	containerID := runContainerWithOpts(t, image,
		[]string{"--cap-add=NET_ADMIN", "--cap-add=DAC_READ_SEARCH"},
		hostPort, ipsecPort,
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

	t.Run("network_metric_families_present", func(t *testing.T) {
		_, body := httpGet(t, baseURL+"/metrics")
		// IPsec exporter is a superset of network_exporter; all network
		// metric families must be present.
		families := []struct {
			name string
			alts []string
		}{
			{"network_arp", []string{"network_arp_entry", "network_arp_entries_truncated"}},
			{"network_interface_type", []string{"network_interface_type"}},
			{"network_graph", []string{"network_graph_edge", "network_graph_edges_truncated"}},
			{"network_port", []string{"network_port_connections", "network_port_listen"}},
			{"network_firewall_collector_up", []string{"network_firewall_collector_up"}},
		}
		for _, fam := range families {
			found := false
			for _, alt := range fam.alts {
				if strings.Contains(body, alt) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("metric family %s not found; expected one of %v", fam.name, fam.alts)
			}
		}
	})

	t.Run("ipsec_up_metric_present", func(t *testing.T) {
		_, body := httpGet(t, baseURL+"/metrics")
		if !strings.Contains(body, "ipsec_up") {
			t.Error("ipsec_up metric not found in /metrics output")
		}
	})

	t.Run("graceful_degradation_without_vici", func(t *testing.T) {
		_, body := httpGet(t, baseURL+"/metrics")
		// Without a VICI socket, network metrics must still be served.
		if !strings.Contains(body, "network_") {
			t.Error("no network_ metrics found despite VICI socket being unavailable")
		}
		// ipsec_up should report 0, not be absent.
		if !strings.Contains(body, "ipsec_up 0") {
			t.Error("expected ipsec_up 0 for graceful degradation without VICI socket")
		}
	})

	t.Run("with_capabilities_produces_metrics", func(t *testing.T) {
		_, body := httpGet(t, baseURL+"/metrics")
		if !strings.Contains(body, "network_") {
			t.Error("no network_ metrics found despite capabilities being granted")
		}
	})

	t.Run("nonroot_with_caps", func(t *testing.T) {
		// Verify the image is configured to run as UID 65532 even with capabilities.
		out, err := exec.Command("docker", "inspect",
			"--format", "{{.Config.User}}", image).CombinedOutput()
		if err != nil {
			t.Fatalf("docker inspect failed: %v\n%s", err, out)
		}
		if !strings.Contains(string(out), "65532") {
			t.Error("expected container user to contain 65532")
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

func TestIpsecExporterViciSocketFlag(t *testing.T) {
	skipIfNoDocker(t)

	image := buildImage(t, ipsecDockerfile, ipsecImageTag)
	hostPort := freePort(t)

	// Start with an explicit --vici-socket pointing to a nonexistent path.
	// The exporter must start successfully and serve metrics with ipsec_up=0.
	_ = runContainerWithOpts(t, image,
		[]string{"--cap-add=NET_ADMIN", "--cap-add=DAC_READ_SEARCH"},
		hostPort, ipsecPort,
		"--listen-address=0.0.0.0:"+ipsecPort,
		"--proc-path=/proc",
		"--sys-path=/sys",
		"--vici-socket=/nonexistent",
	)

	url := "http://127.0.0.1:" + hostPort
	waitForHealthy(t, url+"/", 15*time.Second)

	status, body := httpGet(t, url+"/metrics")
	if status != 200 {
		t.Fatalf("expected 200 from /metrics with --vici-socket=/nonexistent, got %d", status)
	}
	if !strings.Contains(body, "ipsec_up 0") {
		t.Error("expected ipsec_up 0 with nonexistent VICI socket")
	}
}

func TestIpsecExporterVersionFlag(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, ipsecDockerfile, ipsecImageTag)
	testVersionFlag(t, image, "ipsec_exporter", "--cap-add=NET_ADMIN", "--cap-add=DAC_READ_SEARCH")
}

func TestIpsecExporterHelpFlag(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, ipsecDockerfile, ipsecImageTag)
	testHelpFlag(t, image, "--cap-add=NET_ADMIN", "--cap-add=DAC_READ_SEARCH")
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

func TestIpsecExporterInvalidFlag(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, ipsecDockerfile, ipsecImageTag)
	testInvalidFlag(t, image, "--cap-add=NET_ADMIN", "--cap-add=DAC_READ_SEARCH")
}
