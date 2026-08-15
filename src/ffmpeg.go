package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

var remuxExts = map[string]bool{".mkv": true, ".avi": true}

func needsRemux(path string) bool {
	return remuxExts[strings.ToLower(filepath.Ext(path))]
}

var mp4VideoOK = map[string]bool{"h264": true, "hevc": true, "av1": true}

var mp4AudioOK = map[string]bool{"aac": true, "mp3": true}

func streamSubtitle(ctx context.Context, w http.ResponseWriter, binDir, path string, subIdx int) error {
	bin, err := ffmpegPath(binDir)
	if err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, bin,
		"-i", path,
		"-map", "0:s:"+strconv.Itoa(subIdx),
		"-f", "webvtt",
		"pipe:1",
	).Output()
	if err != nil {
		return fmt.Errorf("no se pudo extraer subtítulo %d: %w", subIdx, err)
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Write(out)
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
	bin, err := ffmpegPath(binDir)
	if err != nil {
		return err
	}
	vcodec, acodec, _ := probeMedia(ctx, binDir, path)

	vArg := "mpeg4"
	if mp4VideoOK[vcodec] {
		vArg = "copy"
	}

	aArg := "aac"
	if audioIdx == 0 && mp4AudioOK[acodec] {
		aArg = "copy"
	}

	var args []string
	if startSec > 0 {

		args = append(args, "-ss", strconv.FormatFloat(startSec, 'f', 3, 64))
	}
	args = append(args, "-i", path,
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
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = cw
	var errTail ringBuffer
	cmd.Stderr = &errTail
	w.Header().Set("Content-Type", "video/mp4")
	if err := cmd.Run(); err != nil {

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
	bin, err := ffmpegPath(binDir)
	if err != nil {
		return "", "", 0
	}

	out, _ := exec.CommandContext(ctx, bin, "-i", path).CombinedOutput()
	s := string(out)
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
	bin, err := ffmpegPath(binDir)
	if err != nil {
		return "", err
	}
	out, _ := exec.CommandContext(ctx, bin, "-i", path).CombinedOutput()
	return string(out), nil
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

var ffmpegDownloadMu sync.Mutex

func ffmpegName() string {
	if runtime.GOOS == "windows" {
		return "ffmpeg.exe"
	}
	return "ffmpeg"
}

func ffmpegPath(binDir string) (string, error) {
	name := ffmpegName()
	for _, dir := range []string{binDir, binaryDir()} {
		if local := filepath.Join(dir, name); fileExists(local) {
			return local, nil
		}
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p, nil
	}
	return downloadFFmpeg()
}

func downloadFFmpeg() (string, error) {
	ffmpegDownloadMu.Lock()
	defer ffmpegDownloadMu.Unlock()

	name := ffmpegName()
	dest := filepath.Join(binaryDir(), name)
	if fileExists(dest) {
		return dest, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rel, err := fetchLatestRelease(ctx)
	if err != nil {
		return "", fmt.Errorf("ffmpeg no encontrado y no se pudo consultar la release: %w — coloca %s junto a Gobby o instálalo en el PATH", err, name)
	}
	url := rel.findAsset("ffmpeg-")
	if url == "" {
		return "", fmt.Errorf("ffmpeg no encontrado — la release %s no trae binario para %s; coloca %s junto a Gobby o instálalo en el PATH", rel.Tag, assetSuffix(), name)
	}
	if err := downloadFile(url, dest); err != nil {
		return "", fmt.Errorf("no se pudo descargar ffmpeg: %w", err)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(dest, 0o755)
	}
	return dest, nil
}
