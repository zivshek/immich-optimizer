#!/bin/sh
set -eu

src=$1
dst=$2
current_stage=metadata-probe
candidate=
score_dir=
log_stage() {
  current_stage=$1
  printf '[avif] stage=%s %s\n' "$current_stage" "${2:-}"
}
finish() {
  result=$?
  if [ "$result" -ne 0 ]; then
    printf '[avif] result=failed stage=%s exit=%s; original-file fallback required\n' "$current_stage" "$result" >&2
  fi
  [ -z "$candidate" ] || rm -f "$candidate"
  [ -z "$score_dir" ] || rm -rf "$score_dir"
  exit "$result"
}
trap finish EXIT
log_stage metadata-probe
# libavif's JPEG reader converts supported Ultra HDR XMP gain maps into
# native AVIF gain maps. Never flatten an HDR source through ImageMagick.
metadata=$(exiftool -j -G1 -s -api ExtractEmbedded=1 "$src")
printf '%s' "$metadata" | jq -e 'length == 1 and (.[0] | has("ExifTool:Error") | not)' >/dev/null
printf '%s' "$metadata" | jq -r '.[0] | to_entries[] | select(.key | test("(^File:FileType$|ImageWidth$|ImageHeight$|BitsPerSample$|BitDepth$)")) | "[avif] source \(.key)=\(.value)"'
hdr_jpeg=0
hdr_reasons=$(printf '%s' "$metadata" | jq -r '.[0] | to_entries | map(select(
  (.key | test("GainMap|HDRCapacity|XMP-hdrgm:|AuxiliaryImage|MPImage|JUMBF"; "i")) or
  (.key | test("FileType$")) and (.value | tostring | test("HEIC|HEIF|HIF"; "i")) or
  (.key | test("BitDepth$|BitsPerSample$")) and (.value | tostring | test("^(10|12|16|32|64)( |$)")) or
  (.key | test("TransferCharacteristics$|TransferFunction$")) and (.value | tostring | test("PQ|HLG|2084|B67|^(16|18)$"; "i"))
)) | .[].key' | paste -sd ',' -)
if [ -n "$hdr_reasons" ]; then
  printf '[avif] HDR detection matched tags: %s\n' "$hdr_reasons"
  if printf '%s' "$metadata" | jq -e '.[0]["File:FileType"] == "JPEG"' >/dev/null; then
    hdr_jpeg=1
    echo "[avif] route=gain-map-jpeg (HDR metadata detected; gain-map pixels not yet verified). Ultra HDR JPEG: encoding AVIF with a lossless gain map; image score evaluates the SDR base"
  else
    echo "[avif] result=passthrough reason=unsupported-HDR-source; unsupported HDR/HEIF/high-depth source: retaining original to preserve HDR"
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

score_dir=$(mktemp -d)
log_stage prepare-score-input "target=${target_score} hdr_jpeg=${hdr_jpeg}"
score_source=$src
if [ "$hdr_jpeg" = "1" ]; then
  score_source="${score_dir}/source.png"
  # JPEG's first image is the SDR base; keep its profile for color-aware scoring.
  magick "${src}[0]" "$score_source"
fi

validate_gain_map() {
  # --info decodes both the base and gain-map images, rather than trusting XMP
  # tags that could remain even if the actual auxiliary pixels were lost.
  log_stage validate-gain-map "file=$(basename "$1")"
  info=$(avifdec --info "$1")
  printf '%s\n' "$info" | grep -E 'Gain map[[:space:]]*:|Base Image is' || true
  if ! printf '%s\n' "$info" | grep -Eq 'Gain map[[:space:]]*:[[:space:]]*[1-9][0-9]*x[1-9][0-9]* pixels'; then
    echo "HDR validation failed: missing decodable AVIF gain map; retaining original" >&2
    exit 1
  fi
}
encode_image() {
  log_stage encode "quality=$1 file=$(basename "$2") hdr_jpeg=${hdr_jpeg}"
  if [ "$hdr_jpeg" = "1" ]; then
    # Preserve the original JPEG base depth/profile; 10-bit is not needed for
    # an SDR-base gain-map representation. The gain map is encoded losslessly.
    if avifenc -q "$1" --qgain-map 100 --speed 6 "$src" "$2" > "${score_dir}/encoder.log" 2>&1; then
      printf '[avif] encode completed: bytes=%s\n' "$(wc -c < "$2" | tr -d '[:space:]')"
    else
      result=$?
      cat "${score_dir}/encoder.log" >&2
      return "$result"
    fi
    validate_gain_map "$2"
  else
    magick "$src" -quality "$1" "$2"
  fi
}

score_candidate() {
  score_input=$candidate
  if [ "$hdr_jpeg" = "1" ]; then
    score_input="${score_dir}/candidate.png"
    # avifdec defaults to the base image, without applying its auxiliary gain map.
    # Avoid fssimu2's AVIF decoding path for these gain-map containers.
    log_stage decode-score-input "format=PNG base=SDR"
    avifdec "$candidate" "$score_input" >/dev/null
  fi
  log_stage score "quality=${quality:-$best_quality} target=${target_score}"
  if score_output=$(fssimu2 "$score_source" "$score_input" 2>&1); then
    :
  else
    result=$?
    printf '[avif] tool=fssimu2 exit=%s (132 commonly indicates an illegal instruction)\n' "$result" >&2
    printf 'SSIMULACRA2 scoring failed: %s\n' "$score_output" >&2
    exit 1
  fi
  score=$(printf '%s\n' "$score_output" | tail -n 1 | tr -d '[:space:]')
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

log_stage restore-metadata
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

log_stage complete "result=optimized quality=${best_quality} score=${best_score} target=${target_score} bytes=$(wc -c < "$dst" | tr -d '[:space:]') hdr_gain_map=${hdr_jpeg}"
