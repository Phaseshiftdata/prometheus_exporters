# Build stage
FROM ubuntu:24.04 AS build

ARG VERSION=dev
ARG COMMIT=unknown

# Install dependencies required to fetch and extract the Go toolchain.
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl && \
    rm -rf /var/lib/apt/lists/*

# Install Go 1.26 from the upstream tarball.
ARG GO_VERSION=1.26.0
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

# Build a fully static binary (no CGO) with trimmed paths and stripped symbols.
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.revision=${COMMIT}" \
    -o /cloudflare_exporter \
    ./cmd/cloudflare_exporter/

# Runtime stage
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /cloudflare_exporter /cloudflare_exporter

EXPOSE 9199

USER 65532:65532

ENTRYPOINT ["/cloudflare_exporter"]
