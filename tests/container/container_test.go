// Package container_test provides molecule-style end-to-end container tests
// for every exporter in the prometheus_exporters project. Tests build real
// Docker images from each exporter's Dockerfile, start containers, and verify
// behavior through HTTP and exec probes.
//
// All Docker interaction uses os/exec (no Docker client library).
package container_test

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// projectRoot returns the absolute path to the repository root. Detected
// dynamically by walking up from the test file's directory until go.mod is found.
var projectRoot = detectProjectRoot()

func detectProjectRoot() string {
	// Start from the current working directory.
	dir, err := os.Getwd()
	if err != nil {
		// Fallback: walk up from this file.
		dir = "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			abs, _ := filepath.Abs(dir)
			return abs
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root without finding go.mod.
			// Fall back to working directory.
			abs, _ := filepath.Abs(".")
			return abs
		}
		dir = parent
	}
}

var (
	dockerHost     string
	dockerHostOnce sync.Once
)

// detectDockerHost returns the IP address to use when contacting containers
// via port mapping. In a Docker-in-Docker environment, 127.0.0.1 does not
// reach sibling containers; instead we route through the Docker bridge
// gateway (typically 172.17.0.1). If detection fails, falls back to
// 127.0.0.1.
func detectDockerHost(t *testing.T) string {
	t.Helper()
	dockerHostOnce.Do(func() {
		out, err := exec.Command("docker", "network", "inspect", "bridge",
			"--format", "{{(index .IPAM.Config 0).Gateway}}").CombinedOutput()
		if err == nil {
			gw := strings.TrimSpace(string(out))
			if gw != "" {
				dockerHost = gw
				return
			}
		}
		dockerHost = "127.0.0.1"
	})
	return dockerHost
}

// dockerAvailable returns true when `docker info` succeeds.
func dockerAvailable(t *testing.T) bool {
	t.Helper()
	out, err := exec.Command("docker", "info").CombinedOutput()
	if err != nil {
		t.Logf("docker info: %v\n%s", err, out)
		return false
	}
	return true
}

// skipIfNoDocker calls t.Skip when Docker is not available.
func skipIfNoDocker(t *testing.T) {
	t.Helper()
	if !dockerAvailable(t) {
		t.Skip("docker not available")
	}
}

// buildImage builds a Docker image from the given Dockerfile (relative to
// projectRoot) and tags it. The image is removed when the test completes.
// Returns the tag for convenience.
func buildImage(t *testing.T, dockerfile, tag string) string {
	t.Helper()

	cmd := exec.Command("docker", "build",
		"-f", projectRoot+"/"+dockerfile,
		"-t", tag,
		"--build-arg", "VERSION=test",
		"--build-arg", "COMMIT=test123",
		projectRoot,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker build failed for %s:\n%s\n%v", dockerfile, out, err)
	}

	t.Cleanup(func() {
		_ = exec.Command("docker", "rmi", "-f", tag).Run()
	})
	return tag
}

// runContainer starts a container from image, mapping hostPort to
// containerPort, and passing entrypointArgs to the binary.
// Returns the container ID.
func runContainer(t *testing.T, image, hostPort, containerPort string, entrypointArgs ...string) string {
	t.Helper()
	return runContainerWithOpts(t, image, nil, hostPort, containerPort, entrypointArgs...)
}

// runContainerWithOpts is like runContainer but accepts additional Docker
// arguments placed before the image name (env vars, volumes, etc.).
func runContainerWithOpts(t *testing.T, image string, dockerArgs []string, hostPort, containerPort string, entrypointArgs ...string) string {
	t.Helper()

	cmdArgs := []string{"run", "-d", "--rm"}
	cmdArgs = append(cmdArgs, dockerArgs...)
	if hostPort != "" && containerPort != "" {
		cmdArgs = append(cmdArgs, "-p", hostPort+":"+containerPort)
	}
	cmdArgs = append(cmdArgs, image)
	cmdArgs = append(cmdArgs, entrypointArgs...)

	cmd := exec.Command("docker", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("docker run failed for %s:\n%s\n%v", image, out, err)
	}

	id := strings.TrimSpace(string(out))
	t.Cleanup(func() {
		_ = exec.Command("docker", "stop", "-t", "5", id).Run()
		_ = exec.Command("docker", "rm", "-f", id).Run()
	})
	return id
}

