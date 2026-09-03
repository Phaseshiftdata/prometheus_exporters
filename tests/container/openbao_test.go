package container_test

import (
	"strings"
	"testing"
	"time"
)

const (
	openbaoDockerfile = "cmd/openbao_exporter/Dockerfile"
	openbaoImageTag   = "test-openbao-exporter"
	openbaoPort       = "9100"
)

// TestOpenBaoExporterUnreachable starts the exporter pointed at an unreachable
// OpenBao address and verifies it still serves metrics gracefully with
// openbao_up=0.
func TestOpenBaoExporterUnreachable(t *testing.T) {
	skipIfNoDocker(t)

	image := buildImage(t, openbaoDockerfile, openbaoImageTag)
	hostPort := freePort(t)

	// Point at an unreachable address. The exporter should start and serve
	// degraded metrics rather than crashing.
	containerID := runContainer(t, image, hostPort, openbaoPort,
		"--listen-address=0.0.0.0:"+openbaoPort,
		"--openbao-addr=http://127.0.0.1:1",
	)

	base := baseURL(t, hostPort)
	waitForHealthy(t, base+"/", 15*time.Second)

	t.Run("openbao_up_metric_present", func(t *testing.T) {
		status, body := httpGet(t, base+"/metrics")
		if status != 200 {
			t.Fatalf("expected 200 from /metrics, got %d", status)
		}
		if !strings.Contains(body, "openbao_up") {
			t.Error("/metrics does not contain openbao_up metric")
		}
	})

	t.Run("openbao_up_is_zero_when_unreachable", func(t *testing.T) {
		_, body := httpGet(t, base+"/metrics")
		if !strings.Contains(body, "openbao_up 0") {
			t.Errorf("expected openbao_up 0 when OpenBao is unreachable, got:\n%s", body)
		}
	})

	t.Run("landing_page", func(t *testing.T) {
		status, body := httpGet(t, base+"/")
		if status != 200 {
			t.Fatalf("expected 200 from /, got %d", status)
		}
		if !strings.Contains(body, "OpenBao Exporter") {
			t.Error("landing page does not contain 'OpenBao Exporter'")
		}
	})

	t.Run("clean_shutdown", func(t *testing.T) {
		testCleanShutdown(t, containerID)
	})
}

func TestOpenBaoExporterVersionFlag(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, openbaoDockerfile, openbaoImageTag)
	testVersionFlag(t, image, "openbao_exporter")
}

func TestOpenBaoExporterHelpFlag(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, openbaoDockerfile, openbaoImageTag)
	testHelpFlag(t, image)
}

func TestOpenBaoExporterNoShell(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, openbaoDockerfile, openbaoImageTag)
	testNoShell(t, image)
}

func TestOpenBaoExporterUser(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, openbaoDockerfile, openbaoImageTag)
	testUser(t, image, "65532")
}

func TestOpenBaoExporterInvalidFlag(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, openbaoDockerfile, openbaoImageTag)
	testInvalidFlag(t, image)
}

func TestOpenBaoExporterNoOpenbaoAddr(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, openbaoDockerfile, openbaoImageTag)

	// Without --openbao-addr the exporter should refuse to start.
	out, err := runContainerForeground(t, image, image)
	if err == nil {
		t.Errorf("expected container to fail without --openbao-addr, but it succeeded: %s", out)
	}
	if !strings.Contains(strings.ToLower(out), "openbao-addr") {
		t.Errorf("error output should mention --openbao-addr: %s", out)
	}
}

func TestOpenBaoExporterInvalidAddr(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, openbaoDockerfile, openbaoImageTag)

	// A non-HTTP URL should be rejected at startup by the PreRunE validator.
	out, err := runContainerForeground(t, image, image,
		"--openbao-addr=ftp://invalid:8200",
	)
	if err == nil {
		t.Errorf("expected container to fail with invalid --openbao-addr, but it succeeded: %s", out)
	}
	if !strings.Contains(out, "http://") && !strings.Contains(out, "https://") {
		t.Errorf("error output should mention http:// or https:// requirement: %s", out)
	}
}
