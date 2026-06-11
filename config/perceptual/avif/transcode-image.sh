#!/bin/sh
set -eu

src=$1
dst=$2
workdir=$(dirname "$dst")
candidate="${workdir}/candidate.avif"
target_score="${IUO_IMAGE_SCORE:-85}"
low=1
high=100
best_quality=
best_score=

trap 'rm -f "$candidate"' EXIT

score_candidate() {
  score=$(fssimu2 "$src" "$candidate" 2>&1 | tail -n 1 | tr -d '[:space:]')
  if ! printf '%s\n' "$score" | grep -Eq '^-?[0-9]+([.][0-9]+)?$'; then
    echo "unable to read SSIMULACRA2 score from fssimu2 output: ${score}" >&2
    exit 1
  fi
}

while [ "$low" -le "$high" ]; do
  quality=$(( (low + high) / 2 ))
  magick "$src" -quality "$quality" "$candidate"
  score_candidate
  echo "AVIF quality ${quality}: SSIMULACRA2 ${score}"

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
  magick "$src" -quality "$best_quality" "$candidate"
  score_candidate
  best_score=$score
  echo "warning: AVIF could not meet SSIMULACRA2 ${target_score}; using quality ${best_quality} at score ${best_score}" >&2
fi

echo "selected AVIF quality ${best_quality} at SSIMULACRA2 ${best_score}"
magick "$src" -quality "$best_quality" "$dst"

exiftool -overwrite_original -m \
  -tagsFromFile "$src" \
  -all:all \
  "$dst"

for tag in GPSPosition Model DateTimeOriginal; do
  src_value=$(exiftool -s3 "-$tag" "$src")
  dst_value=$(exiftool -s3 "-$tag" "$dst")
  if [ -n "$src_value" ] && [ -z "$dst_value" ]; then
    echo "metadata validation failed: output is missing $tag" >&2
    exit 1
  fi
done
