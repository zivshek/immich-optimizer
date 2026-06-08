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

FROM --platform=$TARGETPLATFORM rust:1-bookworm AS ab-av1-builder

ARG AB_AV1_VERSION=0.11.2

RUN cargo install ab-av1 --version "${AB_AV1_VERSION}" --locked --root /out && \
    /out/bin/ab-av1 --version

FROM --platform=$TARGETPLATFORM debian:trixie-slim AS ab-ffmpeg-builder

ARG FFMPEG_VERSION=7.1.1
ARG VMAF_VERSION=v3.0.0

RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    curl \
    git \
    libdav1d-dev \
    libopus-dev \
    libsvtav1enc-dev \
    meson \
    nasm \
    ninja-build \
    pkg-config \
    yasm \
    zlib1g-dev && \
    rm -rf /var/lib/apt/lists/* && \
    mkdir -p /build /opt/ab-av1

WORKDIR /build
RUN set -eux; \
    git clone --depth 1 --branch "${VMAF_VERSION}" https://github.com/Netflix/vmaf.git; \
    meson setup vmaf/libvmaf/build vmaf/libvmaf \
      --prefix=/opt/ab-av1 \
      --libdir=lib \
      --buildtype=release \
      --default-library=shared \
      -Denable_tests=false \
      -Denable_docs=false; \
    meson compile -C vmaf/libvmaf/build; \
    meson install -C vmaf/libvmaf/build

RUN set -eux; \
    curl -fsSL -o ffmpeg.tar.xz "https://ffmpeg.org/releases/ffmpeg-${FFMPEG_VERSION}.tar.xz"; \
    tar xf ffmpeg.tar.xz; \
    cd "ffmpeg-${FFMPEG_VERSION}"; \
    PKG_CONFIG_PATH=/opt/ab-av1/lib/pkgconfig ./configure \
      --prefix=/opt/ab-av1 \
      --extra-cflags=-I/opt/ab-av1/include \
      --extra-ldflags=-L/opt/ab-av1/lib \
      --enable-gpl \
      --enable-libdav1d \
      --enable-libopus \
      --enable-libsvtav1 \
      --enable-libvmaf \
      --disable-debug \
      --disable-doc; \
    make -j"$(nproc)"; \
    make install; \
    LD_LIBRARY_PATH=/opt/ab-av1/lib /opt/ab-av1/bin/ffmpeg -hide_banner -filters | grep libvmaf; \
    LD_LIBRARY_PATH=/opt/ab-av1/lib /opt/ab-av1/bin/ffmpeg -hide_banner -encoders | grep libsvtav1

FROM --platform=$TARGETPLATFORM ubuntu:24.04 AS oavif-builder

ARG ZIG_VERSION=0.15.2
ARG OAVIF_VERSION=0.1.3
ARG LIBAVIF_VERSION=1.3.0
ARG LIBWEBP_VERSION=1.4.0
ARG LIBJPEG_TURBO_VERSION=3.1.3
ARG LIBSPNG_VERSION=0.7.4
ARG LIBAOM_VERSION=v3.13.1
ARG DAV1D_VERSION=1.5.0

RUN apt-get update && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
    build-essential \
    ca-certificates \
    cmake \
    curl \
    git \
    libgav1-dev \
    libgcc-s1 \
    libheif-dev \
    libpng-dev \
    libstdc++6 \
    meson \
    nasm \
    pkg-config \
    wget \
    xz-utils \
    zlib1g-dev && \
    rm -rf /var/lib/apt/lists/*

RUN set -eux; \
    curl -fsSL "https://ziglang.org/download/${ZIG_VERSION}/zig-$(uname -m)-linux-${ZIG_VERSION}.tar.xz" -o zig-linux.tar.xz; \
    mkdir /opt/zig; \
    tar xf zig-linux.tar.xz -C /opt/zig --strip-components=1
ENV PATH="/opt/zig:${PATH}"

WORKDIR /build
ENV INSTALL_PREFIX=/build/install
ENV PKG_CONFIG_PATH=${INSTALL_PREFIX}/lib/pkgconfig
ENV CFLAGS="-I/build/install/include"
ENV LDFLAGS="-L/build/install/lib"
RUN mkdir -p ${INSTALL_PREFIX}

RUN set -eux; \
    wget -q "https://github.com/libjpeg-turbo/libjpeg-turbo/releases/download/${LIBJPEG_TURBO_VERSION}/libjpeg-turbo-${LIBJPEG_TURBO_VERSION}.tar.gz"; \
    tar -xzf "libjpeg-turbo-${LIBJPEG_TURBO_VERSION}.tar.gz"; \
    cmake -S "libjpeg-turbo-${LIBJPEG_TURBO_VERSION}" -B jt -DCMAKE_INSTALL_PREFIX=${INSTALL_PREFIX} -DCMAKE_BUILD_TYPE=Release -DENABLE_SHARED=OFF -DENABLE_STATIC=ON; \
    cmake --build jt -j"$(nproc)"; \
    cmake --install jt

RUN set -eux; \
    wget -q "https://github.com/randy408/libspng/archive/refs/tags/v${LIBSPNG_VERSION}.tar.gz" -O libspng.tgz; \
    tar -xzf libspng.tgz; \
    cmake -S "libspng-${LIBSPNG_VERSION}" -B spng -DCMAKE_INSTALL_PREFIX=${INSTALL_PREFIX} -DCMAKE_BUILD_TYPE=Release -DBUILD_SHARED_LIBS=OFF -DSPNG_STATIC=ON; \
    cmake --build spng -j"$(nproc)"; \
    cmake --install spng; \
    ln -s ${INSTALL_PREFIX}/lib/libspng_static.a ${INSTALL_PREFIX}/lib/libspng.a

RUN set -eux; \
    wget -q "https://github.com/webmproject/libwebp/archive/refs/tags/v${LIBWEBP_VERSION}.tar.gz" -O libwebp.tgz; \
    tar -xzf libwebp.tgz; \
    cmake -S "libwebp-${LIBWEBP_VERSION}" -B webp -DCMAKE_INSTALL_PREFIX=${INSTALL_PREFIX} -DBUILD_SHARED_LIBS=OFF; \
    cmake --build webp -j"$(nproc)"; \
    cmake --install webp

RUN set -eux; \
    wget -q "https://code.videolan.org/videolan/dav1d/-/archive/${DAV1D_VERSION}/dav1d-${DAV1D_VERSION}.tar.gz" -O dav1d.tgz; \
    tar -xzf dav1d.tgz; \
    meson setup "dav1d-${DAV1D_VERSION}/build" "dav1d-${DAV1D_VERSION}" --prefix=${INSTALL_PREFIX} --buildtype=release --default-library=static; \
    meson compile -C "dav1d-${DAV1D_VERSION}/build"; \
    meson install -C "dav1d-${DAV1D_VERSION}/build"

RUN set -eux; \
    wget -q "https://aomedia.googlesource.com/aom/+archive/${LIBAOM_VERSION}.tar.gz" -O libaom.tgz; \
    mkdir -p libaom-src libaom-build; \
    tar -xzf libaom.tgz -C libaom-src; \
    cmake -S libaom-src -B libaom-build \
      -DCMAKE_INSTALL_PREFIX=${INSTALL_PREFIX} \
      -DCMAKE_BUILD_TYPE=Release \
      -DBUILD_SHARED_LIBS=OFF \
      -DENABLE_TESTS=OFF \
      -DENABLE_EXAMPLES=OFF \
      -DENABLE_DOCS=OFF \
      -DENABLE_TOOLS=OFF \
      -DCONFIG_AV1_ENCODER=1 \
      -DCONFIG_AV1_DECODER=1 \
      -DCONFIG_TUNE_VMAF=0; \
    cmake --build libaom-build -j"$(nproc)"; \
    cmake --install libaom-build

RUN set -eux; \
    wget -q "https://github.com/AOMediaCodec/libavif/archive/refs/tags/v${LIBAVIF_VERSION}.tar.gz" -O libavif.tgz; \
    tar -xzf libavif.tgz; \
    cmake -S "libavif-${LIBAVIF_VERSION}" -B avif \
      -DCMAKE_INSTALL_PREFIX=${INSTALL_PREFIX} \
      -DCMAKE_BUILD_TYPE=Release \
      -DBUILD_SHARED_LIBS=OFF \
      -DAVIF_CODEC_AOM=SYSTEM \
      -DAVIF_CODEC_DAV1D=LOCAL \
      -DAVIF_CODEC_RAV1E=OFF \
      -DAVIF_CODEC_SVT=OFF \
      -DAVIF_LIBYUV=LOCAL \
      -DAVIF_BUILD_APPS=ON; \
    cmake --build avif -j"$(nproc)"; \
    cmake --install avif

RUN set -eux; \
    git clone --depth 1 --branch "${OAVIF_VERSION}" https://github.com/gianni-rosato/oavif.git /build/oavif; \
    cd /build/oavif; \
    zig build --release=fast --search-prefix ${INSTALL_PREFIX}; \
    install -m 755 zig-out/bin/oavif /usr/local/bin/oavif; \
    /usr/local/bin/oavif --version

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
    libio-compress-brotli-perl \
    libheif-plugin-aomenc \
    libjxl-tools \
    libgif7 \
    libsvtav1enc2 \
    libtcmalloc-minimal4 \
    libvips-tools \
    tzdata \
    vainfo \
    libva2 \
    libva-drm2 \
    mesa-va-drivers && \
    rm -rf /var/lib/apt/lists/* && \
    mkdir -p /etc/immich-optimizer/config /etc/immich-optimizer/bundled-configs /watch /undone /data && \
    chown -R appuser:appuser /etc/immich-optimizer /watch /undone /data

COPY --from=app-builder /out/immich-optimizer /usr/local/bin/immich-optimizer
COPY --from=caesium-builder /out/caesiumclt /usr/local/bin/caesiumclt
COPY --from=ab-av1-builder /out/bin/ab-av1 /usr/local/bin/ab-av1
COPY --from=ab-ffmpeg-builder /opt/ab-av1 /opt/ab-av1
COPY --from=oavif-builder /build/install/ /build
COPY --from=oavif-builder /usr/local/bin/oavif /usr/local/bin/oavif
ENV LD_LIBRARY_PATH=/build/lib:/opt/ab-av1/lib
RUN oavif --version && \
    ab-av1 --version && \
    /opt/ab-av1/bin/ffmpeg -hide_banner -filters | grep libvmaf && \
    /opt/ab-av1/bin/ffmpeg -hide_banner -encoders | grep libsvtav1
COPY --chown=appuser:appuser config/lossless /etc/immich-optimizer/config
COPY --chown=appuser:appuser config /etc/immich-optimizer/bundled-configs

LABEL org.opencontainers.image.title="Immich Optimizer" \
      org.opencontainers.image.description="File optimization service for Immich" \
      org.opencontainers.image.source="https://github.com/zivshek/immich-optimizer" \
      org.opencontainers.image.licenses="MIT"

ENV IUO_TASKS_FILE=/etc/immich-optimizer/config/tasks.yaml \
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
