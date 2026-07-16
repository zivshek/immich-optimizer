#!/bin/sh
set -eu

src=$1
dst=$2
workdir=$(dirname "$dst")
candidate="${workdir}/candidate.jxl"
decoded_candidate="${workdir}/candidate-jxl-score.png"
target_score="${IUO_IMAGE_SCORE:-85}"
low=0
high=100
best_quality=
best_score=

trap 'rm -f "$candidate" "$decoded_candidate"' EXIT

score_candidate() {
  djxl "$candidate" "$decoded_candidate"
  score=$(fssimu2 "$src" "$decoded_candidate" 2>&1 | tail -n 1 | tr -d '[:space:]')
  if ! printf '%s\n' "$score" | grep -Eq '^-?[0-9]+([.][0-9]+)?$'; then
    echo "unable to read SSIMULACRA2 score from fssimu2 output: ${score}" >&2
    exit 1
  fi
}

encode_candidate() {
  quality=$1
  cjxl --lossless_jpeg=0 -q "$quality" --effort=7 "$src" "$candidate"
}

while [ "$low" -le "$high" ]; do
  quality=$(( (low + high) / 2 ))
  encode_candidate "$quality"
  score_candidate
  echo "JXL quality ${quality}: SSIMULACRA2 ${score}"

  if awk "BEGIN { exit !(${score} >= ${target_score}) }"; then
    best_quality=$quality
    best_score=$score
    high=$((quality - 1))
  else
    low=$((quality + 1))
  fi
done

if [ -z "$best_quality" ]; then
  best_quality=100
  encode_candidate "$best_quality"
  score_candidate
  best_score=$score
  echo "warning: JXL could not meet SSIMULACRA2 ${target_score}; using quality ${best_quality} at score ${best_score}" >&2
fi

echo "selected JXL quality ${best_quality} at SSIMULACRA2 ${best_score}"
cjxl --lossless_jpeg=0 -q "$best_quality" --effort=7 "$src" "$dst"

exiftool -overwrite_original -m \
  -api ExtractEmbedded=1 \
  -tagsFromFile "$src" \
  -all:all \
  "$dst"

for tag in GPSPosition Model DateTimeOriginal; do
  src_value=$(exiftool -api ExtractEmbedded=1 -s3 "-$tag" "$src")
  dst_value=$(exiftool -api ExtractEmbedded=1 -s3 "-$tag" "$dst")
  if [ -n "$src_value" ] && [ -z "$dst_value" ]; then
    echo "metadata validation failed: output is missing $tag" >&2
    exit 1
  fi
done
