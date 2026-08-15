package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
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

type tunnel struct {
	mu      sync.Mutex
	cmd     *exec.Cmd
	url     string
	running bool
	ready   bool
	err     string
	token   string
}

func (t *tunnel) Token() string { t.mu.Lock(); defer t.mu.Unlock(); return t.token }

var reTryCloudflare = regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)

func (t *tunnel) status() map[string]any {
	t.mu.Lock()
	defer t.mu.Unlock()
	return map[string]any{"running": t.running, "url": t.url, "ready": t.ready, "error": t.err}
}

func (t *tunnel) start(ctx context.Context, binDir, port string) error {
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return nil
	}
	tok := make([]byte, 16)
	_, _ = rand.Read(tok)
	t.err, t.url, t.ready, t.token = "", "", false, hex.EncodeToString(tok)
	t.mu.Unlock()

	bin, err := cloudflaredPath(binDir)
	if err != nil {
		t.setErr(err.Error())
		return err
	}

	killStrayCloudflared()

	cmd := exec.CommandContext(ctx, bin, "tunnel", "--no-autoupdate", "--url", "http://127.0.0.1:"+port)

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

		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			if m := reTryCloudflare.FindString(sc.Text()); m != "" {

				t.mu.Lock()
				t.url, t.ready = m+"?k="+t.token, true
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
	t.running, t.url, t.ready, t.cmd, t.token = false, "", false, nil, ""
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

func cloudflaredURL() (string, error) {
	const base = "https://github.com/cloudflare/cloudflared/releases/latest/download/"
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "windows/amd64":
		return base + "cloudflared-windows-amd64.exe", nil
	case "windows/386":
		return base + "cloudflared-windows-386.exe", nil
	case "darwin/amd64", "darwin/arm64":

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
