#!/bin/sh
set -eu

src=$1
dst=$2
workdir=$(dirname "$dst")
candidate="${workdir}/candidate.webp"
target_score=90
low=0
high=100
best_quality=
best_score=

trap 'rm -f "$candidate"' EXIT

while [ "$low" -le "$high" ]; do
  quality=$(( (low + high) / 2 ))
  cwebp -quiet -q "$quality" -m 6 "$src" -o "$candidate"
  score=$(fssimu2 "$src" "$candidate")
  echo "WebP quality ${quality}: SSIMULACRA2 ${score}"

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
  cwebp -quiet -q "$best_quality" -m 6 "$src" -o "$candidate"
  best_score=$(fssimu2 "$src" "$candidate")
  echo "warning: WebP could not meet SSIMULACRA2 ${target_score}; using quality ${best_quality} at score ${best_score}" >&2
fi

echo "selected WebP quality ${best_quality} at SSIMULACRA2 ${best_score}"
cwebp -quiet -q "$best_quality" -m 6 -metadata all "$src" -o "$dst"

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
