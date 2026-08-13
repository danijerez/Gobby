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

// Containers browsers can't play natively but ffmpeg can remux to fragmented
// MP4 on the fly. mp4/webm/mov/m4v are already browser-native — served as-is.
var remuxExts = map[string]bool{".mkv": true, ".avi": true}

// needsRemux reports whether a file must go through ffmpeg to play in a browser.
func needsRemux(path string) bool {
	return remuxExts[strings.ToLower(filepath.Ext(path))]
}

// Codecs that live happily inside an mp4 and that browsers decode natively — so
// we can `-c copy` them (zero CPU). Anything else gets transcoded.
var mp4VideoOK = map[string]bool{"h264": true, "hevc": true, "av1": true}

// Only codecs browsers actually DECODE — AC3/EAC3/DTS fit in mp4 but Chrome and
// Firefox play them silently or not at all, so those get transcoded to AAC.
var mp4AudioOK = map[string]bool{"aac": true, "mp3": true}

// streamSubtitle extracts one subtitle track (by per-type index) as WebVTT to w.
// The <video> element loads it via a <track> element. Small and fast — subs are
// tiny text, so no streaming subtleties here.
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

// errStreamStarted means ffmpeg failed after the response body already began, so
// the handler must stay silent rather than write a (superfluous) error status.
var errStreamStarted = errors.New("stream already started")

// countingWriter tracks whether any bytes reached the client, so we know if the
// HTTP headers have been committed.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// streamRemux pipes an on-the-fly mkv/avi→mp4 remux to the response. It probes
// the source codecs first: H264/HEVC video is copied (near-zero CPU), older
// DivX/XviD/VC-1 is transcoded to H264; AAC/MP3/AC3 audio is copied, anything
// else (DTS/TrueHD/FLAC/PCM) is transcoded to AAC. Subtitles are dropped (mp4
// can't hold ASS/SRT). frag_keyframe+empty_moov makes the mp4 streamable over a
// pipe.
//
// Trade-off: a piped fragmented stream has no byte-range support, so the browser
// can't seek. Full playback works; scrubbing restarts from 0.
// ponytail: no seek on remuxed streams; add `-ss` per Range request if it matters.
func streamRemux(ctx context.Context, w http.ResponseWriter, binDir, path string, startSec float64, audioIdx int) error {
	bin, err := ffmpegPath(binDir)
	if err != nil {
		return err
	}
	vcodec, acodec, _ := probeMedia(ctx, binDir, path) // "" if probe failed — then we transcode to be safe

	// Video is copied whenever possible (h264/hevc/av1 → zero CPU). The rare old
	// DivX/XviD that can't sit in mp4 falls back to the native mpeg4 encoder — the
	// slim ffmpeg has no libx264, keeping the binary ~20MB.
	vArg := "mpeg4"
	if mp4VideoOK[vcodec] {
		vArg = "copy"
	}
	// Audio: copy only when the DEFAULT track (idx 0) is already mp4/browser-friendly.
	// A non-default pick may be any codec, so transcode it to be safe.
	aArg := "aac"
	if audioIdx == 0 && mp4AudioOK[acodec] {
		aArg = "copy"
	}

	var args []string
	if startSec > 0 {
		// -ss BEFORE -i = fast input seek: ffmpeg jumps near the timestamp using the
		// index instead of decoding from 0. Lands on the nearest keyframe.
		args = append(args, "-ss", strconv.FormatFloat(startSec, 'f', 3, 64))
	}
	args = append(args, "-i", path,
		"-map", "0:v:0", // first video
		"-map", "0:a:"+strconv.Itoa(audioIdx), // chosen audio track
		"-c:v", vArg)
	if vArg == "mpeg4" {
		// Decent quality for the fallback transcode without ballooning bitrate.
		args = append(args, "-q:v", "4")
	}
	args = append(args,
		"-c:a", aArg,
		"-sn", // subtitles are served separately as WebVTT tracks
		// delay_moov: fragmented mp4 otherwise refuses to write the header before the
		// first audio packets ("Cannot write moov atom before AC3 packets") and emits
		// an empty stream — the player just sits blank with no error.
		"-movflags", "frag_keyframe+empty_moov+delay_moov",
		"-f", "mp4",
		"pipe:1",
	)

	cw := &countingWriter{w: w}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdout = cw
	// Keep the tail of stderr: if ffmpeg aborts before writing anything, this is
	// the only clue why (unsupported codec, etc.).
	var errTail ringBuffer
	cmd.Stderr = &errTail
	w.Header().Set("Content-Type", "video/mp4")
	if err := cmd.Run(); err != nil {
		// If bytes already went out (headers sent), the response is committed — the
		// caller must NOT try to write a 500 (that's the "superfluous WriteHeader"
		// spam when the browser aborts a stream on seek/close). Signal that with a
		// sentinel the handler checks.
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

// probeMedia reads codec + duration in one `ffmpeg -i` pass (it prints them to
// stderr, then exits non-zero because there's no output — expected). Parsing its
// text output means we don't ship ffprobe at all (halves the download). On any
// failure codecs are "" (caller transcodes, the safe default) and duration 0.
func probeMedia(ctx context.Context, binDir, path string) (video, audio string, dur float64) {
	bin, err := ffmpegPath(binDir)
	if err != nil {
		return "", "", 0
	}
	// -i with no output: ffmpeg dumps the stream info to stderr and exits with an
	// error ("At least one output file must be specified") — we only want the info.
	out, _ := exec.CommandContext(ctx, bin, "-i", path).CombinedOutput()
	s := string(out)
	if m := reProbeVideo.FindStringSubmatch(s); m != nil {
		video = m[1]
	}
	if m := reProbeAudio.FindStringSubmatch(s); m != nil {
		audio = m[1]
	}
	if m := reProbeDur.FindStringSubmatch(s); m != nil {
		h, _ := strconv.Atoi(m[1])
		mi, _ := strconv.Atoi(m[2])
		se, _ := strconv.Atoi(m[3])
		frac, _ := strconv.ParseFloat("0."+m[4], 64)
		dur = float64(h*3600+mi*60+se) + frac
	}
	return video, audio, dur
}

// Track is one audio or subtitle stream. Idx is the per-type index (0-based) used
// for ffmpeg's -map 0:a:Idx / -map 0:s:Idx. Lang is the 3-letter tag ("spa") or "".
type Track struct {
	Idx  int    `json:"idx"`
	Lang string `json:"lang"`
}

// Tracks lists the selectable audio and subtitle streams of a file.
type Tracks struct {
	Audio []Track `json:"audio"`
	Subs  []Track `json:"subs"`
}

// Stream #0:2(eng): Audio: ...  /  Stream #0:4(spa): Subtitle: ...
var reProbeStream = regexp.MustCompile(`Stream #\d+:\d+(?:\(([a-z]{2,3})\))?[^:]*: (Audio|Subtitle):`)

// probeTracks lists audio + subtitle streams (with language) via one `ffmpeg -i`.
// Per-type indices are assigned in appearance order, matching ffmpeg's 0:a:N/0:s:N.
func probeTracks(ctx context.Context, binDir, path string) Tracks {
	var t Tracks
	bin, err := ffmpegPath(binDir)
	if err != nil {
		return t
	}
	out, _ := exec.CommandContext(ctx, bin, "-i", path).CombinedOutput()
	for _, m := range reProbeStream.FindAllStringSubmatch(string(out), -1) {
		lang, kind := m[1], m[2]
		if kind == "Audio" {
			t.Audio = append(t.Audio, Track{Idx: len(t.Audio), Lang: lang})
		} else {
			t.Subs = append(t.Subs, Track{Idx: len(t.Subs), Lang: lang})
		}
	}
	return t
}

// ringBuffer keeps only the last ~4KB written — enough to hold ffmpeg's final
// error lines without buffering a whole session of progress spam.
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
		return strings.TrimSpace(lines[len(lines)-1]) // the decisive last line
	}
	return s
}

