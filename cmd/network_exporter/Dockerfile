# Build stage
FROM ubuntu:26.04 AS build

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

ARG VERSION=dev
ARG COMMIT=unknown

# hadolint ignore=DL3008
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl libcap2-bin && \
    rm -rf /var/lib/apt/lists/*

ARG GO_VERSION=1.26.6
ARG GO_SHA256=708effb774be8237570d0add163225abbdfaf4fca28b2611df167beba4feef89
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" \
        -o /tmp/go.tar.gz && \
    echo "${GO_SHA256}  /tmp/go.tar.gz" | sha256sum -c - && \
    tar -C /usr/local -xzf /tmp/go.tar.gz && \
    rm /tmp/go.tar.gz

ENV PATH="/usr/local/go/bin:${PATH}"

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# When COVERAGE=1, build with coverage instrumentation for molecule test coverage
# collection. Coverage builds omit -s -w to preserve symbols required by the
# coverage runtime.
ARG COVERAGE=0
RUN if [ "$COVERAGE" = "1" ]; then \
        CGO_ENABLED=0 go build -cover -coverpkg=./... \
            -trimpath \
            -ldflags "-X github.com/phaseshiftdata/prometheus_exporters/src/version.Version=${VERSION} -X github.com/phaseshiftdata/prometheus_exporters/src/version.GitCommit=${COMMIT}" \
            -o /network_exporter \
            ./cmd/network_exporter/; \
    else \
        CGO_ENABLED=0 go build \
            -trimpath \
            -ldflags "-s -w -X github.com/phaseshiftdata/prometheus_exporters/src/version.Version=${VERSION} -X github.com/phaseshiftdata/prometheus_exporters/src/version.GitCommit=${COMMIT}" \
            -o /network_exporter \
            ./cmd/network_exporter/; \
    fi

# Set file capabilities so the binary can use NET_ADMIN and DAC_READ_SEARCH
# as a non-root user. The permitted set must be a subset of the container's
# bounding set, so only these two capabilities are granted.
# hadolint ignore=DL3059
RUN setcap cap_net_admin,cap_dac_read_search=+eip /network_exporter

# Runtime stage
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /network_exporter /network_exporter

EXPOSE 9100

USER 65532:65532

ENTRYPOINT ["/network_exporter"]
