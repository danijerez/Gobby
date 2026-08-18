package main

import (
	"path/filepath"
	"testing"
)

func TestCoverFallbackToSeries(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.Exec(`INSERT INTO items (id, library_key, kind, album, section, section_source, rel_path, title, cover, scan_state, enrich_state)
		VALUES (1,'k','video','Show','series','manual','a.mkv','E01', X'FFD8FFAA','present','not_found')`)
	db.Exec(`INSERT INTO items (id, library_key, kind, album, section, section_source, rel_path, title, scan_state, enrich_state)
		VALUES (2,'k','video','Show','series','manual','b.mkv','E02','present','not_found')`)

	b, err := coverOf(db, "k", 2)
	if err != nil {
		t.Fatalf("coverOf ep2: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("ep2 got no cover; fallback to series cover failed")
	}
	if b[0] != 0xFF {
		t.Fatalf("unexpected cover bytes: %x", b)
	}
}
