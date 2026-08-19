package container_test

import (
	"strings"
	"testing"
	"time"
)

const (
	libvirtDockerfile = "cmd/libvirt_exporter/Dockerfile"
	libvirtImageTag   = "test-libvirt-exporter"
	libvirtPort       = "9177"
)

func TestLibvirtExporter(t *testing.T) {
	skipIfNoDocker(t)

	image := buildImage(t, libvirtDockerfile, libvirtImageTag)
	hostPort := freePort(t)

	// Use test:///default which is a built-in libvirt test driver that does
	// not require a running libvirtd. It may or may not work inside the
	// minimal scratch container depending on shared library availability.
	containerID := runContainer(t, image, hostPort, libvirtPort,
		"--listen-address=0.0.0.0:"+libvirtPort,
		"--libvirt-uri=test:///default",
	)

	baseURL := "http://127.0.0.1:" + hostPort
	waitForHealthy(t, baseURL+"/", 15*time.Second)

	t.Run("metrics_returns_200", func(t *testing.T) {
		status, body := httpGet(t, baseURL+"/metrics")
		if status != 200 {
			t.Fatalf("expected 200 from /metrics, got %d", status)
		}
		// The metrics should contain libvirt_up (value may be 0 or 1
		// depending on whether test:///default works in the container).
		if !strings.Contains(body, "libvirt_up") {
			t.Error("/metrics does not contain libvirt_up metric")
		}
	})

	t.Run("landing_page", func(t *testing.T) {
		status, body := httpGet(t, baseURL+"/")
		if status != 200 {
			t.Fatalf("expected 200 from /, got %d", status)
		}
		if !strings.Contains(body, "Libvirt Exporter") {
			t.Error("landing page does not contain 'Libvirt Exporter'")
		}
	})

	t.Run("binary_starts_successfully", func(t *testing.T) {
		// Verify the container is actually running (binary linked correctly
		// against libvirt shared libs in the scratch+libs image).
		logs := containerLogs(t, containerID)
		if strings.Contains(logs, "not found") || strings.Contains(logs, "No such file") {
			t.Errorf("container logs suggest missing shared libraries:\n%s", logs)
		}
	})

	t.Run("clean_shutdown", func(t *testing.T) {
		testCleanShutdown(t, containerID)
	})
}

func TestLibvirtExporterVersionFlag(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, libvirtDockerfile, libvirtImageTag)
	testVersionFlag(t, image, "libvirt_exporter")
}

func TestLibvirtExporterHelpFlag(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, libvirtDockerfile, libvirtImageTag)
	testHelpFlag(t, image)
}

func TestLibvirtExporterNoShell(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, libvirtDockerfile, libvirtImageTag)
	// The libvirt_exporter uses a scratch+libs base (not distroless), but
	// should still not have a shell.
	testNoShell(t, image)
}

func TestLibvirtExporterUser(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, libvirtDockerfile, libvirtImageTag)
	testUser(t, image, "65532")
}
