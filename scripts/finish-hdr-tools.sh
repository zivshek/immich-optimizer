#!/bin/sh
set -eux
prefix=/opt/iuo-hdr
export PKG_CONFIG_PATH="$prefix/lib/pkgconfig"
export LD_LIBRARY_PATH="$prefix/lib"
c++ -O2 -std=c++17 /build/hdr-jxl-codec.cpp -o "$prefix/bin/iuo-hdr-jxl-codec" \
  $(pkg-config --cflags --libs vips libjxl)
install -m 755 /build/transcode-hdr-jxl.sh "$prefix/bin/iuo-hdr-jxl"
# A real gain-map fixture checks reconstruction, encoding, metadata rewrite,
# independent pixel/color validation, and rejection of a flattened candidate.
cp /build/hdr/vips-8.18.0/test/test-suite/images/ultra-hdr.jpg /tmp/hdr-jxl-smoke.jpg
sh /build/smoke-test-hdr-jxl.sh /tmp/hdr-jxl-smoke.jpg
