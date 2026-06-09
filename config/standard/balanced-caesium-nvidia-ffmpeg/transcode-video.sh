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
  -c:v hevc_nvenc \
  -preset p7 \
  -tune hq \
  -rc vbr \
  -cq 18 \
  -b:v 0 \
  -profile:v main \
  -pix_fmt yuv420p \
  -spatial_aq 1 \
  -rc-lookahead 32 \
  -c:a copy \
  -tag:v hvc1 \
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
