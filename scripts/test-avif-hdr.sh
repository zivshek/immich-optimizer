#!/bin/sh
# Behavioral checks for routing and fail-closed HDR validation. Requires jq.
# Encoder/metric mocks isolate the profile; the Docker build separately runs
# a real libavif Ultra HDR fixture conversion and decodes its gain map.
set -eu
repo=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
testdir=$(mktemp -d)
trap 'rm -rf "$testdir"' EXIT
mkdir -p "$testdir/bin" "$testdir/src" "$testdir/dst"
export TEST_LOG="$testdir/calls"
export PATH="$testdir/bin:$PATH"
cat > "$testdir/bin/exiftool" <<'EOF'
#!/bin/sh
case "$*" in
  *-j*)
    case "$TEST_CASE" in
      sdr) echo '[{"File:FileType":"JPEG","EXIF:BitsPerSample":"8 8 8","XMP-GCamera:HDRPlusMakerNote":"binary"}]' ;;
      heif) echo '[{"File:FileType":"HEIC"}]' ;;
      *) echo '[{"File:FileType":"JPEG","XMP-hdrgm:Version":1,"Google:GainMapImage":"binary"}]' ;;
    esac ;;
  *) echo exiftool >> "$TEST_LOG" ;;
esac
EOF
cat > "$testdir/bin/avifenc" <<'EOF'
#!/bin/sh
echo "avifenc $*" >> "$TEST_LOG"
for last do :; done
printf encoded > "$last"
EOF
cat > "$testdir/bin/avifdec" <<'EOF'
#!/bin/sh
echo avifdec >> "$TEST_LOG"
case "$TEST_CASE" in
  missing) echo ' * Gain map       : Absent' ;;
  broken) exit 1 ;;
  rewrite)
    if grep -q exiftool "$TEST_LOG"; then
      echo ' * Gain map       : Absent'
    else
      echo ' * Gain map       : 32x24 pixels, 8 bit, YUV400'
    fi ;;
  *) echo ' * Gain map       : 32x24 pixels, 8 bit, YUV400' ;;
esac
EOF
cat > "$testdir/bin/magick" <<'EOF'
#!/bin/sh
echo magick >> "$TEST_LOG"
for last do :; done
printf encoded > "$last"
EOF
cat > "$testdir/bin/fssimu2" <<'EOF'
#!/bin/sh
echo 90
EOF
chmod +x "$testdir/bin/"*
printf original > "$testdir/src/photo.jpg"
for TEST_CASE in hdr missing broken rewrite heif sdr; do
  export TEST_CASE
  : > "$TEST_LOG"
  rm -f "$testdir/dst/"*
  status=0
  sh "$repo/config/perceptual/avif/transcode-image.sh" \
    "$testdir/src/photo.jpg" "$testdir/dst/photo.avif" > "$testdir/output" 2>&1 || status=$?
  case "$TEST_CASE" in
    hdr)
      [ "$status" = 0 ]
      [ -s "$testdir/dst/photo.avif" ]
      grep -q -- '--qgain-map 100' "$TEST_LOG"
      ! grep -q magick "$TEST_LOG"
      [ "$(tail -n 1 "$TEST_LOG")" = avifdec ] ;;
    missing|broken|rewrite)
      [ "$status" != 0 ]
      ! grep -q magick "$TEST_LOG" ;;
    heif)
      [ "$status" = 0 ]
      cmp "$testdir/src/photo.jpg" "$testdir/dst/photo.jpg"
      ! grep -q 'avifenc\|magick' "$TEST_LOG" ;;
    sdr)
      [ "$status" = 0 ]
      grep -q magick "$TEST_LOG"
      ! grep -q avifenc "$TEST_LOG" ;;
  esac
  echo "PASS: $TEST_CASE"
done
