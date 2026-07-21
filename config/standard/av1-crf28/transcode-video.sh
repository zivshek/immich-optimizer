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

color_transfer=$(ffprobe -v error -select_streams v:0 \
  -show_entries stream=color_transfer \
  -of default=noprint_wrappers=1:nokey=1 "$src")
output_pix_fmt=yuv420p
video_filter=
color_args=
case "$color_transfer" in
  arib-std-b67|smpte2084)
    video_filter='-vf zscale=t=linear:npl=100,format=gbrpf32le,tonemap=tonemap=hable:desat=0,zscale=t=bt709:p=bt709:m=bt709:r=tv,format=yuv420p'
    color_args='-color_primaries bt709 -color_trc bt709 -colorspace bt709'
    ;;
esac

if [ -n "$video_filter" ]; then
  echo "encoding AV1 with CRF ${crf}; tonemapping HDR (${color_transfer}) to 8-bit SDR"
else
  echo "encoding AV1 with CRF ${crf} and pixel format ${output_pix_fmt}"
fi
ffmpeg -hide_banner -y \
  -noautorotate \
  -i "$src" \
  -map 0:v:0 \
  -map 0:a:0? \
  -map_metadata 0 \
  -map_chapters 0 \
  $video_filter \
  -c:v libsvtav1 \
  -preset 6 \
  -crf "$crf" \
  -pix_fmt "$output_pix_fmt" \
  $color_args \
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
