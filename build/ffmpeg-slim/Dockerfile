# Cross-compiles a slim static Windows ffmpeg.exe for Gobby's mkv/avi→mp4 remux.
# All demuxers/decoders/parsers (plays anything) but only mp4/mov muxer +
# aac/mpeg4 encoders (Gobby copies video, re-encodes audio to AAC) → ~20MB vs the
# ~100-170MB public builds.
#
# Don't run docker by hand — use ./build.sh, which downloads the source into the
# build context (this daemon has no network), builds, and drops ffmpeg.exe here.
# Place the resulting ffmpeg.exe next to the Gobby binary. Docker is needed ONLY
# to produce it; Gobby never uses Docker at runtime.

FROM debian:bookworm-slim

ARG FF_VERSION=7.1

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential gcc-mingw-w64-x86-64 pkg-config yasm nasm \
    curl xz-utils ca-certificates make \
 && rm -rf /var/lib/apt/lists/*

# ponytail: skipped mingw zlib. Some mkv log "Unsupported encoding type" on a
# compressed side track, but A/V remux fine. Add a cross-built zlib + --enable-zlib
# only if a real file fails to play because of it.

WORKDIR /build
# Source tarball is downloaded on the host into the build context (the Docker
# daemon here has no outbound network). Fetch it with:
#   curl -fSL -o build/ffmpeg-slim/ff.dl https://ffmpeg.org/releases/ffmpeg-7.1.tar.xz
COPY ff.dl ffmpeg.tar.xz
RUN tar xJf ffmpeg.tar.xz && rm ffmpeg.tar.xz
WORKDIR /build/ffmpeg-${FF_VERSION}

# Goal: play WHATEVER shows up. So enable ALL demuxers + decoders + parsers +
# file protocols (reading anything is cheap — decoders are small). Restrict only
# the OUTPUT side: mp4/mov muxer + aac/mpeg4 encoders, since Gobby only ever
# copies video and re-encodes audio to AAC. This skips the heavy video encoders
# (x264/x265/aom/vpx) that would balloon the binary, while reading everything.
RUN ./configure \
      --arch=x86_64 \
      --target-os=mingw32 \
      --cross-prefix=x86_64-w64-mingw32- \
      --enable-cross-compile \
      --disable-doc --disable-htmlpages --disable-manpages --disable-podpages --disable-txtpages \
      --disable-network \
      --disable-ffplay --disable-ffprobe \
      --disable-encoders --enable-encoder=aac,mpeg4,webvtt,mov_text \
      --disable-muxers --enable-muxer=mp4,mov,webvtt \
      --extra-ldflags=-static \
    && make -j"$(nproc)"

RUN mkdir /out && cp ffmpeg.exe /out/ffmpeg.exe \
 && x86_64-w64-mingw32-strip /out/ffmpeg.exe \
 && ls -lh /out/ffmpeg.exe
