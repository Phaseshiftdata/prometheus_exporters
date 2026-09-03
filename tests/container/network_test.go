package container_test

import (
	"os/exec"
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

	t.Run("metric_families_present", func(t *testing.T) {
		_, body := httpGet(t, baseURL+"/metrics")
		// Each family may appear as the base metric or the truncated variant.
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

	t.Run("arp_entries_truncated_gauge", func(t *testing.T) {
		_, body := httpGet(t, baseURL+"/metrics")
		if !strings.Contains(body, "network_arp_entries_truncated") {
			t.Error("network_arp_entries_truncated gauge not present in /metrics output")
		}
	})

	t.Run("graph_edges_truncated_gauge", func(t *testing.T) {
		_, body := httpGet(t, baseURL+"/metrics")
		if !strings.Contains(body, "network_graph_edges_truncated") {
			t.Error("network_graph_edges_truncated gauge not present in /metrics output")
		}
	})

	t.Run("with_capabilities_produces_metrics", func(t *testing.T) {
		_, body := httpGet(t, baseURL+"/metrics")
		// With NET_ADMIN + DAC_READ_SEARCH capabilities, we should get
		// actual metric output (not just an error page).
		if !strings.Contains(body, "network_") {
			t.Error("no network_ metrics found despite capabilities being granted")
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

	t.Run("clean_shutdown", func(t *testing.T) {
		testCleanShutdown(t, containerID)
	})
}

func TestNetworkExporterCardinalityFlags(t *testing.T) {
	skipIfNoDocker(t)

	image := buildImage(t, networkDockerfile, networkImageTag)
	hostPort := freePort(t)

	_ = runContainerWithOpts(t, image,
		[]string{"--cap-add=NET_ADMIN", "--cap-add=DAC_READ_SEARCH"},
		hostPort, networkPort,
		"--listen-address=0.0.0.0:"+networkPort,
		"--proc-path=/proc",
		"--sys-path=/sys",
		"--max-arp-entries=100",
		"--max-graph-edges=100",
	)

	url := "http://127.0.0.1:" + hostPort
	waitForHealthy(t, url+"/", 15*time.Second)

	t.Run("max_arp_entries_accepted", func(t *testing.T) {
		status, _ := httpGet(t, url+"/metrics")
		if status != 200 {
			t.Fatalf("expected 200 from /metrics with --max-arp-entries, got %d", status)
		}
	})

	t.Run("max_graph_edges_accepted", func(t *testing.T) {
		status, _ := httpGet(t, url+"/metrics")
		if status != 200 {
			t.Fatalf("expected 200 from /metrics with --max-graph-edges, got %d", status)
		}
	})
}

func TestNetworkExporterPathFlags(t *testing.T) {
	skipIfNoDocker(t)

	image := buildImage(t, networkDockerfile, networkImageTag)
	hostPort := freePort(t)

	_ = runContainerWithOpts(t, image,
		[]string{"--cap-add=NET_ADMIN", "--cap-add=DAC_READ_SEARCH"},
		hostPort, networkPort,
		"--listen-address=0.0.0.0:"+networkPort,
		"--proc-path=/proc",
		"--sys-path=/sys",
	)

	url := "http://127.0.0.1:" + hostPort
	waitForHealthy(t, url+"/", 15*time.Second)

	t.Run("proc_path_accepted", func(t *testing.T) {
		status, _ := httpGet(t, url+"/metrics")
		if status != 200 {
			t.Fatalf("expected 200 from /metrics with --proc-path, got %d", status)
		}
	})

	t.Run("sys_path_accepted", func(t *testing.T) {
		status, _ := httpGet(t, url+"/metrics")
		if status != 200 {
			t.Fatalf("expected 200 from /metrics with --sys-path, got %d", status)
		}
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
