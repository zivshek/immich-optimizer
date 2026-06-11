#!/bin/sh
set -eu

src=$1
dst=$2
workdir=$(dirname "$dst")
video_only="${workdir}/video-only-av1.mp4"
tmpdir="${workdir}/ab-av1-tmp"
min_vmaf="${IUO_VIDEO_SCORE:-95}"

mkdir -p "$tmpdir"
trap 'rm -rf "$tmpdir" "$video_only"' EXIT

export PATH="/opt/ab-av1/bin:${PATH}"
export LD_LIBRARY_PATH="/opt/ab-av1/lib:${LD_LIBRARY_PATH:-}"
export AB_AV1_TEMP_DIR="$tmpdir"
export XDG_CACHE_HOME="${tmpdir}/cache"
mkdir -p "$XDG_CACHE_HOME"

probe=$(ffprobe -v error -select_streams v:0 \
  -show_entries stream=width,height -of json "$src")
width=$(printf '%s' "$probe" | jq -r '.streams[0].width')
height=$(printf '%s' "$probe" | jq -r '.streams[0].height')
vmaf_model=vmaf_v0.6.1.json
if [ "$width" -gt 2560 ] && [ "$height" -gt 1440 ]; then
  vmaf_model=vmaf_4k_v0.6.1.json
fi

ab-av1 auto-encode \
  --input "$src" \
  --output "$video_only" \
  --video-only \
  --min-vmaf "$min_vmaf" \
  --max-encoded-percent 80 \
  --preset 6 \
  --min-samples 5 \
  --sample-every 2m \
  --sample-duration 6s \
  --vmaf "model=path=/opt/ab-av1/share/vmaf/model/${vmaf_model}" \
  --enc-input noautorotate

ffmpeg -hide_banner -y \
  -noautorotate \
  -i "$video_only" \
  -i "$src" \
  -map 0:v:0 \
  -map 1:a? \
  -map_metadata 1 \
  -map_chapters 1 \
  -c copy \
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
