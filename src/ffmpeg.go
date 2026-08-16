package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var remuxExts = map[string]bool{".mkv": true, ".avi": true}

func needsRemux(path string) bool {
	return remuxExts[strings.ToLower(filepath.Ext(path))]
}

var mp4VideoOK = map[string]bool{"h264": true, "hevc": true, "av1": true}

var mp4AudioOK = map[string]bool{"aac": true, "mp3": true}

func streamSubtitle(ctx context.Context, w http.ResponseWriter, binDir, path string, subIdx int) error {
	var buf bytes.Buffer
	err := runFF(ctx, binDir, path, []string{
		"-map", "0:s:" + strconv.Itoa(subIdx),
		"-f", "webvtt",
		"pipe:1",
	}, &buf, nil)
	if err != nil {
		return fmt.Errorf("no se pudo extraer subtítulo %d: %w", subIdx, err)
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Write(buf.Bytes())
	return nil
}

var errStreamStarted = errors.New("stream already started")

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

func streamRemux(ctx context.Context, w http.ResponseWriter, binDir, path string, startSec float64, audioIdx int) error {
	vcodec, acodec, _ := probeMedia(ctx, binDir, path)

	vArg := "mpeg4"
	if mp4VideoOK[vcodec] {
		vArg = "copy"
	}

	aArg := "aac"
	if audioIdx == 0 && mp4AudioOK[acodec] {
		aArg = "copy"
	}

	var pre []string
	if startSec > 0 {
		pre = append(pre, "-ss", strconv.FormatFloat(startSec, 'f', 3, 64))
	}
	args := append(pre,
		"-map", "0:v:0",
		"-map", "0:a:"+strconv.Itoa(audioIdx),
		"-c:v", vArg)
	if vArg == "mpeg4" {
		args = append(args, "-q:v", "4")
	}
	args = append(args,
		"-c:a", aArg,
		"-sn",
		"-movflags", "frag_keyframe+empty_moov+delay_moov",
		"-f", "mp4",
		"pipe:1",
	)

	cw := &countingWriter{w: w}
	var errTail ringBuffer
	w.Header().Set("Content-Type", "video/mp4")
	if err := runFF(ctx, binDir, path, args, cw, &errTail); err != nil {
		if cw.n > 0 {
			return errStreamStarted
		}
		size := int64(-1)
		if fi, e := os.Stat(path); e == nil {
			size = fi.Size()
		}
		return fmt.Errorf("ffmpeg (v=%s→%s a=%s→%s) path=%q size=%d: %s", vcodec, vArg, acodec, aArg, path, size, errTail.String())
	}
	return nil
}

var (
	reProbeDur   = regexp.MustCompile(`Duration: (\d+):(\d+):(\d+)\.(\d+)`)
	reProbeVideo = regexp.MustCompile(`Stream #\d+:\d+.*: Video: (\w+)`)
	reProbeAudio = regexp.MustCompile(`Stream #\d+:\d+.*: Audio: (\w+)`)
)

func probeMedia(ctx context.Context, binDir, path string) (video, audio string, dur float64) {
	s, _ := ffmpegInfo(ctx, binDir, path)
	if m := reProbeVideo.FindStringSubmatch(s); m != nil {
		video = m[1]
	}
	if m := reProbeAudio.FindStringSubmatch(s); m != nil {
		audio = m[1]
	}
	return video, audio, parseDuration(s)
}

type Track struct {
	Idx  int    `json:"idx"`
	Lang string `json:"lang"`
}

type Tracks struct {
	Audio []Track `json:"audio"`
	Subs  []Track `json:"subs"`
}

var reProbeStream = regexp.MustCompile(`Stream #\d+:\d+(?:\(([a-z]{2,3})\))?[^:]*: (Audio|Subtitle):`)

func probeTracks(ctx context.Context, binDir, path string) Tracks {
	out, _ := ffmpegInfo(ctx, binDir, path)
	return parseTracks(out)
}

func probeAll(ctx context.Context, binDir, path string) (dur float64, t Tracks) {
	out, _ := ffmpegInfo(ctx, binDir, path)
	return parseDuration(out), parseTracks(out)
}

func ffmpegInfo(ctx context.Context, binDir, path string) (string, error) {
	var buf bytes.Buffer
	// ffmpeg with only -i prints stream info to stderr then exits non-zero; that's fine.
	_ = runFF(ctx, binDir, path, nil, &buf, &buf)
	return buf.String(), nil
}

func parseDuration(s string) float64 {
	m := reProbeDur.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	h, _ := strconv.Atoi(m[1])
	mi, _ := strconv.Atoi(m[2])
	se, _ := strconv.Atoi(m[3])
	frac, _ := strconv.ParseFloat("0."+m[4], 64)
	return float64(h*3600+mi*60+se) + frac
}

func parseTracks(s string) Tracks {
	var t Tracks
	for _, m := range reProbeStream.FindAllStringSubmatch(s, -1) {
		lang, kind := m[1], m[2]
		if kind == "Audio" {
			t.Audio = append(t.Audio, Track{Idx: len(t.Audio), Lang: lang})
		} else {
			t.Subs = append(t.Subs, Track{Idx: len(t.Subs), Lang: lang})
		}
	}
	return t
}

type ringBuffer struct{ buf []byte }

func (b *ringBuffer) Write(p []byte) (int, error) {
	const max = 4096
	b.buf = append(b.buf, p...)
	if len(b.buf) > max {
		b.buf = b.buf[len(b.buf)-max:]
	}
	return len(p), nil
}

func (b *ringBuffer) String() string {
	s := strings.TrimSpace(string(bytes.ReplaceAll(b.buf, []byte("\r"), []byte("\n"))))
	if lines := strings.Split(s, "\n"); len(lines) > 0 {
		return strings.TrimSpace(lines[len(lines)-1])
	}
	return s
}

