package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const ghRepo = "danijerez/Gobby"

func assetSuffix() string {
	s := runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "darwin" {
		s = "macos-" + runtime.GOARCH
	}
	if runtime.GOOS == "windows" {
		s += ".exe"
	}
	return s
}

type latestRelease struct {
	Tag    string `json:"tag_name"`
	Assets []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func fetchLatestRelease(ctx context.Context) (*latestRelease, error) {
	url := "https://api.github.com/repos/" + ghRepo + "/releases/latest"
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("aún no hay releases publicadas")
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub respondió %d", resp.StatusCode)
	}
	var rel latestRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

func newerThan(latest, current string) bool {
	l, c := parseVer(latest), parseVer(current)
	if l == nil || c == nil {
		return strings.TrimPrefix(latest, "v") != strings.TrimPrefix(current, "v")
	}
	for i := 0; i < len(l) && i < len(c); i++ {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return len(l) > len(c)
}

func parseVer(s string) []int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n := 0
		for _, r := range p {
			if r < '0' || r > '9' {
				return nil
			}
			n = n*10 + int(r-'0')
		}
		out = append(out, n)
	}
	return out
}

func (rel *latestRelease) findAsset(prefix string) string {
	suffix := assetSuffix()
	for _, a := range rel.Assets {
		if strings.HasPrefix(a.Name, prefix) && strings.HasSuffix(a.Name, suffix) {
			return a.URL
		}
	}
	return ""
}

func replaceInPlace(ctx context.Context, url, dest string) error {
	newPath := dest + ".new"
	if err := downloadFile(url, newPath); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(newPath, 0o755)
	}
	if fileExists(dest) {
		oldPath := dest + ".old"
		_ = os.Remove(oldPath)
		if err := os.Rename(dest, oldPath); err != nil {
			_ = os.Remove(newPath)
			return fmt.Errorf("no se pudo apartar el binario actual: %w", err)
		}
	}
	if err := os.Rename(newPath, dest); err != nil {
		return fmt.Errorf("no se pudo instalar el nuevo binario: %w", err)
	}
	return nil
}

type updateStatus struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
}

func checkForUpdate(ctx context.Context, current string) updateStatus {
	rel, err := fetchLatestRelease(ctx)
	if err != nil {
		return updateStatus{Current: current, Error: err.Error()}
	}
	return updateStatus{
		Current:   current,
		Latest:    rel.Tag,
		Available: newerThan(rel.Tag, current),
	}
}

func applyUpdate(ctx context.Context, current string) error {
	rel, err := fetchLatestRelease(ctx)
	if err != nil {
		return err
	}
	if !newerThan(rel.Tag, current) {
		return fmt.Errorf("ya estás en la última versión (%s)", current)
	}
	dir := binaryDir()

	gobbyURL := rel.findAsset("gobby-")
	if gobbyURL == "" {
		return fmt.Errorf("la release %s no trae binario para %s", rel.Tag, assetSuffix())
	}
	self, err := os.Executable()
	if err != nil {
		self = filepath.Join(dir, "gobby")
		if runtime.GOOS == "windows" {
			self += ".exe"
		}
	}
	if err := replaceInPlace(ctx, gobbyURL, self); err != nil {
		return err
	}

	name := "ffmpeg"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	ffmpegDest := filepath.Join(dir, name)
	if ffURL := rel.findAsset("ffmpeg-"); ffURL != "" && fileExists(ffmpegDest) {
		if err := replaceInPlace(ctx, ffURL, ffmpegDest); err != nil {
			return fmt.Errorf("gobby actualizado, pero ffmpeg falló: %w", err)
		}
	}
	return nil
}

const updateTimeout = 60 * time.Second

func cleanupOldBinary() {
	self, err := os.Executable()
	if err != nil {
		return
	}
	_ = os.Remove(self + ".old")
}

func relaunchSelf() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self, os.Args[1:]...)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	cmd.Dir, _ = os.Getwd()
	return cmd.Start()
}
