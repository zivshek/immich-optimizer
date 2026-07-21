#!/bin/sh
set -eu

src=$1
dst=$2
crf="${IUO_VIDEO_CRF:-28}"

has_apac=$(ffprobe -v error -select_streams a \
  -show_entries stream=codec_tag_string \
  -of default=noprint_wrappers=1:nokey=1 "$src" | grep -xq apac && echo 1 || echo 0)
if [ "$has_apac" = "1" ] && [ "${IUO_DROP_APAC:-0}" != "1" ]; then
  echo "input video contains APAC spatial audio; passing through original to preserve it"
  cp "$src" "$dst"
  exit 0
fi
if [ "$has_apac" = "1" ]; then
  echo "input video contains APAC spatial audio; dropping APAC and keeping primary audio only"
fi

video_codec=$(ffprobe -v error -select_streams v:0 \
  -show_entries stream=codec_name \
  -of default=noprint_wrappers=1:nokey=1 "$src")
if [ "$video_codec" = "av1" ] && [ "$has_apac" != "1" ]; then
  echo "input video is already AV1; passing through without transcoding"
  cp "$src" "$dst"
  exit 0
fi

input_pix_fmt=$(ffprobe -v error -select_streams v:0 \
  -show_entries stream=pix_fmt \
  -of default=noprint_wrappers=1:nokey=1 "$src")
output_pix_fmt=yuv420p
case "$input_pix_fmt" in
  *10le|*12le)
    output_pix_fmt=yuv420p10le
    ;;
esac

echo "encoding AV1 with CRF ${crf} and pixel format ${output_pix_fmt}"
ffmpeg -hide_banner -y \
  -noautorotate \
  -i "$src" \
  -map 0:v:0 \
  -map 0:a:0? \
  -map_metadata 0 \
  -map_chapters 0 \
  -c:v libsvtav1 \
  -preset 6 \
  -crf "$crf" \
  -pix_fmt "$output_pix_fmt" \
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
