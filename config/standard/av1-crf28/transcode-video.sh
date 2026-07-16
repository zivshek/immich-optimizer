#!/bin/sh
set -eu

src=$1
dst=$2
crf="${IUO_VIDEO_CRF:-28}"

video_codec=$(ffprobe -v error -select_streams v:0 \
  -show_entries stream=codec_name \
  -of default=noprint_wrappers=1:nokey=1 "$src")
if [ "$video_codec" = "av1" ]; then
  echo "input video is already AV1; passing through without transcoding"
  cp "$src" "$dst"
  exit 0
fi

ffmpeg -hide_banner -y \
  -noautorotate \
  -i "$src" \
  -map 0:v:0 \
  -map 0:a? \
  -map_metadata 0 \
  -map_chapters 0 \
  -c:v libsvtav1 \
  -preset 6 \
  -crf "$crf" \
  -pix_fmt yuv420p \
  -c:a copy \
  -tag:v av01 \
  -movflags +faststart+use_metadata_tags \
  "$dst"

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
