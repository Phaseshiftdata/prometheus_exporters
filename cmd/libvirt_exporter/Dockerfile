# Build stage — Ubuntu for libvirt-dev (CGO required)
FROM ubuntu:26.04 AS build

ARG VERSION=dev
ARG COMMIT=unknown

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates curl gcc libc6-dev pkg-config libvirt-dev && \
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

RUN CGO_ENABLED=1 go build \
    -trimpath \
    -ldflags "-s -w \
        -X github.com/phaseshiftdata/prometheus_exporters/src/version.Version=${VERSION} \
        -X github.com/phaseshiftdata/prometheus_exporters/src/version.GitCommit=${COMMIT}" \
    -o /libvirt_exporter \
    ./cmd/libvirt_exporter/

# Collect runtime dependencies into a minimal root filesystem.
RUN mkdir -p /runtime/lib/x86_64-linux-gnu /runtime/lib64 /runtime/etc/ssl/certs /runtime/usr/lib/x86_64-linux-gnu && \
    cp /etc/ssl/certs/ca-certificates.crt /runtime/etc/ssl/certs/ && \
    cp /lib64/ld-linux-x86-64.so.2 /runtime/lib64/ && \
    cp /lib/x86_64-linux-gnu/libc.so.6 /runtime/lib/x86_64-linux-gnu/ && \
    cp /lib/x86_64-linux-gnu/libm.so.6 /runtime/lib/x86_64-linux-gnu/ && \
    cp /lib/x86_64-linux-gnu/libpthread.so.0 /runtime/lib/x86_64-linux-gnu/ 2>/dev/null || true && \
    cp /lib/x86_64-linux-gnu/libdl.so.2 /runtime/lib/x86_64-linux-gnu/ 2>/dev/null || true && \
    for lib in $(ldd /libvirt_exporter | grep -oP '/\S+'); do \
        dir=$(dirname "$lib"); \
        mkdir -p "/runtime$dir"; \
        cp "$lib" "/runtime$lib" 2>/dev/null || true; \
    done && \
    echo "nonroot:x:65532:65532:nonroot:/:/sbin/nologin" > /runtime/etc/passwd && \
    echo "nonroot:x:65532:" > /runtime/etc/group

# Runtime stage — scratch with only the binary, libs, and CA certs.
FROM scratch

COPY --from=build /runtime/ /
COPY --from=build /libvirt_exporter /libvirt_exporter

EXPOSE 9177

USER 65532:65532

ENTRYPOINT ["/libvirt_exporter"]
