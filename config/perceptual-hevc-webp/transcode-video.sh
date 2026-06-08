#!/bin/sh
set -eu

src=$1
dst=$2
workdir=$(dirname "$dst")
video_only="${workdir}/video-only-hevc.mp4"
tmpdir="${workdir}/ab-av1-tmp"

mkdir -p "$tmpdir"
trap 'rm -rf "$tmpdir" "$video_only"' EXIT

export PATH="/opt/ab-av1/bin:${PATH}"
export AB_AV1_TEMP_DIR="$tmpdir"
export XDG_CACHE_HOME="${tmpdir}/cache"
mkdir -p "$XDG_CACHE_HOME"

dimensions=$(ffprobe -v error -select_streams v:0 \
  -show_entries stream=width,height -of csv=s=x:p=0 "$src")
width=${dimensions%x*}
height=${dimensions#*x}
vmaf_model=vmaf_v0.6.1.json
if [ "$width" -gt 2560 ] && [ "$height" -gt 1440 ]; then
  vmaf_model=vmaf_4k_v0.6.1.json
fi

ab-av1 auto-encode \
  --input "$src" \
  --output "$video_only" \
  --video-only \
  --encoder libx265 \
  --pix-format yuv420p \
  --min-vmaf 95 \
  --max-encoded-percent 85 \
  --preset slow \
  --min-samples 3 \
  --sample-every 8m \
  --sample-duration 12s \
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
