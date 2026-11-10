#!/bin/sh
set -eu

src=$1
dst=$2

magick "$src" -quality 65 "$dst"

exiftool -overwrite_original -m \
  -api ExtractEmbedded=1 \
  -tagsFromFile "$src" \
  -all:all \
  "$dst"

for tag in GPSPosition Model DateTimeOriginal; do
  src_value=$(exiftool -api ExtractEmbedded=1 -s3 "-$tag" "$src")
  dst_value=$(exiftool -api ExtractEmbedded=1 -s3 "-$tag" "$dst")
  if [ -n "$src_value" ] && [ -z "$dst_value" ]; then
    echo "metadata validation failed: output is missing $tag" >&2
    exit 1
  fi
done
