ARG DEBIAN_VERSION=bookworm

# Keep the image compiler aligned with the verified CI/local toolchain.
# Floating golang:bookworm advanced to Go 1.27, whose HTTP/2 wrapper is not
# compatible with the currently pinned gRPC dependency.
FROM --platform=$BUILDPLATFORM golang:1.26.1-bookworm@sha256:ab3d6955bbc813a0f3fdf220c1d817dd89c0b3f283777db8ece4a32fe7858edd AS builder

# Provided by buildx for every target platform.
ARG TARGETOS
ARG TARGETARCH

WORKDIR /build
# Copy the dependency manifests first to leverage the Docker layer cache.
COPY go.mod go.sum ./
RUN go mod download

COPY . ./

ARG VERSION
ARG REVISION

# The SQL backend is pure Go; build a fully static binary without cgo.
# The builder stage runs on the native build platform and cross-compiles
# for the requested target, so multi-arch image builds need no QEMU.
RUN go version && CGO_ENABLED=0 GOMAXPROCS=2 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -p=1 -o /go/bin/bigquery-emulator \
    -ldflags "-s -w -X main.version=${VERSION} -X main.revision=${REVISION}" \
    ./cmd/bigquery-emulator

# The license helper is pinned, built for the native build platform, and scans
# the same target package/platform as the binary above.
RUN GOMAXPROCS=2 go install github.com/google/go-licenses/v2@v2.0.1
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} ./scripts/container-licenses.sh /go/licenses

FROM debian:${DEBIAN_VERSION}

COPY --from=builder /go/bin/bigquery-emulator /bin/bigquery-emulator
COPY --from=builder /go/licenses/ /usr/share/licenses/bigquery-emulator/
LABEL org.opencontainers.image.licenses="MIT"
RUN test -s /usr/share/licenses/bigquery-emulator/LICENSE \
 && test -s /usr/share/licenses/bigquery-emulator/go/LICENSE \
 && test -s /usr/share/licenses/bigquery-emulator/dependency-versions.txt \
 && test -s /usr/share/licenses/bigquery-emulator/third-party/github.com/goccy/googlesqlite/NOTICE \
 && test -s /usr/share/licenses/bigquery-emulator/third-party/github.com/apache/arrow/go/v15/NOTICE.txt

WORKDIR /work

ENTRYPOINT ["/bin/bigquery-emulator"]
