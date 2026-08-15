# Build stage
FROM ubuntu:26.04 AS build

ARG VERSION=dev
ARG COMMIT=unknown

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl && \
    rm -rf /var/lib/apt/lists/*

ARG GO_VERSION=1.26.6
RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" \
        -o /tmp/go.tar.gz && \
    tar -C /usr/local -xzf /tmp/go.tar.gz && \
    rm /tmp/go.tar.gz

ENV PATH="/usr/local/go/bin:${PATH}"

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags "-s -w -X github.com/phaseshiftdata/prometheus_exporters/src/version.Version=${VERSION} -X github.com/phaseshiftdata/prometheus_exporters/src/version.GitCommit=${COMMIT}" \
    -o /network_exporter \
    ./cmd/network_exporter/

# Runtime stage
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /network_exporter /network_exporter

EXPOSE 9100

USER 65532:65532

ENTRYPOINT ["/network_exporter"]
