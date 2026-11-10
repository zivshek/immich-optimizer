#!/bin/sh
set -eu

src=$1
dst=$2
reconstructed=
trap '[ -z "$reconstructed" ] || rm -f "$reconstructed"' EXIT

metadata=$(exiftool -j -G1 -s -api ExtractEmbedded=1 "$src")
printf '%s' "$metadata" | jq -e 'length == 1 and (.[0] | has("ExifTool:Error") | not)' >/dev/null
# Preserve gain-map and native PQ/HLG sources as originals. A reversible JPEG
# bitstream inside JXL does not guarantee displayable HDR in JXL viewers.
# C2PA, depth auxiliaries and high bit depth alone do not identify HDR.
hdr_reasons=$(printf '%s' "$metadata" | jq -r '.[0] | to_entries | map(select(
  (.key | test("GainMap|HDRCapacity|XMP-hdrgm:|HDRHeadroom"; "i")) or
  ((.key | test("(JUMBF:.*(Type|Label)|AuxiliaryImage(Type|SubType)|MPImage.*Type)$"; "i")) and
    (.value | tostring | test("gain.?map|hdrgm|21496"; "i"))) or
  ((.key | test("TransferCharacteristics$|TransferFunction$")) and
    (.value | tostring | test("PQ|HLG|2084|B67|^(16|18)$"; "i")))
)) | map(.key) | join(",")')
if [ -n "$hdr_reasons" ]; then
  printf '[lossless] result=passthrough reason=HDR tags=%s; retaining original to preserve HDR\n' "$hdr_reasons"
  cp "$src" "$(dirname "$dst")/$(basename "$src")"
  exit 0
fi

file_type=$(printf '%s' "$metadata" | jq -r '.[0]["File:FileType"] // ""')
case "$file_type" in
  JPEG|PNG|PAM|PGM|PGX|PNM|PPM) : ;;
  *)
    echo '[lossless] result=passthrough reason=unsupported-source'
    cp "$src" "$(dirname "$dst")/$(basename "$src")"
    exit 0 ;;
esac
if [ "$file_type" = JPEG ]; then
  echo '[lossless] route=jpeg-recompression; verifying byte-for-byte JPEG reconstruction'
  cjxl --lossless_jpeg=1 "$src" "$dst"
  reconstructed=$(mktemp "$(dirname "$dst")/reconstructed-XXXXXX.jpg")
  djxl "$dst" "$reconstructed"
  if ! cmp -s "$src" "$reconstructed"; then
    echo '[lossless] JPEG reconstruction differs from original; original-file fallback required' >&2
    exit 1
  fi
else
  echo '[lossless] route=pixel-lossless; distance=0; perceptual score unused'
  cjxl --lossless_jpeg=0 -d 0 "$src" "$dst"
  # Preserve writable photo metadata. Do not rewrite reversible JPEG outputs.
  exiftool -overwrite_original -m -api ExtractEmbedded=1 -tagsFromFile "$src" -all:all "$dst"
  for tag in GPSPosition Model DateTimeOriginal Orientation; do
    src_value=$(exiftool -api ExtractEmbedded=1 -s3 "-$tag" "$src")
    dst_value=$(exiftool -api ExtractEmbedded=1 -s3 "-$tag" "$dst")
    if [ "$src_value" != "$dst_value" ]; then
      echo "[lossless] metadata validation failed: $tag differs; original-file fallback required" >&2
      exit 1
    fi
  done
fi
echo '[lossless] result=optimized'
