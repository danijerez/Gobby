package main

import (
	"database/sql"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"time"
)

// scan walks root and upserts media files into the DB. It is incremental: a
// file whose modtime matches the stored one is skipped without re-reading tags.
// relPaths are stored relative to root so the DB stays portable across drive
// letters (E: -> F:) when the external disk moves between machines.
func scan(db *sql.DB, root, libraryKey string) (added int, err error) {
	runID := time.Now().UnixNano()
	scanProgress.mu.Lock()
	scanProgress.Running, scanProgress.Phase = true, "scan"
	scanProgress.Total, scanProgress.Done, scanProgress.Found = 0, 0, 0
	scanProgress.mu.Unlock()
	defer func() {
		scanProgress.mu.Lock()
		scanProgress.Running = false
		scanProgress.mu.Unlock()
	}()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err // never mark the catalogue missing after a failed root walk
			}
			return nil // skip unreadable entries, keep going
		}
		if d.IsDir() {
			if skipDir(d.Name()) {
				return filepath.SkipDir // don't descend into system/non-media dirs
			}
			return nil
		}
		kind := kindFor(filepath.Ext(path))
		if kind == "" {
			return nil
		}
		// Skip sidecar artwork (cover.jpg, folder.png, poster.jpg…): these are
		// covers for other media, not files you want listed in the Files tab.
		if kind == "file" && isArtworkSidecar(path) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		if mt, ok := modTimeOf(db, libraryKey, rel); ok && mt == info.ModTime().Unix() {
			_ = markSeen(db, libraryKey, rel, runID)
			return nil // unchanged, skip
		} else if !ok {
			// Unknown path: might be a file that moved/renamed since last scan.
			// Adopt the matching missing row (keeps your edits) instead of dup'ing.
			if moved, e := rematchMoved(db, libraryKey, rel, info.Size(), info.ModTime().Unix(), runID); e == nil && moved {
				return nil // rematchMoved already stamped this run's last_seen_scan
			}
		}

		it, cover := extractMeta(path, kind)
		it.RelPath = rel
		it.Size = info.Size()
		it.ModTime = info.ModTime().Unix()
		if err := upsertScanned(db, libraryKey, runID, it, cover); err != nil {
			slog.Warn("upsert failed", "path", rel, "err", err)
			return nil
		}
		added++
		scanProgress.mu.Lock()
		scanProgress.Done, scanProgress.Found = scanProgress.Done+1, added
		scanProgress.mu.Unlock()
		return nil
	})
	if err == nil {
		if err = markMissing(db, libraryKey, runID); err != nil {
			return added, err
		}
	}
	return added, err
}

// isArtworkSidecar reports whether an image file is really cover art for other
// media (cover.jpg, folder.png, poster.jpg, season01-poster.jpg…) rather than a
// standalone image worth cataloguing.
func isArtworkSidecar(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp", ".bmp", ".gif", ".tiff":
	default:
		return false // only images can be sidecar artwork
	}
	name := strings.ToLower(stripExt(filepath.Base(path)))
	for _, art := range []string{"cover", "folder", "poster", "fanart", "backdrop", "banner", "thumb", "back", "disc", "cdart", "clearart", "logo", "season"} {
		if name == art || strings.HasPrefix(name, art+"-") || strings.HasPrefix(name, art+"_") || strings.Contains(name, "-"+art) {
			return true
		}
	}
	return false
}

// skipDir avoids descending into OS/system folders and noisy technical dirs
// (dependency caches, VCS metadata) that would flood the Files tab with junk.
// User content — roms, games, programs, logs, media — is indexed.
func skipDir(name string) bool {
	switch name {
	case "$RECYCLE.BIN", "System Volume Information", // Windows system
		"node_modules", ".git", "__pycache__", ".cache", "AppData", ".svn", ".hg": // technical noise
		return true
	}
	return false
}
