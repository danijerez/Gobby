package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestScanKeepsLibrariesSeparateAndMarksMissing(t *testing.T) {
	tmp := t.TempDir()
	db, err := openDB(filepath.Join(tmp, "gobby.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rootA := filepath.Join(tmp, "library-a")
	rootB := filepath.Join(tmp, "library-b")
	for _, root := range []string{rootA, rootB} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "same-name.mkv"), []byte("not a real video"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := scan(db, rootA, "library-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := scan(db, rootB, "library-b"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"library-a", "library-b"} {
		items, err := listItems(db, key, "video", "")
		if err != nil || len(items) != 1 {
			t.Fatalf("%s: got %d visible items, err=%v", key, len(items), err)
		}
	}

	if err := os.Remove(filepath.Join(rootA, "same-name.mkv")); err != nil {
		t.Fatal(err)
	}
	if _, err := scan(db, rootA, "library-a"); err != nil {
		t.Fatal(err)
	}
	items, err := listItems(db, "library-a", "video", "")
	if err != nil || len(items) != 0 {
		t.Fatalf("removed item remains visible: got %d, err=%v", len(items), err)
	}
	items, err = listItems(db, "library-b", "video", "")
	if err != nil || len(items) != 1 {
		t.Fatalf("other library changed: got %d, err=%v", len(items), err)
	}
}

func TestScanRematchesMovedFile(t *testing.T) {
	tmp := t.TempDir()
	db, err := openDB(filepath.Join(tmp, "gobby.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	root := filepath.Join(tmp, "lib")
	if err := os.MkdirAll(filepath.Join(root, "old"), 0o755); err != nil {
		t.Fatal(err)
	}
	orig := filepath.Join(root, "old", "movie.mkv")
	if err := os.WriteFile(orig, []byte("some bytes here"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := scan(db, root, "lib"); err != nil {
		t.Fatal(err)
	}
	items, _ := listItems(db, "lib", "video", "")
	if len(items) != 1 {
		t.Fatalf("expected 1 item after first scan, got %d", len(items))
	}
	id := items[0].ID
	if err := updateItem(db, "lib", id, ItemEdit{Title: "My Edited Title", Notes: "my note", Rating: 5}); err != nil {
		t.Fatal(err)
	}

	fi, _ := os.Stat(orig)
	dst := filepath.Join(root, "movie.mkv")
	if err := os.Rename(orig, dst); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(dst, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}

	if _, err := scan(db, root, "lib"); err != nil {
		t.Fatal(err)
	}
	items, _ = listItems(db, "lib", "video", "")
	if len(items) != 1 {
		t.Fatalf("moved file should not duplicate: got %d items", len(items))
	}
	if items[0].ID != id {
		t.Fatalf("row identity lost on move: id %d -> %d", id, items[0].ID)
	}
	if items[0].Title != "My Edited Title" || items[0].Rating != 5 {
		t.Fatalf("edits lost on move: %+v", items[0])
	}
	if items[0].RelPath != "movie.mkv" {
		t.Fatalf("rel_path not updated to new location: %q", items[0].RelPath)
	}
}

func TestSkipDir(t *testing.T) {
	skip := []string{"$RECYCLE.BIN", "System Volume Information", "node_modules", ".git", "__pycache__", "AppData"}
	keep := []string{"roms", "switch", "programs", "logs", "Movies", "Music", "Games"}
	for _, n := range skip {
		if !skipDir(n) {
			t.Errorf("expected to skip %q", n)
		}
	}
	for _, n := range keep {
		if skipDir(n) {
			t.Errorf("expected to index %q, was skipped", n)
		}
	}
}

func TestOpenDBMigratesLegacyRelativePathConstraint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = legacy.Exec(`CREATE TABLE items (
        id INTEGER PRIMARY KEY, kind TEXT NOT NULL, rel_path TEXT NOT NULL UNIQUE,
        size INTEGER, modtime INTEGER, title TEXT, artist TEXT, album TEXT,
        year INTEGER, genre TEXT, duration INTEGER, season INTEGER DEFAULT 0,
        episode INTEGER DEFAULT 0, cover BLOB, meta TEXT, imdb_id TEXT,
        notes TEXT, rating INTEGER DEFAULT 0, updated_at INTEGER
    ); INSERT INTO items (kind, rel_path, title) VALUES ('video', 'film.mkv', 'Film')`)
	if err != nil {
		t.Fatal(err)
	}
	legacy.Close()

	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	items, err := listItems(db, "legacy", "video", "")
	if err != nil || len(items) != 1 || items[0].Title != "Film" {
		t.Fatalf("legacy data was not preserved: %#v, err=%v", items, err)
	}
	if err := upsertScanned(db, "other-library", 1, Item{Kind: "video", RelPath: "film.mkv", Title: "Other"}, nil); err != nil {
		t.Fatalf("same relative path should work in another library: %v", err)
	}
}
