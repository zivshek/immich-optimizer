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

# Inspect frame side data as well as stream tags: dynamic HDR can occur later
# in a clip. Probe errors fail the task so the watcher uploads the original.
source_probe=$(mktemp)
output_probe=$(mktemp)
trap 'rm -f "$source_probe" "$output_probe"' EXIT
probe_hdr() {
  ffprobe -v error -select_streams v:0 -show_streams -show_frames \
    -show_entries stream=pix_fmt,color_transfer,color_primaries,color_space,color_range:stream_side_data:frame_side_data \
    -of json "$1" > "$2"
}
probe_hdr "$src" "$source_probe"
jq -e '.streams | length == 1' "$source_probe" >/dev/null
if jq -e '[.. | objects | .side_data_type? // empty] | any(test("DOVI|Dolby|HDR Dynamic|2094"; "i"))' "$source_probe" >/dev/null; then
  echo "dynamic HDR requires original-file preservation; refusing transcode" >&2
  exit 1
fi
color_transfer=$(jq -r '.streams[0].color_transfer // "unknown"' "$source_probe")
output_pix_fmt=yuv420p
set --
case "$color_transfer" in
  arib-std-b67|smpte2084)
    case "$(jq -r '.streams[0].pix_fmt' "$source_probe")" in
      *12*|*16*|*f32*)
        echo "HDR input exceeds 10-bit output precision; retaining original" >&2
        exit 1 ;;
    esac
    output_pix_fmt=yuv420p10le
    echo "preserving HDR (${color_transfer}) in 10-bit AV1"
    ;;
esac
for field in color_transfer color_primaries color_space color_range; do
  value=$(jq -r --arg field "$field" '.streams[0][$field] // "unknown"' "$source_probe")
  if [ "$value" != "unknown" ]; then
    case "$field" in
      color_transfer) set -- "$@" -color_trc "$value" ;;
      color_primaries) set -- "$@" -color_primaries "$value" ;;
      color_space) set -- "$@" -colorspace "$value" ;;
      color_range) set -- "$@" -color_range "$value" ;;
    esac
  fi
done
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
  "$@" \
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

# Compare encoded HDR signalling after all metadata writes. Older FFmpeg/SVT
# builds may drop static HDR metadata; never upload that output in its place.
probe_hdr "$dst" "$output_probe"
if [ "$output_pix_fmt" = "yuv420p10le" ]; then
  [ "$(jq -r '.streams[0].pix_fmt' "$output_probe")" = "$output_pix_fmt" ] || exit 1
fi
for field in color_transfer color_primaries color_space color_range; do
  expected=$(jq -r --arg field "$field" '.streams[0][$field] // "unknown"' "$source_probe")
  actual=$(jq -r --arg field "$field" '.streams[0][$field] // "unknown"' "$output_probe")
  if [ "$expected" != "unknown" ] && [ "$expected" != "$actual" ]; then
    echo "HDR/color validation failed for ${field}; retaining original" >&2
    exit 1
  fi
done
hdr_signature() {
  jq -Sc '[.. | objects | select(.side_data_type? == "Mastering display metadata" or
    .side_data_type? == "Content light level metadata")] | unique' "$1"
}
if [ "$(hdr_signature "$source_probe")" != "$(hdr_signature "$output_probe")" ]; then
  echo "HDR/color validation failed; retaining original" >&2
  exit 1
fi
