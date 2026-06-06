#!/bin/sh
set -eu

src=$1
dst=$2

# Keep the stored frame dimensions unchanged and preserve the source display
# rotation metadata. No scale, crop, or autorotation filter is applied.
ffmpeg -hide_banner -y \
  -noautorotate \
  -i "$src" \
  -map 0:v:0 \
  -map 0:a? \
  -map 0:s? \
  -map_metadata 0 \
  -map_chapters 0 \
  -c:v hevc_nvenc \
  -preset p7 \
  -tune hq \
  -rc vbr \
  -cq 18 \
  -b:v 0 \
  -profile:v main10 \
  -pix_fmt p010le \
  -spatial_aq 1 \
  -temporal_aq 1 \
  -rc-lookahead 32 \
  -c:a copy \
  -c:s copy \
  "$dst"
