FROM --platform=$BUILDPLATFORM golang:1.23-bookworm AS app-builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${BUILD_DATE}" \
    -o /out/immich-optimizer .

FROM --platform=$BUILDPLATFORM ubuntu:24.04 AS caesium-builder

ARG TARGETARCH
ARG CAESIUM_GITHUB_REPO=Lymphatus/caesium-clt

RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    ca-certificates curl jq && \
    rm -rf /var/lib/apt/lists/* && \
    mkdir -p /out

RUN set -eux; \
    CAESIUM_LATEST_RELEASE=$(curl -fsSL "https://api.github.com/repos/${CAESIUM_GITHUB_REPO}/releases/latest" | jq -er '.tag_name'); \
    case "$TARGETARCH" in \
      amd64) CAESIUM_ARCH=x86_64-unknown-linux-musl ;; \
      arm64) CAESIUM_ARCH=aarch64-unknown-linux-musl ;; \
      *) echo "Architecture $TARGETARCH is not supported"; exit 1 ;; \
    esac; \
    CAESIUM_ARCHIVE="caesiumclt-${CAESIUM_LATEST_RELEASE}-${CAESIUM_ARCH}"; \
    curl -fsSL -o "/tmp/${CAESIUM_ARCHIVE}.tar.gz" \
      "https://github.com/${CAESIUM_GITHUB_REPO}/releases/latest/download/${CAESIUM_ARCHIVE}.tar.gz"; \
    tar xzf "/tmp/${CAESIUM_ARCHIVE}.tar.gz" -C /tmp; \
    test -x "/tmp/${CAESIUM_ARCHIVE}/caesiumclt"; \
    install -m 755 "/tmp/${CAESIUM_ARCHIVE}/caesiumclt" /out/caesiumclt; \
    /out/caesiumclt --version

FROM debian:trixie-slim

RUN groupadd -r appuser && \
    useradd -r -g appuser -u 1001 appuser && \
    apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    exiftool \
    ffmpeg \
    handbrake-cli \
    imagemagick \
    jq \
    libc6 \
    libheif-plugin-aomenc \
    libio-compress-brotli-perl \
    libjxl-tools \
    libgif7 \
    libtcmalloc-minimal4 \
    libvips-tools \
    tzdata \
    vainfo \
    libva2 \
    libva-drm2 \
    mesa-va-drivers \
    webp && \
    rm -rf /var/lib/apt/lists/* && \
    mkdir -p /etc/immich-optimizer/config /etc/immich-optimizer/bundled-configs/standard /custom_profiles /watch /undone /data && \
    chown -R appuser:appuser /etc/immich-optimizer /custom_profiles /watch /undone /data

COPY --from=app-builder /out/immich-optimizer /usr/local/bin/immich-optimizer
COPY --from=caesium-builder /out/caesiumclt /usr/local/bin/caesiumclt
COPY --chown=appuser:appuser config/standard/lossless /etc/immich-optimizer/config
COPY --chown=appuser:appuser config/standard /etc/immich-optimizer/bundled-configs/standard

LABEL org.opencontainers.image.title="Immich Optimizer" \
      org.opencontainers.image.description="File optimization service for Immich" \
      org.opencontainers.image.source="https://github.com/zivshek/immich-optimizer" \
      org.opencontainers.image.licenses="MIT"

ENV IUO_TASKS_FILE=/etc/immich-optimizer/bundled-configs/standard/lossless/tasks.yaml \
    IUO_WATCH_DIR=/watch \
    IUO_UNDONE_DIR=/undone \
    IUO_DASHBOARD_ADDRESS=:8098 \
    IUO_STATS_DATABASE=/data/immich-optimizer.db

EXPOSE 8098

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD immich-optimizer -version || exit 1

USER appuser
WORKDIR /watch
CMD ["immich-optimizer"]
