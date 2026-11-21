#!/bin/sh
# Run only in the target-platform Debian build stage.
set -eux
prefix=/opt/iuo-hdr
vips_version=${VIPS_VERSION:-8.18.0}
uhdr_version=${UHDR_VERSION:-1.4.0}
jobs=${IUO_BUILD_JOBS:-2}
apt-get update
DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
  build-essential ca-certificates cmake curl meson ninja-build pkg-config xz-utils \
  libglib2.0-dev libexpat1-dev libjpeg62-turbo-dev libpng-dev libjxl-dev \
  liblcms2-dev libexif-dev exiftool libio-compress-brotli-perl
mkdir -p /build/hdr "$prefix"
cd /build/hdr
curl -fL --retry 3 "https://github.com/google/libultrahdr/archive/refs/tags/v${uhdr_version}.tar.gz" -o uhdr.tar.gz
tar xf uhdr.tar.gz
cmake -S "libultrahdr-${uhdr_version}" -B uhdr-build \
  -DCMAKE_BUILD_TYPE=Release -DCMAKE_INSTALL_PREFIX="$prefix" -DCMAKE_INSTALL_LIBDIR=lib \
  -DBUILD_SHARED_LIBS=ON -DUHDR_BUILD_DEPS=OFF -DUHDR_BUILD_EXAMPLES=OFF -DUHDR_BUILD_TESTS=OFF
cmake --build uhdr-build -j"$jobs"
cmake --install uhdr-build
export PKG_CONFIG_PATH="$prefix/lib/pkgconfig"
export LD_LIBRARY_PATH="$prefix/lib"
curl -fL --retry 3 "https://github.com/libvips/libvips/releases/download/v${vips_version}/vips-${vips_version}.tar.xz" -o vips.tar.xz
tar xf vips.tar.xz
meson setup vips-build "vips-${vips_version}" --prefix="$prefix" --libdir=lib \
  --buildtype=release --auto-features=disabled -Dmodules=disabled \
  -Djpeg=enabled -Djpeg-xl=enabled -Duhdr=enabled -Dpng=enabled -Dlcms=enabled -Dexif=enabled
meson compile -C vips-build -j "$jobs"
meson install -C vips-build
