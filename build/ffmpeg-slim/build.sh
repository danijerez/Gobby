#!/usr/bin/env bash
# Builds the slim Windows ffmpeg.exe Gobby uses to remux mkv/avi → mp4.
#
# Produces a ~20MB ffmpeg with ALL demuxers/decoders/parsers (plays anything) but
# only the mp4/mov muxer + aac/mpeg4 encoders (Gobby only copies video / encodes
# audio to AAC) — a fraction of the ~100-170MB public builds.
#
# Needs: Docker running. Docker is used ONLY here to produce the binary; Gobby
# never needs it at runtime.
#
# Usage:
#   ./build.sh              # build and drop ffmpeg.exe in this folder
#   ./build.sh /some/dir    # also copy the result to /some/dir/ffmpeg.exe
set -euo pipefail

FF_VERSION=7.1
DIR="$(cd "$(dirname "$0")" && pwd)"
OUT_EXTRA="${1:-}"

cd "$DIR"

# 1. Source tarball into the build context. The Docker daemon here has no outbound
#    network, so download on the host and COPY it in.
if [ ! -f ff.dl ]; then
  echo ">> downloading ffmpeg-${FF_VERSION} source..."
  curl -fSL --retry 3 --connect-timeout 20 \
    -o ff.dl "https://ffmpeg.org/releases/ffmpeg-${FF_VERSION}.tar.xz"
fi

# 2. Cross-compile in Docker.
echo ">> building slim ffmpeg (this takes a few minutes)..."
docker build -t gobby-ffmpeg-slim "$DIR"

# 3. Extract the exe out of the image.
echo ">> extracting ffmpeg.exe..."
CID=$(docker create gobby-ffmpeg-slim)
docker cp "$CID:/out/ffmpeg.exe" "$DIR/ffmpeg.exe"
docker rm "$CID" >/dev/null

# 4. Clean the source tarball (keep the repo light).
rm -f ff.dl

SIZE=$(ls -la "$DIR/ffmpeg.exe" | awk '{print $5}')
echo ">> done: $DIR/ffmpeg.exe ($SIZE bytes)"

if [ -n "$OUT_EXTRA" ]; then
  cp "$DIR/ffmpeg.exe" "$OUT_EXTRA/ffmpeg.exe"
  echo ">> copied to $OUT_EXTRA/ffmpeg.exe"
fi
