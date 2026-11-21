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
encode_progress=$(mktemp)
current_stage=probe-source
progress_pid=
stage_started=0
source_duration=0
stop_progress() {
  if [ -n "$progress_pid" ]; then
    kill "$progress_pid" 2>/dev/null || :
    wait "$progress_pid" 2>/dev/null || :
    progress_pid=
  fi
}
report_progress() {
  elapsed=$(( $(date +%s) - stage_started ))
  if [ "$current_stage" != encode ]; then
    printf '[av1] stage=%s elapsed=%ss scanning frame metadata\n' "$current_stage" "$elapsed" >&2
    return
  fi
  # FFmpeg writes machine-readable progress independently of its diagnostic
  # output. Prefix our summaries so the application's progress filter keeps them.
  tail -n 32 "$encode_progress" | awk -F= -v elapsed="$elapsed" -v duration="$source_duration" '
    $1 == "frame" { frame = $2 }
    $1 == "fps" { fps = $2 }
    $1 == "out_time_us" { media_us = $2 }
    $1 == "out_time" { media_time = $2 }
    $1 == "speed" { speed = $2 }
    $1 == "total_size" { size = $2 }
    END {
      printf "[av1] stage=encode elapsed=%ss", elapsed
      if (duration + 0 > 0 && media_us ~ /^[0-9]+$/) {
        percent = media_us / 1000000 / duration * 100
        if (percent > 100) percent = 100
        printf " progress=%.1f%%", percent
      }
      if (media_time != "") printf " media_time=%s", media_time
      if (frame != "") printf " frame=%s", frame
      if (fps != "") printf " fps=%s", fps
      if (speed != "") printf " speed=%s", speed
      if (size ~ /^[0-9]+$/) printf " output_mib=%.1f", size / 1048576
      printf "\n"
    }
  ' >&2
}
run_with_progress() {
  stage_started=$(date +%s)
  (
    sleeper=
    trap '[ -z "$sleeper" ] || kill "$sleeper" 2>/dev/null || :; exit 0' TERM INT
    while :; do
      sleep 5 &
      sleeper=$!
      wait "$sleeper" || exit 0
      sleeper=
      report_progress
    done
  ) &
  progress_pid=$!
  # Keep the encoder/probe exit status; progress logging must never turn a
  # failed conversion into success or bypass original-file fallback.
  command_status=0
  "$@" || command_status=$?
  stop_progress
  report_progress
  return "$command_status"
}
finish() {
  result=$?
  stop_progress
  if [ "$result" -ne 0 ]; then
    printf '[av1] result=failed stage=%s exit=%s; original-file fallback required\n' "$current_stage" "$result" >&2
  fi
  rm -f "$source_probe" "$output_probe" "$encode_progress"
  exit "$result"
}
trap finish EXIT
echo "[av1] stage=probe-source scanning frame metadata for HDR"
probe_hdr() {
  run_with_progress ffprobe -v error -select_streams v:0 -show_streams -show_frames -show_format \
    -show_entries stream=pix_fmt,color_transfer,color_primaries,color_space,color_range:stream_side_data:frame_side_data:format=duration \
    -of json "$1" > "$2"
}
probe_hdr "$src" "$source_probe"
jq -e '.streams | length == 1' "$source_probe" >/dev/null
source_duration=$(jq -r '.format.duration // 0' "$source_probe")
jq -c '.streams[0] | {pix_fmt,color_transfer,color_primaries,color_space,color_range}' "$source_probe" | sed 's/^/[av1] source=/'
jq -c '[.. | objects | .side_data_type? // empty] | unique' "$source_probe" | sed 's/^/[av1] source_side_data=/'
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
current_stage=encode
echo "[av1] stage=encode codec=libsvtav1 preset=6 crf=${crf} output_pix_fmt=${output_pix_fmt}"
run_with_progress ffmpeg -hide_banner -nostats -stats_period 5 -progress "$encode_progress" -y \
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

current_stage=restore-metadata
echo "[av1] stage=restore-metadata"
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
current_stage=validate-output
echo "[av1] stage=validate-output scanning encoded frame metadata"
probe_hdr "$dst" "$output_probe"
jq -c '.streams[0] | {pix_fmt,color_transfer,color_primaries,color_space,color_range}' "$output_probe" | sed 's/^/[av1] output=/'
if [ "$output_pix_fmt" = "yuv420p10le" ]; then
  [ "$(jq -r '.streams[0].pix_fmt' "$output_probe")" = "$output_pix_fmt" ] || exit 1
fi
for field in color_transfer color_primaries color_space color_range; do
  expected=$(jq -r --arg field "$field" '.streams[0][$field] // "unknown"' "$source_probe")
  actual=$(jq -r --arg field "$field" '.streams[0][$field] // "unknown"' "$output_probe")
  if [ "$expected" != "unknown" ] && [ "$expected" != "$actual" ]; then
    echo "HDR/color validation failed for ${field}: expected=${expected} actual=${actual}; retaining original" >&2
    exit 1
  fi
done
hdr_signature() {
  jq -Sc '[.. | objects | select(.side_data_type? == "Mastering display metadata" or
    .side_data_type? == "Content light level metadata")] | unique' "$1"
}
if [ "$(hdr_signature "$source_probe")" != "$(hdr_signature "$output_probe")" ]; then
  echo "HDR/color validation failed; retaining original"
  printf '[av1] expected_static_HDR=%s\n' "$(hdr_signature "$source_probe")" >&2
  printf '[av1] actual_static_HDR=%s\n' "$(hdr_signature "$output_probe")" >&2 >&2
  exit 1
fi

echo "[av1] stage=complete result=optimized bytes=$(wc -c < "$dst" | tr -d '[:space:]') crf=${crf} pixel_format=${output_pix_fmt}"
