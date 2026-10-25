#!/bin/sh
# Build-time integration checks. Stop at the first failing command and keep its
# native exit status so build logs identify a crash instead of hiding it in &&.
set -eu

check() {
  label=$1
  shift
  printf '\n[smoke] START %s\n' "$label"
  printf '[smoke] command:'
  printf ' %s' "$@"
  printf '\n'
  if "$@"; then
    printf '[smoke] PASS %s\n' "$label"
  else
    result=$?
    printf '[smoke] FAIL %s exit=%s\n' "$label" "$result" >&2
    if [ "$result" = 132 ]; then
      echo '[smoke] SIGILL: check CPU compatibility or an internal tool trap; the failing command is printed above' >&2
    fi
    exit "$result"
  fi
}

check architecture uname -m
check metric-version fssimu2 --version
check avif-version avifenc --version
check hdr-encode avifenc -q 80 --qgain-map 100 /tmp/hdr-smoke.jpg /tmp/hdr-smoke.avif
check hdr-copy-metadata exiftool -overwrite_original -m -api ExtractEmbedded=1 \
  -tagsFromFile /tmp/hdr-smoke.jpg -all:all /tmp/hdr-smoke.avif
check hdr-decode-all sh -c 'avifdec --info /tmp/hdr-smoke.avif > /tmp/hdr-smoke.info'
check hdr-gain-map grep -E 'Gain map[[:space:]]*:[[:space:]]*[1-9][0-9]*x[1-9][0-9]* pixels' /tmp/hdr-smoke.info
check hdr-source-base magick '/tmp/hdr-smoke.jpg[0]' /tmp/hdr-source.png
check hdr-candidate-base avifdec /tmp/hdr-smoke.avif /tmp/hdr-candidate.png
check hdr-base-score fssimu2 /tmp/hdr-source.png /tmp/hdr-candidate.png

check sdr-fixture magick -size 64x64 gradient: /tmp/avif-smoke.png
check sdr-encode magick /tmp/avif-smoke.png -quality 80 /tmp/avif-smoke.avif
check sdr-score fssimu2 /tmp/avif-smoke.png /tmp/avif-smoke.avif
check jxl-fixture magick -size 64x64 gradient: /tmp/jxl-smoke.png
check jxl-encode cjxl --lossless_jpeg=0 -q 80 --effort=1 /tmp/jxl-smoke.png /tmp/jxl-smoke.jxl
check jxl-decode djxl /tmp/jxl-smoke.jxl /tmp/jxl-smoke-decoded.png
check jxl-score fssimu2 /tmp/jxl-smoke.png /tmp/jxl-smoke-decoded.png
check video-tool-version ab-av1 --version

check ffmpeg-filters sh -c '/opt/ab-av1/bin/ffmpeg -hide_banner -filters > /tmp/ffmpeg-filters 2>&1'
check vmaf-filter grep libvmaf /tmp/ffmpeg-filters
check ffmpeg-encoders sh -c '/opt/ab-av1/bin/ffmpeg -hide_banner -encoders > /tmp/ffmpeg-encoders 2>&1'
for encoder in libsvtav1 libx265 hevc_nvenc; do
  check "$encoder" grep "$encoder" /tmp/ffmpeg-encoders
done

rm /tmp/hdr-source.png /tmp/hdr-candidate.png /tmp/hdr-smoke.jpg \
  /tmp/hdr-smoke.avif /tmp/hdr-smoke.info /tmp/avif-smoke.png \
  /tmp/avif-smoke.avif /tmp/jxl-smoke.png /tmp/jxl-smoke.jxl \
  /tmp/jxl-smoke-decoded.png /tmp/ffmpeg-filters /tmp/ffmpeg-encoders
echo '[smoke] All perceptual image and video checks passed'
