#!/bin/sh
set -eu
src=$1
dst=$2
tools=${IUO_HDR_TOOLS_DIR:-/opt/iuo-hdr/bin}
export LD_LIBRARY_PATH="$(dirname "$tools")/lib:${LD_LIBRARY_PATH:-}"
stage=encode
finish() {
  result=$?
  if [ "$result" -ne 0 ]; then
    rm -f "$dst"
    printf '[hdr-jxl] result=failed stage=%s; original-file fallback required\n' "$stage" >&2
  fi
  exit "$result"
}
trap finish EXIT
echo '[hdr-jxl] route=UltraHDR-to-native-JXL distance=0 effort=9; image score unused; SDR tone mapping may change'
"$tools/iuo-hdr-jxl-codec" encode "$src" "$dst"
stage=restore-metadata
# Copy capture metadata, not the source SDR ICC or obsolete gain-map tags.
# Pixel orientation was normalized before encoding.
exiftool -overwrite_original -m -api ExtractEmbedded=1 -tagsFromFile "$src" \
  -GPS:all -Make -Model -DateTimeOriginal -CreateDate -ModifyDate \
  -ExposureTime -FNumber -ISO -FocalLength -FocalLengthIn35mmFormat \
  -ExposureCompensation -ExposureProgram -ExposureMode -MeteringMode \
  -WhiteBalance -Flash -LensMake -LensModel -LensInfo -Software \
  -Artist -Copyright -ImageDescription -UserComment \
  '-OffsetTime*' '-SubSecTime*' '-Orientation#=1' "$dst"
for tag in GPSPosition Make Model DateTimeOriginal ExposureTime FNumber ISO FocalLength LensModel; do
  src_value=$(exiftool -api ExtractEmbedded=1 -s3 "-$tag" "$src")
  dst_value=$(exiftool -api ExtractEmbedded=1 -s3 "-$tag" "$dst")
  if [ "$src_value" != "$dst_value" ]; then
    echo "[hdr-jxl] metadata validation failed: $tag differs" >&2
    exit 1
  fi
done
orientation=$(exiftool -s3 '-Orientation#' "$dst")
if [ "$orientation" != 1 ]; then
  echo '[hdr-jxl] metadata validation failed: output orientation must be 1' >&2
  exit 1
fi
stage=validate-HDR-pixels-and-color
"$tools/iuo-hdr-jxl-codec" validate "$src" "$dst"
echo '[hdr-jxl] result=optimized format=JXL HDR=native lossless=reconstructed-pixels'
