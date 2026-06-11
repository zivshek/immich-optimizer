#!/bin/sh
set -eu

src=$1
dst=$2

ffmpeg -hide_banner -y \
  -noautorotate \
  -i "$src" \
  -map 0:v:0 \
  -map 0:a? \
  -map_metadata 0 \
  -map_chapters 0 \
  -c:v libsvtav1 \
  -preset 6 \
  -crf 28 \
  -pix_fmt yuv420p \
  -c:a copy \
  -tag:v av01 \
  -movflags +faststart+use_metadata_tags \
  "$dst"

exiftool -overwrite_original -m \
  -api QuickTimeUTC=1 \
  -api LargeFileSupport=1 \
  -tagsFromFile "$src" \
  -all:all \
  "$dst"

for tag in Rotation GPSCoordinates Model CreateDate; do
  src_value=$(exiftool -s3 "-$tag" "$src")
  dst_value=$(exiftool -s3 "-$tag" "$dst")
  if [ -n "$src_value" ] && [ -z "$dst_value" ]; then
    echo "metadata validation failed: output is missing $tag" >&2
    exit 1
  fi
done
