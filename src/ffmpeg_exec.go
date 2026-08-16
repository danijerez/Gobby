//go:build !embedffmpeg

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// warmFFmpeg is a no-op for the native build (nothing to precompile).
func warmFFmpeg(binDir string) {}

// runFF runs the external ffmpeg binary: `ffmpeg -i <inPath> <args...>`, writing
// to stdout/stderr (either may be nil). This is the default build; ffmpeg is a
// separate slim binary found beside Gobby, on PATH, or auto-downloaded.
func runFF(ctx context.Context, binDir, inPath string, args []string, stdout, stderr io.Writer) error {
	bin, err := ffmpegPath(binDir)
	if err != nil {
		return err
	}
	full := append([]string{"-i", inPath}, args...)
	cmd := exec.CommandContext(ctx, bin, full...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
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