// ffmpegDownloadMu serializes the one-time auto-download so two near-simultaneous
// stream requests don't both fetch (and race on) the same file.
var ffmpegDownloadMu sync.Mutex

// ffmpegName is the platform's ffmpeg filename.
func ffmpegName() string {
	if runtime.GOOS == "windows" {
		return "ffmpeg.exe"
	}
	return "ffmpeg"
}

// ffmpegPath returns a runnable ffmpeg: a slim custom build placed next to the data
// dir (binDir) or next to the binary itself, one on PATH, or — failing all that —
// the slim build auto-downloaded from Gobby's own GitHub release into the binary's
// folder (like cloudflared). Kept as a separate file, never embedded, so the exe
// stays small and the ffmpeg build (see build/ffmpeg-slim/) stays swappable.
//
// binDir is the scanned folder (where gobby.db lives), which is NOT the binary's
// folder when -p points elsewhere — so we also look next to the exe. Otherwise a
// `gobby -p E:\movies` run would fail on every remux because ffmpeg sits by the exe.
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

// downloadFFmpeg fetches the slim ffmpeg asset from Gobby's latest GitHub release
// into the binary's folder and returns its path. Only one download runs at a time.
func downloadFFmpeg() (string, error) {
	ffmpegDownloadMu.Lock()
	defer ffmpegDownloadMu.Unlock()

	name := ffmpegName()
	dest := filepath.Join(binaryDir(), name)
	if fileExists(dest) { // a concurrent request already downloaded it
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

