# Build stage
FROM ubuntu:26.04 AS build

ARG VERSION=dev
ARG COMMIT=unknown

# Install dependencies required to fetch and extract the Go toolchain.
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl && \
    rm -rf /var/lib/apt/lists/*

# Install Go 1.26 from the upstream tarball.
ARG GO_VERSION=1.26.6
ARG GO_SHA256=708effb774be8237570d0add163225abbdfaf4fca28b2611df167beba4feef89
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" \
        -o /tmp/go.tar.gz && \
    echo "${GO_SHA256}  /tmp/go.tar.gz" | sha256sum -c - && \
    tar -C /usr/local -xzf /tmp/go.tar.gz && \
    rm /tmp/go.tar.gz

ENV PATH="/usr/local/go/bin:${PATH}"

WORKDIR /src

# Cache module downloads before copying the full source tree.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build a fully static binary (no CGO) with trimmed paths and stripped symbols.
# When COVERAGE=1, build with coverage instrumentation for molecule test coverage
# collection. Coverage builds omit -s -w to preserve symbols required by the
# coverage runtime.
ARG COVERAGE=0
RUN if [ "$COVERAGE" = "1" ]; then \
        CGO_ENABLED=0 go build -cover -coverpkg=./... \
            -trimpath \
            -ldflags "-X github.com/phaseshiftdata/prometheus_exporters/src/version.Version=${VERSION} -X github.com/phaseshiftdata/prometheus_exporters/src/version.GitCommit=${COMMIT}" \
            -o /cloudflare_exporter \
            ./cmd/cloudflare_exporter/; \
    else \
        CGO_ENABLED=0 go build \
            -trimpath \
            -ldflags "-s -w -X github.com/phaseshiftdata/prometheus_exporters/src/version.Version=${VERSION} -X github.com/phaseshiftdata/prometheus_exporters/src/version.GitCommit=${COMMIT}" \
            -o /cloudflare_exporter \
            ./cmd/cloudflare_exporter/; \
    fi

# Runtime stage
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /cloudflare_exporter /cloudflare_exporter

EXPOSE 9199

USER 65532:65532

ENTRYPOINT ["/cloudflare_exporter"]
