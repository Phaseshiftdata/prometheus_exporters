# Build stage — Ubuntu 24.04 for libvirt-dev (CGO required)
FROM ubuntu:24.04 AS build

ARG VERSION=dev
ARG COMMIT=unknown

# Install build dependencies including libvirt-dev for CGO bindings.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates curl gcc libc6-dev pkg-config libvirt-dev && \
    rm -rf /var/lib/apt/lists/*

# Install Go 1.26 from the upstream tarball.
ARG GO_VERSION=1.26.6
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" \
        -o /tmp/go.tar.gz && \
    tar -C /usr/local -xzf /tmp/go.tar.gz && \
    rm /tmp/go.tar.gz

ENV PATH="/usr/local/go/bin:${PATH}"

WORKDIR /src

# Cache module downloads before copying the full source tree.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build with CGO enabled (required by libvirt Go bindings).
RUN CGO_ENABLED=1 go build \
    -trimpath \
    -ldflags "-s -w \
        -X github.com/phaseshiftdata/prometheus_exporters/src/version.Version=${VERSION} \
        -X github.com/phaseshiftdata/prometheus_exporters/src/version.GitCommit=${COMMIT} \
        -X github.com/phaseshiftdata/prometheus_exporters/src/version.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /libvirt_exporter \
    ./cmd/libvirt_exporter/

# Runtime stage — Ubuntu 24.04 with only the libvirt client library.
# Cannot use distroless because the binary requires CGO and libvirt shared libs.
FROM ubuntu:24.04

RUN apt-get update && \
    apt-get install -y --no-install-recommends libvirt0 ca-certificates && \
    rm -rf /var/lib/apt/lists/* && \
    useradd -r -s /usr/sbin/nologin -u 65532 exporter

COPY --from=build /libvirt_exporter /libvirt_exporter

EXPOSE 9177

USER 65532:65532

ENTRYPOINT ["/libvirt_exporter"]
