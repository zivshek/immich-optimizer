#!/bin/sh
set -eu

src=$1
dst=$2
target_score="${IUO_IMAGE_SCORE:-85}"

encode_avif() {
  tenbit=$1
  oavif \
    --score-tgt "$target_score" \
    --tolerance 2 \
    --max-threads 8 \
    --tenbit "$tenbit" \
    "$src" "$dst"
}

if ! encode_avif 1; then
  echo "warning: 10-bit AVIF encode failed; retrying with 8-bit output" >&2
  rm -f "$dst"
  encode_avif 0
fi

exiftool -overwrite_original -m \
  -tagsFromFile "$src" \
  -all:all \
  "$dst"

for tag in GPSPosition Model DateTimeOriginal; do
  src_value=$(exiftool -s3 "-$tag" "$src")
  dst_value=$(exiftool -s3 "-$tag" "$dst")
  if [ -n "$src_value" ] && [ -z "$dst_value" ]; then
    echo "metadata validation failed: output is missing $tag" >&2
    exit 1
  fi
done