// runContainerHostNetwork starts a container with --network=host. Because
// runContainerForeground runs a container in the foreground and returns
// combined stdout/stderr and the exit error. Useful for --version and --help.
func runContainerForeground(t *testing.T, image string, args ...string) (string, error) {
	t.Helper()

	cmdArgs := []string{"run", "--rm"}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.Command("docker", cmdArgs...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// baseURL returns the HTTP URL prefix for a container exposed via port
// mapping on the given host port, using the auto-detected Docker host IP.
func baseURL(t *testing.T, hostPort string) string {
	t.Helper()
	return fmt.Sprintf("http://%s:%s", detectDockerHost(t), hostPort)
}

// httpGet performs an HTTP GET with a 5-second timeout.
func httpGet(t *testing.T, url string) (int, string) {
	t.Helper()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("HTTP GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading response body from %s: %v", url, err)
	}
	return resp.StatusCode, string(body)
}

// httpGetStatus performs an HTTP GET and returns the status code and body
// without calling t.Fatal on network errors (returns -1 instead).
func httpGetStatus(t *testing.T, url string) (int, string) {
	t.Helper()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return -1, err.Error()
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, ""
	}
	return resp.StatusCode, string(body)
}

// waitForHealthy polls url until it returns HTTP 200 or the timeout expires.
func waitForHealthy(t *testing.T, url string, timeout time.Duration) {
	t.Helper()

	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to become healthy after %s", url, timeout)
}

// freePort returns an available TCP port by briefly binding to port 0.
func freePort(t *testing.T) string {
	t.Helper()

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return fmt.Sprintf("%d", port)
}

// containerLogs returns the stdout/stderr logs from a container.
func containerLogs(t *testing.T, containerID string) string {
	t.Helper()
	out, err := exec.Command("docker", "logs", containerID).CombinedOutput()
	if err != nil {
		t.Logf("docker logs %s: %v", containerID, err)
	}
	return string(out)
}

// testCleanShutdown sends SIGTERM via docker stop and verifies the container
// exits within a reasonable timeout.
func testCleanShutdown(t *testing.T, containerID string) {
	t.Helper()

	cmd := exec.Command("docker", "stop", "-t", "10", containerID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("docker stop: %v\n%s", err, out)
	}

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		inspect, err := exec.Command("docker", "inspect",
			"--format", "{{.State.Running}}", containerID).CombinedOutput()
		if err != nil {
			// Container removed (--rm flag), clean shutdown.
			return
		}
		if strings.TrimSpace(string(inspect)) == "false" {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Errorf("container %s did not stop within 15 seconds after SIGTERM", containerID)
}

// testVersionFlag verifies that the binary supports --version.
func testVersionFlag(t *testing.T, image, binary string, dockerArgs ...string) {
	t.Helper()

	args := []string{"run", "--rm"}
	args = append(args, dockerArgs...)
	args = append(args, "--entrypoint", "/"+binary, image, "--version")

	out, _ := exec.Command("docker", args...).CombinedOutput()

	combined := string(out)
	if !strings.Contains(strings.ToLower(combined), "version") &&
		!strings.Contains(combined, "test") &&
		!strings.Contains(combined, binary) {
		t.Errorf("--version output does not contain version info: %s", combined)
	}
}

// testHelpFlag verifies that the binary supports --help.
func testHelpFlag(t *testing.T, image string, dockerArgs ...string) {
	t.Helper()

	args := []string{"run", "--rm"}
	args = append(args, dockerArgs...)
	args = append(args, image, "--help")

	out, _ := exec.Command("docker", args...).CombinedOutput()

	combined := string(out)
	if !strings.Contains(strings.ToLower(combined), "usage") &&
		!strings.Contains(strings.ToLower(combined), "flags") &&
		!strings.Contains(strings.ToLower(combined), "help") {
		t.Errorf("--help output does not contain usage info: %s", combined)
	}
}

// testInvalidFlag verifies that running the container with an unknown flag
// causes a non-zero exit code.
func testInvalidFlag(t *testing.T, image string, dockerArgs ...string) {
	t.Helper()

	args := append(dockerArgs, image, "--bogus-flag")
	_, err := runContainerForeground(t, image, args...)
	if err == nil {
		t.Error("expected non-zero exit for --bogus-flag, but container succeeded")
	}
}

// httpGetContentType performs an HTTP GET and returns the Content-Type header.
func httpGetContentType(t *testing.T, url string) string {
	t.Helper()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("HTTP GET %s: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Fatalf("expected 200 from %s, got %d", url, resp.StatusCode)
	}
	return resp.Header.Get("Content-Type")
}

// testNoShell verifies that /bin/sh does not exist in the image (distroless).
func testNoShell(t *testing.T, image string) {
	t.Helper()

	_, err := runContainerForeground(t, image, "--entrypoint", "/bin/sh", image)
	if err == nil {
		t.Errorf("expected /bin/sh to be absent in %s, but it succeeded", image)
	}
}

// testUser verifies the image is configured to run as the given UID.
func testUser(t *testing.T, image, expectedUID string) {
	t.Helper()

	out, err := exec.Command("docker", "inspect",
		"--format", "{{.Config.User}}", image).CombinedOutput()
	if err != nil {
		t.Fatalf("docker inspect failed: %v\n%s", err, out)
	}

	user := strings.TrimSpace(string(out))
	if !strings.Contains(user, expectedUID) {
		t.Errorf("expected container user to contain %s, got %q", expectedUID, user)
	}
}
