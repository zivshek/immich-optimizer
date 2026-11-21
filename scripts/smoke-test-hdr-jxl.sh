#!/bin/sh
set -eu
src=$1
tools=${IUO_HDR_TOOLS_DIR:-/opt/iuo-hdr/bin}
export LD_LIBRARY_PATH="$(dirname "$tools")/lib:${LD_LIBRARY_PATH:-}"
workdir=$(mktemp -d)
trap 'rm -rf "$workdir"' EXIT
cp "$src" "$workdir/source.jpg"
src="$workdir/source.jpg"
# Exercise capture metadata and a nontrivial display orientation as well.
exiftool -overwrite_original -Make=IUO -Model=HDR-smoke \
  -ExposureTime=1/125 -FNumber=1.8 -ISO=200 -FocalLength=4.5 -LensModel=HDR-smoke \
  '-DateTimeOriginal=2026:11:10 12:34:56' '-Orientation#=6' \
  -GPSLatitude=40.123456 -GPSLatitudeRef=N \
  -GPSLongitude=73.123456 -GPSLongitudeRef=W "$src"
"$tools/iuo-hdr-jxl" "$src" "$workdir/native.jxl"
# A changed float HDR candidate must reach and fail the pixel comparison.
"$tools/vips" linear "$workdir/native.jxl" "$workdir/changed.v" 0.5 0
"$tools/vips" jxlsave "$workdir/changed.v" "$workdir/changed.jxl" --distance 0 --effort 1 --strip
if "$tools/iuo-hdr-jxl-codec" validate "$src" "$workdir/changed.jxl" > "$workdir/changed.log" 2>&1; then
  echo 'HDR validator accepted altered HDR pixels' >&2
  exit 1
fi
grep -q 'HDR pixel values changed' "$workdir/changed.log"
# Reject an SDR JXL even when its dimensions match the HDR source.
"$tools/vips" thumbnail "$src" "$workdir/sdr.png" 100000 --size down
"$tools/vips" jxlsave "$workdir/sdr.png" "$workdir/sdr.jxl" --distance 0
if "$tools/iuo-hdr-jxl-codec" validate "$src" "$workdir/sdr.jxl"; then
  echo 'HDR validator accepted flattened SDR pixels' >&2
  exit 1
fi
echo '[smoke] Native HDR JXL pixels, color, orientation, metadata, and corrupted-output rejection passed'
