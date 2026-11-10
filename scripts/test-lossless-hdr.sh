#!/bin/sh
# Behavioral tests: HDR originals, strict JPEG reconstruction, pixel-lossless
# flags, metadata validation, and original-file fallback on tool failures.
set -eu
repo=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
testdir=$(mktemp -d)
trap 'rm -rf "$testdir"' EXIT
mkdir -p "$testdir/bin" "$testdir/src" "$testdir/dst"
export TEST_LOG="$testdir/calls" TEST_METADATA="$testdir/metadata"
export TEST_SOURCE="$testdir/src/photo with spaces.jpg"
export PATH="$testdir/bin:$PATH"
cat > "$testdir/bin/exiftool" <<'EOF'
#!/bin/sh
case "$*" in
  *-j*) cat "$TEST_METADATA" ;;
  *-s3*)
    case "$TEST_CASE:$*" in metadata-loss:*photo.jxl*) : ;; *) echo value ;; esac ;;
  *) echo metadata-copy >> "$TEST_LOG" ;;
esac
EOF
cat > "$testdir/bin/cjxl" <<'EOF'
#!/bin/sh
echo "cjxl $*" >> "$TEST_LOG"
[ "$TEST_CASE" != encoder-error ] || exit 1
for last do :; done
printf encoded > "$last"
EOF
cat > "$testdir/bin/djxl" <<'EOF'
#!/bin/sh
echo djxl >> "$TEST_LOG"
[ "$TEST_CASE" != decoder-error ] || exit 1
if [ "$TEST_CASE" = reconstruction-mismatch ]; then
  printf altered > "$2"
else
  cp "$TEST_SOURCE" "$2"
fi
EOF
cat > "$testdir/bin/fssimu2" <<'EOF'
#!/bin/sh
echo unexpected-score >> "$TEST_LOG"
exit 1
EOF
chmod +x "$testdir/bin/"*
printf original > "$TEST_SOURCE"
for TEST_CASE in jpeg png png16 sdr-c2pa sdr-depth sdr-hdrplus gainmap jumbf aux-hdr headroom pq hlg unsupported probe-error malformed encoder-error decoder-error reconstruction-mismatch metadata-loss; do
  export TEST_CASE
  case "$TEST_CASE" in
    png|metadata-loss) metadata='{"File:FileType":"PNG"}' ;;
    png16) metadata='{"File:FileType":"PNG","PNG:BitDepth":16}' ;;
    sdr-c2pa) metadata='{"File:FileType":"JPEG","JUMBF:JUMDLabel":"c2pa"}' ;;
    sdr-depth) metadata='{"File:FileType":"JPEG","MPF:NumberOfImages":2,"XMP:AuxiliaryImageType":"depth"}' ;;
    sdr-hdrplus) metadata='{"File:FileType":"JPEG","XMP-GCamera:HDRPlusMakerNote":"binary"}' ;;
    gainmap) metadata='{"File:FileType":"JPEG","XMP-hdrgm:Version":1}' ;;
    jumbf) metadata='{"File:FileType":"JPEG","JUMBF:JUMDLabel":"hdrgainmap"}' ;;
    aux-hdr) metadata='{"File:FileType":"JPEG","XMP:AuxiliaryImageType":"urn:hdrgainmap"}' ;;
    headroom) metadata='{"File:FileType":"JPEG","Apple:HDRHeadroom":3}' ;;
    pq) metadata='{"File:FileType":"PNG","PNG:TransferFunction":"PQ"}' ;;
    hlg) metadata='{"File:FileType":"PNG","PNG:TransferCharacteristics":18}' ;;
    unsupported) metadata='{"File:FileType":"EXR"}' ;;
    probe-error) metadata='{"ExifTool:Error":"corrupt metadata"}' ;;
    *) metadata='{"File:FileType":"JPEG"}' ;;
  esac
  printf '[%s]\n' "$metadata" > "$TEST_METADATA"
  [ "$TEST_CASE" != malformed ] || printf 'invalid json' > "$TEST_METADATA"
  : > "$TEST_LOG"
  rm -f "$testdir/dst/"*
  status=0
  sh "$repo/config/standard/lossless/transcode-image.sh" \
    "$TEST_SOURCE" "$testdir/dst/photo.jxl" > "$testdir/output" 2>&1 || status=$?
  ! grep -q unexpected-score "$TEST_LOG"
  ! ls "$testdir/dst/"reconstructed-* >/dev/null 2>&1
  case "$TEST_CASE" in
    gainmap|jumbf|aux-hdr|headroom|pq|hlg|unsupported)
      [ "$status" = 0 ]
      cmp "$TEST_SOURCE" "$testdir/dst/$(basename "$TEST_SOURCE")"
      [ ! -e "$testdir/dst/photo.jxl" ]
      [ ! -s "$TEST_LOG" ] ;;
    probe-error|malformed|encoder-error|decoder-error|reconstruction-mismatch|metadata-loss)
      [ "$status" != 0 ] ;;
    png|png16)
      [ "$status" = 0 ]
      [ -s "$testdir/dst/photo.jxl" ]
      grep -q -- '--lossless_jpeg=0 -d 0' "$TEST_LOG"
      grep -q metadata-copy "$TEST_LOG" ;;
    *)
      [ "$status" = 0 ]
      [ -s "$testdir/dst/photo.jxl" ]
      grep -q -- '--lossless_jpeg=1' "$TEST_LOG"
      grep -q djxl "$TEST_LOG"
      ! grep -q metadata-copy "$TEST_LOG" ;;
  esac
  echo "PASS: lossless $TEST_CASE"
done
