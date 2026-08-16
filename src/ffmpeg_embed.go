//go:build embedffmpeg

package main

import (
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"

	"codeberg.org/gruf/go-ffmpreg/ffmpreg"
	"codeberg.org/gruf/go-ffmpreg/wasm"
	"github.com/tetratelabs/wazero"
)

// This build embeds ffmpeg as WebAssembly (go-ffmpreg), so Gobby is a single
// self-contained binary with no external ffmpeg and no download. GPLv3.
//
// A native ffmpeg beside Gobby (or on PATH) still wins — it's faster and skips
// the wasm startup cost — so the embedded one is only the zero-setup fallback.

var ffmpegInit sync.Once

// nativeFFmpeg returns the path to a native ffmpeg beside the data dir, beside
// the binary, or on PATH — or "" if none. Cheap (a few os.Stat), no caching, so
// dropping an ffmpeg next to Gobby is picked up without restarting.
func nativeFFmpeg(binDir string) string {
	name := "ffmpeg"
	if runtime.GOOS == "windows" {
		name = "ffmpeg.exe"
	}
	for _, dir := range []string{binDir, binaryDir()} {
		if dir == "" {
			continue
		}
		if p := filepath.Join(dir, name); fileExists(p) {
			return p
		}
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p
	}
	return ""
}

// warmFFmpeg compiles the wasm module ahead of time (~5s) so the first playback
// doesn't pay that cost — skipped when a native ffmpeg is present.
func warmFFmpeg(binDir string) {
	if nativeFFmpeg(binDir) == "" {
		ffmpegInit.Do(ffmpreg.Initialize)
	}
}

// runFF prefers a native ffmpeg (faster, no wasm init); otherwise runs the
// embedded wasm build, mounting the input's directory at /in (read-only).
func runFF(ctx context.Context, binDir, inPath string, args []string, stdout, stderr io.Writer) error {
	if bin := nativeFFmpeg(binDir); bin != "" {
		cmd := exec.CommandContext(ctx, bin, append([]string{"-i", inPath}, args...)...)
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		return cmd.Run()
	}

	ffmpegInit.Do(ffmpreg.Initialize)
	dir := filepath.Dir(inPath)
	name := filepath.Base(inPath)
	full := append([]string{"-i", "/in/" + name}, args...)
	_, err := ffmpreg.Run(ctx, wasm.Args{
		Name:   "ffmpeg",
		Stdout: stdout,
		Stderr: stderr,
		Args:   full,
		Config: func(cfg wazero.ModuleConfig) wazero.ModuleConfig {
			fscfg := wazero.NewFSConfig().WithReadOnlyDirMount(dir, "/in")
			return cfg.WithFSConfig(fscfg)
		},
	})
	return err
}
