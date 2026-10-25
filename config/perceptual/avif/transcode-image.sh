#!/bin/sh
set -eu

src=$1
dst=$2
# libavif's JPEG reader converts supported Ultra HDR XMP gain maps into
# native AVIF gain maps. Never flatten an HDR source through ImageMagick.
metadata=$(exiftool -j -G1 -s -api ExtractEmbedded=1 "$src")
printf '%s' "$metadata" | jq -e 'length == 1 and (.[0] | has("ExifTool:Error") | not)' >/dev/null
hdr_jpeg=0
if printf '%s' "$metadata" | jq -e '.[0] | to_entries | any(
  (.key | test("GainMap|HDRCapacity|XMP-hdrgm:|AuxiliaryImage|MPImage|JUMBF"; "i")) or
  (.key | test("FileType$")) and (.value | tostring | test("HEIC|HEIF|HIF"; "i")) or
  (.key | test("BitDepth$|BitsPerSample$")) and (.value | tostring | test("^(10|12|16|32|64)( |$)")) or
  (.key | test("TransferCharacteristics$|TransferFunction$")) and (.value | tostring | test("PQ|HLG|2084|B67|^(16|18)$"; "i"))
)' >/dev/null; then
  if printf '%s' "$metadata" | jq -e '.[0]["File:FileType"] == "JPEG"' >/dev/null; then
    hdr_jpeg=1
    echo "Ultra HDR JPEG: encoding AVIF with a lossless gain map; image score evaluates the SDR base"
  else
    echo "unsupported HDR/HEIF/high-depth source: retaining original to preserve HDR"
    cp "$src" "$(dirname "$dst")/$(basename "$src")"
    exit 0
  fi
fi
workdir=$(dirname "$dst")
candidate="${workdir}/candidate.avif"
target_score="${IUO_IMAGE_SCORE:-85}"
low=1
high=100
best_quality=
best_score=

trap 'rm -f "$candidate"' EXIT

validate_gain_map() {
  # --info decodes both the base and gain-map images, rather than trusting XMP
  # tags that could remain even if the actual auxiliary pixels were lost.
  info=$(avifdec --info "$1")
  if ! printf '%s\n' "$info" | grep -Eq 'Gain map[[:space:]]*:[[:space:]]*[1-9][0-9]*x[1-9][0-9]* pixels'; then
    echo "HDR validation failed: missing decodable AVIF gain map; retaining original" >&2
    exit 1
  fi
}
encode_image() {
  if [ "$hdr_jpeg" = "1" ]; then
    # Preserve the original JPEG base depth/profile; 10-bit is not needed for
    # an SDR-base gain-map representation. The gain map is encoded losslessly.
    avifenc -q "$1" --qgain-map 100 --speed 6 "$src" "$2"
    validate_gain_map "$2"
  else
    magick "$src" -quality "$1" "$2"
  fi
}

score_candidate() {
  score=$(fssimu2 "$src" "$candidate" 2>&1 | tail -n 1 | tr -d '[:space:]')
  if ! printf '%s\n' "$score" | grep -Eq '^-?[0-9]+([.][0-9]+)?$'; then
    echo "unable to read SSIMULACRA2 score from fssimu2 output: ${score}" >&2
    exit 1
  fi
}

while [ "$low" -le "$high" ]; do
  quality=$(( (low + high) / 2 ))
  encode_image "$quality" "$candidate"
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
  encode_image "$best_quality" "$candidate"
  score_candidate
  best_score=$score
  echo "warning: AVIF could not meet SSIMULACRA2 ${target_score}; using quality ${best_quality} at score ${best_score}" >&2
fi

echo "selected AVIF quality ${best_quality} at SSIMULACRA2 ${best_score}"
encode_image "$best_quality" "$dst"

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

# ExifTool rewrites the container; validate again after metadata restoration.
if [ "$hdr_jpeg" = "1" ]; then
  validate_gain_map "$dst"
fi
