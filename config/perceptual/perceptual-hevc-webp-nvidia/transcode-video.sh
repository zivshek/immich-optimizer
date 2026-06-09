#!/bin/sh
set -eu

src=$1
dst=$2
workdir=$(dirname "$dst")
video_only="${workdir}/video-only-hevc-nvenc.mp4"
tmpdir="${workdir}/ab-av1-tmp"
min_vmaf=95

mkdir -p "$tmpdir"
trap 'rm -rf "$tmpdir" "$video_only"' EXIT

export PATH="/opt/ab-av1/bin:${PATH}"
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

# ab-av1 maps its CRF value to NVENC CQ, but auto-encode aborts when even the
# highest-quality trial cannot meet the target. Use a bounded search so we can
# fall back to the best measured result for difficult sources.
low=1
high=40
best_cq=
fallback_cq=
fallback_score=-1
while [ "$low" -le "$high" ]; do
  cq=$(( (low + high) / 2 ))
  echo "testing NVENC CQ ${cq} against VMAF ${min_vmaf}"
  sample_output=$(ab-av1 sample-encode \
    --input "$src" \
    --encoder hevc_nvenc \
    --crf "$cq" \
    --pix-format yuv420p \
    --preset p7 \
    --min-samples 3 \
    --sample-every 8m \
    --sample-duration 12s \
    --vmaf "model=path=/opt/ab-av1/share/vmaf/model/${vmaf_model}" \
    --enc rc=vbr \
    --enc b:v=0 \
    --enc spatial_aq=1 \
    --enc rc-lookahead=32 \
    --enc-input noautorotate 2>&1)
  printf '%s\n' "$sample_output"
  score=$(printf '%s\n' "$sample_output" |
    sed -n 's/.*VMAF \([0-9][0-9.]*\).*/\1/p' | tail -n 1)
  if [ -z "$score" ]; then
    echo "unable to read VMAF score for NVENC CQ ${cq}" >&2
    exit 1
  fi
  if awk "BEGIN { exit !(${score} >= ${fallback_score}) }"; then
    fallback_cq=$cq
    fallback_score=$score
  fi
  if awk "BEGIN { exit !(${score} >= ${min_vmaf}) }"; then
    best_cq=$cq
    low=$((cq + 1))
  else
    high=$((cq - 1))
  fi
done

if [ -z "$best_cq" ]; then
  best_cq=$fallback_cq
  echo "warning: NVENC could not meet VMAF ${min_vmaf}; using best measured CQ ${best_cq} at VMAF ${fallback_score}" >&2
fi
echo "selected NVENC CQ ${best_cq}"

/opt/ab-av1/bin/ffmpeg -hide_banner -y \
  -noautorotate \
  -i "$src" \
  -map 0:v:0 \
  -an \
  -c:v hevc_nvenc \
  -preset p7 \
  -pix_fmt yuv420p \
  -rc vbr \
  -b:v 0 \
  -cq "$best_cq" \
  -spatial_aq 1 \
  -rc-lookahead 32 \
  "$video_only"

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
