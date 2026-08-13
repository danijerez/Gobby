package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Gobby updates itself from its own GitHub Releases: the release page is the single
// source of truth for both the binary and the slim ffmpeg. No package manager, no
// installer — download the asset for this OS/arch and overwrite the file in place.
const ghRepo = "danijerez/Gobby"

// assetSuffix is the per-platform tail of a release asset name. Binaries are
// published as gobby-<suffix> and ffmpeg-<suffix> (see publish.sh / releases).
func assetSuffix() string {
	s := runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "darwin" {
		s = "macos-" + runtime.GOARCH // release assets say "macos", not "darwin"
	}
	if runtime.GOOS == "windows" {
		s += ".exe"
	}
	return s
}

// latestRelease is the sliver of the GitHub Releases API we actually read.
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

// newerThan reports whether release tag `latest` is ahead of the running version.
// Tags are compared as dotted integers ("v0.2.0" → 0,2,0), tolerating a leading
// "v". Anything unparseable falls back to a plain string inequality — good enough
// to nudge "you differ from latest" without a full semver library.
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
				return nil // not a clean numeric tag
			}
			n = n*10 + int(r-'0')
		}
		out = append(out, n)
	}
	return out
}

// findAsset returns the download URL of the release asset whose name starts with
// prefix and ends with this platform's suffix ("" if the release lacks it).
func (rel *latestRelease) findAsset(prefix string) string {
	suffix := assetSuffix()
	for _, a := range rel.Assets {
		if strings.HasPrefix(a.Name, prefix) && strings.HasSuffix(a.Name, suffix) {
			return a.URL
		}
	}
	return ""
}

// replaceInPlace overwrites dest with the file at url. A running executable can't
// be deleted on Windows (and shouldn't be replaced mid-write on any OS), so we
// download to dest.new, rename the live file to dest.old, then move the new one in.
// The .old is best-effort removed; on Windows it lingers (the process holds it)
// until the next start, which is harmless.
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
		_ = os.Remove(oldPath) // clear a leftover from a prior update
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

// updateStatus is what /api/update/check returns: whether a newer release exists
// and what it's called.
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

// applyUpdate downloads the latest gobby binary (and ffmpeg if the release ships
// one for this platform) and overwrites them next to the running exe. It does NOT
// restart — the caller tells the user to relaunch, since the old process keeps
// running the old code until it exits.
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

	// ffmpeg is optional in a release — only replace it if both an asset exists and
	// the user already has one next to the binary (don't force it on people who use
	// system ffmpeg or don't stream mkv/avi).
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

// updateTimeout bounds a check/apply so a slow or unreachable GitHub can't hang
// the request forever.
const updateTimeout = 60 * time.Second
