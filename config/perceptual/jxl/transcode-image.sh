#!/bin/sh
set -eu

src=$1
dst=$2
# cjxl's JPEG pixel decode does not apply Ultra HDR gain maps. Route HDR
# through the validated AVIF encoder instead of silently producing SDR JXL.
metadata=$(exiftool -j -G1 -s -api ExtractEmbedded=1 "$src")
printf '%s' "$metadata" | jq -e 'length == 1 and (.[0] | has("ExifTool:Error") | not)' >/dev/null
# Match HDR-specific evidence only; C2PA, depth auxiliaries, HEIF and high bit
# depth alone also occur in SDR images. Keep aligned with the AVIF profile.
hdr_reasons=$(printf '%s' "$metadata" | jq -r '.[0] | to_entries | map(select(
  (.key | test("GainMap|HDRCapacity|XMP-hdrgm:|HDRHeadroom"; "i")) or
  ((.key | test("(JUMBF:.*(Type|Label)|AuxiliaryImage(Type|SubType)|MPImage.*Type)$"; "i")) and
    (.value | tostring | test("gain.?map|hdrgm|21496"; "i"))) or
  ((.key | test("TransferCharacteristics$|TransferFunction$")) and
    (.value | tostring | test("PQ|HLG|2084|B67|^(16|18)$"; "i")))
)) | map(.key) | join(",")')
if [ -n "$hdr_reasons" ]; then
  printf '[jxl] HDR detection matched tags: %s\n' "$hdr_reasons"
  echo '[jxl] route=hdr-avif; preserving HDR through validated AVIF conversion or original-file passthrough; no JXL quality search'
  script_dir=$(CDPATH= cd -- "$(dirname "$0")" && pwd)
  exec sh "$script_dir/../avif/transcode-image.sh" "$src" "${dst%.*}.avif"
fi
echo '[jxl] route=sdr; optimizing with perceptual JXL'
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
