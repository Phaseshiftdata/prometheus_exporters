package container_test

import (
	"strings"
	"testing"
)

const (
	openbaoDockerfile = "cmd/openbao_exporter/Dockerfile"
	openbaoImageTag   = "test-openbao-exporter"
)

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
