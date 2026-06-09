#!/bin/sh
set -eu

if [ "${IUO_USE_NVIDIA:-0}" = "1" ]; then
  exec sh ./transcode-video-nvidia.sh "$@"
fi

exec sh ./transcode-video-cpu.sh "$@"
