#!/bin/sh
# Route/metadata/error tests. Real codec validation runs in the image build.
set -eu
repo=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
testdir=$(mktemp -d)
trap 'rm -rf "$testdir"' EXIT
mkdir -p "$testdir/bin" "$testdir/src" "$testdir/dst"
export PATH="$testdir/bin:$PATH" IUO_HDR_TOOLS_DIR="$testdir/bin"
export TEST_LOG="$testdir/calls"
cp "$repo/scripts/transcode-hdr-jxl.sh" "$testdir/bin/iuo-hdr-jxl"
cat > "$testdir/bin/iuo-hdr-jxl-codec" <<'EOF'
#!/bin/sh
echo "$1" >> "$TEST_LOG"
case "$1:$TEST_CASE" in encode:encode-failure|validate:validation-failure) exit 1 ;; esac
if [ "$1" = encode ]; then printf encoded > "$3"; fi
EOF
cat > "$testdir/bin/exiftool" <<'EOF'
#!/bin/sh
case "$*" in
  *-j*)
    case "$TEST_CASE" in
      unsupported) echo '[{"File:FileType":"PNG","PNG:TransferFunction":"PQ"}]' ;;
      *) echo '[{"File:FileType":"JPEG","XMP-hdrgm:Version":1}]' ;;
    esac ;;
  *-s3*)
    case "$TEST_CASE:$*" in
      metadata-loss:*photo.jxl*) : ;;
      *Orientation*) echo 1 ;;
      *) echo value ;;
    esac ;;
  *)
    echo "metadata $*" >> "$TEST_LOG"
    [ "$TEST_CASE" != metadata-error ] || exit 1 ;;
esac
EOF
chmod +x "$testdir/bin/"*
printf original > "$testdir/src/photo with spaces.jpg"
for profile in standard/lossless perceptual/jxl; do
  for TEST_CASE in success encode-failure validation-failure metadata-loss metadata-error unsupported; do
    export TEST_CASE
    : > "$TEST_LOG"
    rm -f "$testdir/dst/"*
    status=0
    sh "$repo/config/$profile/transcode-image.sh" \
      "$testdir/src/photo with spaces.jpg" "$testdir/dst/photo.jxl" > "$testdir/output" 2>&1 || status=$?
    case "$TEST_CASE" in
      success)
        [ "$status" = 0 ]
        [ -s "$testdir/dst/photo.jxl" ]
        [ "$(head -n 1 "$TEST_LOG")" = encode ]
        [ "$(tail -n 1 "$TEST_LOG")" = validate ]
        grep -q -- '-Orientation#=1' "$TEST_LOG"
        ! grep -q -- '-all:all\|-ICC_Profile' "$TEST_LOG"
        grep -q 'result=optimized format=JXL HDR=native' "$testdir/output" ;;
      unsupported)
        [ "$status" = 0 ]
        cmp "$testdir/src/photo with spaces.jpg" "$testdir/dst/photo with spaces.jpg"
        [ ! -s "$TEST_LOG" ] ;;
      *)
        [ "$status" != 0 ]
        [ ! -e "$testdir/dst/photo.jxl" ]
        grep -q 'original-file fallback required' "$testdir/output" ;;
    esac
    echo "PASS: native HDR $profile $TEST_CASE"
  done
done
