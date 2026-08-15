package main

import (
	"database/sql"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"time"
)

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
				return err
			}
			return nil
		}
		if d.IsDir() {
			if skipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		kind := kindFor(filepath.Ext(path))
		if kind == "" {
			return nil
		}

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
			return nil
		} else if !ok {

			if moved, e := rematchMoved(db, libraryKey, rel, info.Size(), info.ModTime().Unix(), runID); e == nil && moved {
				return nil
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

func isArtworkSidecar(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp", ".bmp", ".gif", ".tiff":
	default:
		return false
	}
	name := strings.ToLower(stripExt(filepath.Base(path)))
	for _, art := range []string{"cover", "folder", "poster", "fanart", "backdrop", "banner", "thumb", "back", "disc", "cdart", "clearart", "logo", "season"} {
		if name == art || strings.HasPrefix(name, art+"-") || strings.HasPrefix(name, art+"_") || strings.Contains(name, "-"+art) {
			return true
		}
	}
	return false
}

func skipDir(name string) bool {
	switch name {
	case "$RECYCLE.BIN", "System Volume Information",
		"node_modules", ".git", "__pycache__", ".cache", "AppData", ".svn", ".hg":
		return true
	}
	return false
}
