#!/bin/sh
set -eu

src=$1
dst=$2

# Keep the stored frame dimensions unchanged and preserve the source display
# rotation metadata. No scale, crop, or autorotation filter is applied.
# Pixel mett data streams are intentionally not mapped: FFmpeg reads them as
# codec "none" but cannot mux them into the output MP4.
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

# FFmpeg preserves media streams and metadata it understands. ExifTool restores
# writable QuickTime/Android metadata such as GPS, camera model, capture dates,
# and the display rotation matrix.
exiftool -overwrite_original -m \
  -api ExtractEmbedded=1 \
  -api QuickTimeUTC=1 \
  -api LargeFileSupport=1 \
  -tagsFromFile "$src" \
  -all:all \
  "$dst"

for tag in Rotation GPSCoordinates Model CreateDate; do
  src_value=$(exiftool -api ExtractEmbedded=1 -s3 "-$tag" "$src")
  dst_value=$(exiftool -api ExtractEmbedded=1 -s3 "-$tag" "$dst")
  if [ -n "$src_value" ] && [ -z "$dst_value" ]; then
    echo "metadata validation failed: output is missing $tag" >&2
    exit 1
  fi
done
