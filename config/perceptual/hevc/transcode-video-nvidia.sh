#!/bin/sh
set -eu

src=$1
dst=$2
workdir=$(dirname "$dst")
video_only="${workdir}/video-only-hevc-nvenc.mp4"
tmpdir="${workdir}/ab-av1-tmp"
min_vmaf="${IUO_VIDEO_SCORE:-95}"

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

# Some NVENC drivers flatten VBR target-quality trials so every CQ produces the
# same encode. Search constant QP instead, which guarantees that the tested
# quality value controls the output, and use VMAF to choose the result.
low=1
high=40
best_qp=
best_percent=
fallback_qp=
fallback_score=-1
fallback_percent=
while [ "$low" -le "$high" ]; do
  qp=$(( (low + high) / 2 ))
  echo "testing NVENC QP ${qp} against VMAF ${min_vmaf}"
  sample_output=$(ab-av1 sample-encode \
    --input "$src" \
    --encoder hevc_nvenc \
    --crf 0 \
    --pix-format yuv420p \
    --preset p7 \
    --min-samples 3 \
    --sample-every 8m \
    --sample-duration 12s \
    --vmaf "model=path=/opt/ab-av1/share/vmaf/model/${vmaf_model}" \
    --enc rc=constqp \
    --enc qp="$qp" \
    --enc spatial_aq=1 \
    --enc rc-lookahead=32 \
    --enc-input noautorotate 2>&1)
  printf '%s\n' "$sample_output"
  score=$(printf '%s\n' "$sample_output" |
    sed -n 's/.*VMAF \([0-9][0-9.]*\).*/\1/p' | tail -n 1)
  percent=$(printf '%s\n' "$sample_output" |
    sed -n 's/.*predicted video stream size.*(\([0-9][0-9.]*\)%).*/\1/p' | tail -n 1)
  if [ -z "$score" ] || [ -z "$percent" ]; then
    echo "unable to read VMAF score or predicted size for NVENC QP ${qp}" >&2
    exit 1
  fi
  if awk "BEGIN { exit !(${score} > ${fallback_score}) }"; then
    fallback_qp=$qp
    fallback_score=$score
    fallback_percent=$percent
  fi
  if awk "BEGIN { exit !(${score} >= ${min_vmaf}) }"; then
    best_qp=$qp
    best_percent=$percent
    low=$((qp + 1))
  else
    high=$((qp - 1))
  fi
done

if [ -z "$best_qp" ]; then
  best_qp=$fallback_qp
  best_percent=$fallback_percent
  echo "warning: NVENC could not meet VMAF ${min_vmaf}; using best measured QP ${best_qp} at VMAF ${fallback_score}" >&2
fi
echo "selected NVENC QP ${best_qp}"
if awk "BEGIN { exit !(${best_percent} > 80) }"; then
  echo "projected output is ${best_percent}% of original; less than 20% savings, refusing encode" >&2
  exit 1
fi

/opt/ab-av1/bin/ffmpeg -hide_banner -y \
  -noautorotate \
  -i "$src" \
  -map 0:v:0 \
  -an \
  -c:v hevc_nvenc \
  -preset p7 \
  -pix_fmt yuv420p \
  -rc constqp \
  -qp "$best_qp" \
  -spatial_aq 1 \
  -rc-lookahead 32 \
  "$video_only"

ffmpeg -hide_banner -y \
  -noautorotate \
  -i "$video_only" \
  -i "$src" \
  -map 0:v:0 \
  -map 1:a:0? \
  -map_metadata 1 \
  -map_chapters 1 \
  -c copy \
  -tag:v hvc1 \
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
