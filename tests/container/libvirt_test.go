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

	base := baseURL(t, hostPort)
	waitForHealthy(t, base+"/", 15*time.Second)

	t.Run("metrics_returns_200", func(t *testing.T) {
		status, body := httpGet(t, base+"/metrics")
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
		status, body := httpGet(t, base+"/")
		if status != 200 {
			t.Fatalf("expected 200 from /, got %d", status)
		}
		if !strings.Contains(body, "Libvirt Exporter") {
			t.Error("landing page does not contain 'Libvirt Exporter'")
		}
	})

	t.Run("metrics_content_type", func(t *testing.T) {
		contentType := httpGetContentType(t, base+"/metrics")
		if !strings.HasPrefix(contentType, "text/plain") {
			t.Errorf("expected text/plain Content-Type from /metrics, got %q", contentType)
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

func TestLibvirtExporterInvalidFlag(t *testing.T) {
	skipIfNoDocker(t)
	image := buildImage(t, libvirtDockerfile, libvirtImageTag)
	testInvalidFlag(t, image)
}

// TestLibvirtExporterLibvirtUp verifies the libvirt_up metric is present and
// equals 0 when libvirtd is unavailable inside the distroless container.
func TestLibvirtExporterLibvirtUp(t *testing.T) {
	skipIfNoDocker(t)

	image := buildImage(t, libvirtDockerfile, libvirtImageTag)
	hostPort := freePort(t)

	containerID := runContainer(t, image, hostPort, libvirtPort,
		"--listen-address=0.0.0.0:"+libvirtPort,
	)

	base := baseURL(t, hostPort)
	waitForHealthy(t, base+"/", 15*time.Second)

	status, body := httpGet(t, base+"/metrics")
	if status != 200 {
		t.Fatalf("expected 200 from /metrics, got %d", status)
	}

	if !strings.Contains(body, "libvirt_up") {
		t.Fatal("/metrics does not contain libvirt_up metric")
	}

	if !strings.Contains(body, "libvirt_up 0") {
		t.Error("expected libvirt_up 0 when libvirtd is unavailable")
	}

	logs := containerLogs(t, containerID)
	if strings.Contains(logs, "panic") {
		t.Errorf("container panicked:\n%s", logs)
	}
}

func TestLibvirtExporterRemoteURIRejected(t *testing.T) {
	skipIfNoDocker(t)

	image := buildImage(t, libvirtDockerfile, libvirtImageTag)

	out, err := runContainerForeground(t, image,
		image,
		"--listen-address=0.0.0.0:"+libvirtPort,
		"--libvirt-uri=qemu+ssh://host/system",
	)
	if err == nil {
		t.Errorf("expected container to fail with remote URI, but it succeeded: %s", out)
	}
	if !strings.Contains(out, "remote transport") {
		t.Errorf("error output should mention remote transport, got: %s", out)
	}
}

func TestLibvirtExporterGracefulNoLibvirtd(t *testing.T) {
	skipIfNoDocker(t)

	image := buildImage(t, libvirtDockerfile, libvirtImageTag)
	hostPort := freePort(t)

	containerID := runContainer(t, image, hostPort, libvirtPort,
		"--listen-address=0.0.0.0:"+libvirtPort,
	)

	base := baseURL(t, hostPort)
	waitForHealthy(t, base+"/", 15*time.Second)

	status, body := httpGet(t, base+"/")
	if status != 200 {
		t.Fatalf("expected 200 from /, got %d", status)
	}
	if !strings.Contains(body, "Libvirt Exporter") {
		t.Error("landing page does not contain 'Libvirt Exporter'")
	}

	status, body = httpGet(t, base+"/metrics")
	if status != 200 {
		t.Fatalf("expected 200 from /metrics, got %d", status)
	}
	if !strings.Contains(body, "libvirt_up 0") {
		t.Error("expected libvirt_up 0 when libvirtd is not running")
	}

	logs := containerLogs(t, containerID)
	if strings.Contains(logs, "panic") {
		t.Errorf("container panicked:\n%s", logs)
	}

	testCleanShutdown(t, containerID)
}
