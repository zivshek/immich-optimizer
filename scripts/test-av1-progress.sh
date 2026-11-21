#!/bin/sh
# Exercise live stage updates and failure propagation without encoding a video.
set -eu
repo=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
testdir=$(mktemp -d)
profile_pid=
trap '[ -z "$profile_pid" ] || kill "$profile_pid" 2>/dev/null || :; rm -rf "$testdir"' EXIT
mkdir -p "$testdir/bin" "$testdir/tmp" "$testdir/dst"
export TMPDIR="$testdir/tmp"
export TEST_INPUT="$testdir/input.mp4"
export PATH="$testdir/bin:$PATH"
printf original > "$TEST_INPUT"
cat > "$testdir/bin/ffprobe" <<'EOF'
#!/bin/sh
case "$*" in
  *codec_tag_string*) exit 0 ;;
  *codec_name*) echo h264; exit 0 ;;
esac
for input do :; done
if [ "$input" = "$TEST_INPUT" ]; then
  [ "$TEST_SCENARIO" != probe-fail ] || exit 23
  if [ "$TEST_SCENARIO" = live ]; then sleep 6; fi
else
  [ "$TEST_SCENARIO" != validation-fail ] || exit 19
fi
duration=60.0
[ "$TEST_SCENARIO" != unknown-duration ] || duration=N/A
printf '{"streams":[{"pix_fmt":"yuv420p","color_transfer":"bt709","color_primaries":"bt709","color_space":"bt709","color_range":"tv"}],"frames":[],"format":{"duration":"%s"}}\n' "$duration"
EOF
cat > "$testdir/bin/ffmpeg" <<'EOF'
#!/bin/sh
progress=
next_is_progress=0
for arg do
  if [ "$next_is_progress" = 1 ]; then progress=$arg; next_is_progress=0; fi
  if [ "$arg" = -progress ]; then next_is_progress=1; fi
  output=$arg
done
[ -n "$progress" ] || exit 99
cat > "$progress" <<'PROGRESS'
frame=900
fps=30.0
total_size=1048576
out_time_us=30000000
out_time=00:00:30.000000
speed=1.20x
progress=continue
PROGRESS
if [ "$TEST_SCENARIO" = live ]; then sleep 6; fi
[ "$TEST_SCENARIO" != encode-fail ] || exit 17
cat >> "$progress" <<'PROGRESS'
frame=1800
fps=30.0
total_size=2097152
out_time_us=60000000
out_time=00:01:00.000000
speed=1.20x
progress=end
PROGRESS
printf encoded > "$output"
EOF
cat > "$testdir/bin/exiftool" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod +x "$testdir/bin/"*

export TEST_SCENARIO=live
sh "$repo/config/standard/av1-crf28/transcode-video.sh" \
  "$TEST_INPUT" "$testdir/dst/output.mp4" > "$testdir/log" 2>&1 &
profile_pid=$!
# The source probe is still decoding when its first heartbeat is emitted.
sleep 6
kill -0 "$profile_pid"
grep -Eq 'stage=probe-source elapsed=[1-9][0-9]*s scanning frame metadata' "$testdir/log"
wait "$profile_pid"
profile_pid=
grep -Eq 'stage=encode elapsed=[1-9][0-9]*s progress=50.0%.*speed=1.20x.*output_mib=1.0' "$testdir/log"
grep -q 'progress=100.0%' "$testdir/log"
grep -q 'stage=validate-output elapsed=' "$testdir/log"
grep -q 'stage=complete result=optimized' "$testdir/log"
[ -z "$(ls -A "$TMPDIR")" ]
echo 'PASS: live scan heartbeat, encode percentage/speed/size, validation and cleanup'

for TEST_SCENARIO in probe-fail encode-fail validation-fail unknown-duration; do
  export TEST_SCENARIO
  status=0
  sh "$repo/config/standard/av1-crf28/transcode-video.sh" \
    "$TEST_INPUT" "$testdir/dst/output.mp4" > "$testdir/log" 2>&1 || status=$?
  case "$TEST_SCENARIO" in
    probe-fail) expected=23; stage=probe-source ;;
    encode-fail) expected=17; stage=encode ;;
    validation-fail) expected=19; stage=validate-output ;;
    unknown-duration)
      [ "$status" = 0 ]
      grep -q 'stage=encode .*media_time=00:01:00.000000' "$testdir/log"
      ! grep -q 'progress=[0-9]' "$testdir/log"
      grep -q 'stage=complete result=optimized' "$testdir/log"
      expected=0 ;;
  esac
  [ "$status" = "$expected" ]
  if [ "$status" != 0 ]; then
    grep -q "result=failed stage=$stage exit=$expected" "$testdir/log"
    ! grep -q 'stage=complete result=optimized' "$testdir/log"
  fi
  [ -z "$(ls -A "$TMPDIR")" ]
  echo "PASS: $TEST_SCENARIO"
done
