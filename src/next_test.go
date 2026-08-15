package main

import (
	"path/filepath"
	"testing"
)

func TestNextInAlbum(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "gobby.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const key = "lib"
	eps := []Item{
		{Kind: "video", RelPath: "s/e1.mkv", Album: "Show", Season: 1, Episode: 1, Size: 1},
		{Kind: "video", RelPath: "s/e2.mkv", Album: "Show", Season: 1, Episode: 2, Size: 2},
		{Kind: "video", RelPath: "s/e3.mkv", Album: "Show", Season: 2, Episode: 1, Size: 3},
		{Kind: "video", RelPath: "other.mkv", Album: "", Size: 4},
	}
	for _, e := range eps {
		if err := upsertScanned(db, key, 1, e, nil); err != nil {
			t.Fatal(err)
		}
	}
	ids, _ := listItems(db, key, "video", "")
	byPath := map[string]int64{}
	for _, it := range ids {
		byPath[it.RelPath] = it.ID
	}

	if n, ok := nextInAlbum(db, key, byPath["s/e1.mkv"]); !ok || n.RelPath != "s/e2.mkv" {
		t.Errorf("after e1 got %q ok=%v, want s/e2.mkv", n.RelPath, ok)
	}
	if n, ok := nextInAlbum(db, key, byPath["s/e2.mkv"]); !ok || n.RelPath != "s/e3.mkv" {
		t.Errorf("after e2 got %q ok=%v, want s/e3.mkv", n.RelPath, ok)
	}

	if _, ok := nextInAlbum(db, key, byPath["s/e3.mkv"]); ok {
		t.Error("after last episode expected no next")
	}

	if _, ok := nextInAlbum(db, key, byPath["other.mkv"]); ok {
		t.Error("standalone should have no next")
	}
}
