package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
)

// tunnel manages a single temporary Cloudflare quick-tunnel (trycloudflare.com),
// which exposes the local server to the public internet with no account. The URL
// is public and unauthenticated — the UI warns about this before starting.
type tunnel struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	url     string
	running bool
	ready   bool // the edge actually serves the origin (not just URL assigned)
	err     string
}

var reTryCloudflare = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

func (t *tunnel) status() map[string]any {
	t.mu.Lock()
	defer t.mu.Unlock()
	return map[string]any{"running": t.running, "url": t.url, "ready": t.ready, "error": t.err}
}

// start launches cloudflared against the given local port, discovering the
// public URL from its output. binDir is where an auto-downloaded binary lives.
// ctx is the server's lifetime: when it's cancelled (Ctrl+C / shutdown) the
// child cloudflared is killed too, so the tunnel never outlives Gobby.
func (t *tunnel) start(ctx context.Context, binDir, port string) error {
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return nil
	}
	t.err, t.url, t.ready = "", "", false
	t.mu.Unlock()

	bin, err := cloudflaredPath(binDir)
	if err != nil {
		t.setErr(err.Error())
		return err
	}

	// Kill any stray cloudflared from a previous run/crash before starting a new
	// one. Without this, repeated open/close piles up live tunnels — Cloudflare
	// then throttles/deregisters them and the URL stops routing (the flaky 530s).
	killStrayCloudflared()

	cmd := exec.CommandContext(ctx, bin, "tunnel", "--no-autoupdate", "--url", "http://127.0.0.1:"+port)
	// cloudflared writes to both stdout and stderr; BOTH must be drained or the
	// process blocks on a full pipe and the tunnel never comes up. Watch both for
	// the URL to be safe (cloudflared has printed it to either across versions).
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.setErr(err.Error())
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.setErr(err.Error())
		return err
	}
	if err := cmd.Start(); err != nil {
		t.setErr(err.Error())
		return err
	}

	t.mu.Lock()
	t.cmd, t.running = cmd, true
	t.mu.Unlock()

	scan := func(r io.Reader) {
		sc := bufio.NewScanner(r)
		// cloudflared prints a big ASCII-QR box with very long lines; the default
		// 64KB scanner token limit could choke on those, stall the reader, fill the
		// pipe and hang cloudflared. Give it room.
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			if m := reTryCloudflare.FindString(sc.Text()); m != "" {
				// Trust the URL the moment cloudflared prints it (like aigent-manager):
				// show the QR/link immediately instead of gating on a readiness probe.
				// The edge takes a few more seconds to route, but polling /api/info from
				// here was the source of the endless "verifying" hangs — trycloudflare
				// blackholes during propagation, so the probe stalled. Just surface it.
				t.mu.Lock()
				t.url, t.ready = m, true
				t.mu.Unlock()
			}
		}
	}
	go scan(stderr)
	go scan(stdout)
	go func() {
		_ = cmd.Wait()
		t.mu.Lock()
		t.running, t.cmd = false, nil
		t.mu.Unlock()
	}()
	return nil
}

// killStrayCloudflared terminates any lingering cloudflared processes so a fresh
// tunnel never competes with orphans from earlier launches (which is what makes
// trycloudflare start refusing to route). Best-effort and quiet.
func killStrayCloudflared() {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("taskkill", "/F", "/IM", "cloudflared.exe", "/T")
	} else {
		cmd = exec.Command("pkill", "-f", "cloudflared")
	}
	_ = cmd.Run()
}

func (t *tunnel) stop() {
	t.mu.Lock()
	cmd := t.cmd
	t.running, t.url, t.ready, t.cmd = false, "", false, nil
	t.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

func (t *tunnel) setErr(s string) {
	t.mu.Lock()
	t.err, t.running = s, false
	t.mu.Unlock()
}

// cloudflaredPath finds cloudflared on PATH, in binDir, or downloads it into
// binDir for the current OS/arch. The download is the official static binary.
func cloudflaredPath(binDir string) (string, error) {
	name := "cloudflared"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	local := filepath.Join(binDir, name)
	for _, dir := range []string{binDir, binaryDir()} {
		if p := filepath.Join(dir, name); fileExists(p) {
			return p, nil
		}
	}
	if p, err := exec.LookPath("cloudflared"); err == nil {
		return p, nil
	}
	url, err := cloudflaredURL()
	if err != nil {
		return "", err
	}
	if err := downloadFile(url, local); err != nil {
		return "", fmt.Errorf("no se pudo descargar cloudflared: %w", err)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(local, 0o755)
	}
	return local, nil
}

// cloudflaredURL returns the official static-binary download for this platform.
func cloudflaredURL() (string, error) {
	const base = "https://github.com/cloudflare/cloudflared/releases/latest/download/"
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "windows/amd64":
		return base + "cloudflared-windows-amd64.exe", nil
	case "windows/386":
		return base + "cloudflared-windows-386.exe", nil
	case "darwin/amd64", "darwin/arm64":
		// macOS ships a .tgz; unsupported here — ask the user to install it.
		return "", fmt.Errorf("en macOS instala cloudflared con: brew install cloudflared")
	case "linux/amd64":
		return base + "cloudflared-linux-amd64", nil
	case "linux/arm64":
		return base + "cloudflared-linux-arm64", nil
	case "linux/arm":
		return base + "cloudflared-linux-arm", nil
	default:
		return "", fmt.Errorf("plataforma no soportada para descarga automática: %s/%s", runtime.GOOS, runtime.GOARCH)
	}
}

func downloadFile(url, dest string) error {
	req, _ := http.NewRequestWithContext(context.Background(), "GET", url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("descarga devolvió %d", resp.StatusCode)
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return os.Rename(tmp, dest)
}
