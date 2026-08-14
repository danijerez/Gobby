package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Item is one media file plus your edits. Technical fields come from the scan;
// title/notes/rating are yours and are never overwritten by a re-scan.
type Item struct {
	ID            int64  `json:"id"`
	Section       string `json:"section"`        // movie | series | music | book | files
	SectionSource string `json:"section_source"` // auto | manual
	Kind          string `json:"kind"`           // video | audio | book
	RelPath       string `json:"rel_path"`
	Size          int64  `json:"size"`
	ModTime       int64  `json:"modtime"`
	Title         string `json:"title"`
	Artist        string `json:"artist"` // artist / director / author
	Album         string `json:"album"`  // album / collection / series
	Year          int    `json:"year"`
	Genre         string `json:"genre"`
	Duration      int    `json:"duration"` // seconds
	Season        int    `json:"season"`   // series only (0 = n/a)
	Episode       int    `json:"episode"`  // series only (0 = n/a)
	HasCover      bool   `json:"has_cover"`
	CoverID       int64  `json:"cover_id,omitempty"` // sibling id holding the artwork (Home shelves)
	Notes         string `json:"notes"`
	Rating        int    `json:"rating"`
	ImdbID        string `json:"imdb_id"`        // external id used to fetch meta (editable)
	Meta          *Meta  `json:"meta,omitempty"` // rich data from Cinemeta (nil until enriched)
	State         string `json:"state"`          // present | missing
	EnrichState   string `json:"enrich_state"`   // pending | found | not_found
}

// Meta holds a media item's rich metadata, stored as JSON in items.meta. Remote
// fields come from Cinemeta; Tech is recovered locally from the filename and
// must survive a remote refresh, so setMeta preserves it.
type Meta struct {
	Description string    `json:"description,omitempty"`
	Cast        []string  `json:"cast,omitempty"`
	Director    []string  `json:"director,omitempty"`
	Runtime     string    `json:"runtime,omitempty"`
	Genres      []string  `json:"genres,omitempty"`
	ImdbRating  string    `json:"imdb_rating,omitempty"`
	Year        string    `json:"year,omitempty"` // release year/range from the remote source
	Name        string    `json:"-"`              // official title (used to correct the item title on manual identify; not persisted in meta)
	Tech        *TechInfo `json:"tech,omitempty"` // local: resolution/codec/audio/langs
	Source      string    `json:"source,omitempty"` // where remote fields came from: cinemeta/openlibrary/itunes
}

const schema = `
CREATE TABLE IF NOT EXISTS items (
  id         INTEGER PRIMARY KEY,
  library_key TEXT NOT NULL DEFAULT 'legacy',
  kind       TEXT NOT NULL,
  rel_path   TEXT NOT NULL,
  size       INTEGER,
  modtime    INTEGER,
  title      TEXT,
  artist     TEXT,
  album      TEXT,
  year       INTEGER,
  genre      TEXT,
  duration   INTEGER,
  season     INTEGER DEFAULT 0,
  episode    INTEGER DEFAULT 0,
  cover      BLOB,
  meta       TEXT,
  imdb_id    TEXT,
  notes      TEXT,
  rating     INTEGER DEFAULT 0,
  section    TEXT NOT NULL DEFAULT 'files',
  section_source TEXT NOT NULL DEFAULT 'auto',
  section_reason TEXT,
  scan_state TEXT NOT NULL DEFAULT 'present',
  last_seen_scan INTEGER DEFAULT 0,
  enrich_state TEXT NOT NULL DEFAULT 'pending',
  added_at   INTEGER DEFAULT 0,
  last_opened INTEGER DEFAULT 0,
  updated_at INTEGER,
  UNIQUE(library_key, rel_path)
);

CREATE TABLE IF NOT EXISTS watchlist (
  id         INTEGER PRIMARY KEY,
  kind       TEXT,
  title      TEXT NOT NULL,
  note       TEXT,
  poster     TEXT,
  year       TEXT,
  done       INTEGER DEFAULT 0,
  created_at INTEGER
);
`

const itemIndexes = `
CREATE INDEX IF NOT EXISTS idx_items_library_kind ON items(library_key, kind);
CREATE INDEX IF NOT EXISTS idx_items_library_section ON items(library_key, section, scan_state);
`

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// WAL + a busy timeout let reads (browsing) and writes (scan/edits) coexist
	// without "database is locked" errors when many devices hit the server.
	db.Exec(`PRAGMA journal_mode=WAL`)
	db.Exec(`PRAGMA busy_timeout=5000`)
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	// Soft migrations: add columns to DBs created before they existed.
	db.Exec(`ALTER TABLE items ADD COLUMN meta TEXT`)
	db.Exec(`ALTER TABLE items ADD COLUMN imdb_id TEXT`)
	db.Exec(`ALTER TABLE items ADD COLUMN library_key TEXT NOT NULL DEFAULT 'legacy'`)
	db.Exec(`ALTER TABLE items ADD COLUMN section TEXT NOT NULL DEFAULT 'files'`)
	db.Exec(`ALTER TABLE items ADD COLUMN section_source TEXT NOT NULL DEFAULT 'auto'`)
	db.Exec(`ALTER TABLE items ADD COLUMN section_reason TEXT`)
	db.Exec(`ALTER TABLE items ADD COLUMN scan_state TEXT NOT NULL DEFAULT 'present'`)
	db.Exec(`ALTER TABLE items ADD COLUMN last_seen_scan INTEGER DEFAULT 0`)
	db.Exec(`ALTER TABLE items ADD COLUMN enrich_state TEXT NOT NULL DEFAULT 'pending'`)
	db.Exec(`ALTER TABLE watchlist ADD COLUMN poster TEXT`)
	db.Exec(`ALTER TABLE watchlist ADD COLUMN year TEXT`)
	db.Exec(`ALTER TABLE watchlist ADD COLUMN fields TEXT`) // JSON [{k,v}] custom fields
	db.Exec(`ALTER TABLE items ADD COLUMN added_at INTEGER DEFAULT 0`)
	db.Exec(`ALTER TABLE items ADD COLUMN last_opened INTEGER DEFAULT 0`)
	// Seed added_at for pre-existing rows so "recently added" isn't all-zero.
	db.Exec(`UPDATE items SET added_at=COALESCE(updated_at,0) WHERE added_at=0`)
	// Generic files never get a cover — stop them counting as pending (which made
	// startup re-run enrichment every time for work that can never complete).
	db.Exec(`UPDATE items SET enrich_state='not_found' WHERE kind='file' AND enrich_state='pending'`)
	// Purge sidecar artwork (cover.jpg / folder.png / poster.jpg…) that earlier
	// scans catalogued as real files — it clutters the Files tab. Fresh scans now
	// skip these (isArtworkSidecar); this cleans up databases built before that.
	db.Exec(`DELETE FROM items WHERE kind='file' AND (
	  LOWER(rel_path) LIKE '%/cover.%'  OR LOWER(rel_path) LIKE 'cover.%'  OR
	  LOWER(rel_path) LIKE '%/folder.%' OR LOWER(rel_path) LIKE 'folder.%' OR
	  LOWER(rel_path) LIKE '%poster.%'  OR LOWER(rel_path) LIKE '%fanart.%' OR
	  LOWER(rel_path) LIKE '%backdrop.%' OR LOWER(rel_path) LIKE '%banner.%')`)
	if err := migrateLibraries(db); err != nil {
		return nil, err
	}
	if _, err := db.Exec(itemIndexes); err != nil {
		return nil, err
	}
	// Existing catalogues predate sections. Seed a deterministic automatic one.
	_, _ = db.Exec(`UPDATE items SET section=CASE
		WHEN kind='audio' THEN 'music' WHEN kind='book' THEN 'book'
		WHEN kind='video' AND album<>'' THEN 'series'
		WHEN kind='video' THEN 'movie' ELSE 'files' END
		WHERE section='' OR section='files' AND section_source='auto'`)
	// Embedded/previously downloaded artwork means no automatic lookup is due.
	_, _ = db.Exec(`UPDATE items SET enrich_state='found' WHERE enrich_state='pending' AND cover IS NOT NULL`)
	return db, nil
}

// migrateLibraries rebuilds the original rel_path-only uniqueness constraint.
// A DB can now safely retain independent catalogues for several scan roots.
func migrateLibraries(db *sql.DB) error {
	var tableSQL string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='items'`).Scan(&tableSQL); err != nil {
		return err
	}
	// The first schema used UNIQUE(rel_path). Adding a composite index alone is
	// insufficient: that old constraint would still reject the same relative
	// path in a second library, so rebuild once without it.
	if strings.Contains(strings.ToUpper(tableSQL), "REL_PATH   TEXT NOT NULL UNIQUE") || strings.Contains(strings.ToUpper(tableSQL), "REL_PATH TEXT NOT NULL UNIQUE") {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		rollback := func(e error) error { _ = tx.Rollback(); return e }
		if _, err = tx.Exec(`DROP INDEX IF EXISTS idx_items_kind`); err != nil {
			return rollback(err)
		}
		if _, err = tx.Exec(`DROP INDEX IF EXISTS idx_items_library_path`); err != nil {
			return rollback(err)
		}
		if _, err = tx.Exec(`DROP INDEX IF EXISTS idx_items_library_kind`); err != nil {
			return rollback(err)
		}
		if _, err = tx.Exec(`DROP INDEX IF EXISTS idx_items_library_section`); err != nil {
			return rollback(err)
		}
		if _, err = tx.Exec(`ALTER TABLE items RENAME TO items_legacy`); err != nil {
			return rollback(err)
		}
		if _, err = tx.Exec(schema); err != nil {
			return rollback(err)
		}
		if _, err = tx.Exec(`INSERT INTO items (id, library_key, kind, rel_path, size, modtime, title, artist, album, year, genre, duration, season, episode, cover, meta, imdb_id, notes, rating, section, section_source, section_reason, scan_state, last_seen_scan, enrich_state, updated_at)
			SELECT id, library_key, kind, rel_path, size, modtime, title, artist, album, year, genre, duration, season, episode, cover, meta, imdb_id, notes, rating, section, section_source, section_reason, scan_state, last_seen_scan, 'pending', updated_at FROM items_legacy`); err != nil {
			return rollback(err)
		}
		if _, err = tx.Exec(`DROP TABLE items_legacy`); err != nil {
			return rollback(err)
		}
		return tx.Commit()
	}
	rows, err := db.Query(`PRAGMA table_info(items)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	hasLibrary := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull, pk int
		var def any
		if err := rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
			return err
		}
		if name == "library_key" {
			hasLibrary = true
		}
	}
	if hasLibrary {
		return rows.Err()
	}
	// Kept for databases made by a pre-library build. New databases already
	// contain the column because the schema above runs first.
	_, err = db.Exec(`ALTER TABLE items ADD COLUMN library_key TEXT NOT NULL DEFAULT 'legacy'`)
	return err
}

// upsertScanned inserts a new file or updates ONLY technical fields on an
// existing one, leaving your edits (title if you set it, notes, rating) intact.
// On insert, the scanned title seeds the editable title.
func upsertScanned(db *sql.DB, libraryKey string, runID int64, it Item, cover []byte) error {
	it.Section = automaticSection(it)
	enrichState := "pending"
	if len(cover) > 0 {
		enrichState = "found"
	} else if it.Kind == "file" {
		// generic files are never enriched (no identifiable cover). Seed them
		// as not_found so they don't count as pending and re-trigger enrichment
		// on every startup.
		enrichState = "not_found"
	}
	var metaJSON any // local tech metadata; NULL when none so COALESCE keeps remote meta
	if it.Meta != nil {
		if b, e := json.Marshal(it.Meta); e == nil {
			metaJSON = string(b)
		}
	}
	now := time.Now().Unix()
	_, err := db.Exec(`
		INSERT INTO items (library_key, kind, rel_path, size, modtime, title, artist, album, year, genre, duration, season, episode, cover, meta, section, section_source, section_reason, scan_state, last_seen_scan, enrich_state, added_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(library_key, rel_path) DO UPDATE SET
			size=excluded.size, modtime=excluded.modtime,
			artist=excluded.artist, album=excluded.album, year=excluded.year,
			genre=excluded.genre, duration=excluded.duration,
			season=excluded.season, episode=excluded.episode,
			section=CASE WHEN items.section_source='manual' THEN items.section ELSE excluded.section END,
			section_source=CASE WHEN items.section_source='manual' THEN items.section_source ELSE 'auto' END,
			section_reason=CASE WHEN items.section_source='manual' THEN items.section_reason ELSE 'extension/path/tags' END,
			scan_state='present', last_seen_scan=excluded.last_seen_scan,
			enrich_state=CASE WHEN excluded.enrich_state='found' THEN 'found' ELSE 'pending' END,
			cover=COALESCE(excluded.cover, items.cover),
			meta=COALESCE(items.meta, excluded.meta),
			updated_at=excluded.updated_at`,
		libraryKey, it.Kind, it.RelPath, it.Size, it.ModTime, it.Title, it.Artist, it.Album,
		it.Year, it.Genre, it.Duration, it.Season, it.Episode, cover, metaJSON, it.Section, "auto", "extension/path/tags", "present", runID, enrichState, now, now)
	return err
}

// modTimeOf returns the stored modtime for a path, or (0,false) if unknown.
func modTimeOf(db *sql.DB, libraryKey, relPath string) (int64, bool) {
	var mt int64
	err := db.QueryRow(`SELECT modtime FROM items WHERE library_key=? AND rel_path=?`, libraryKey, relPath).Scan(&mt)
	if err != nil {
		return 0, false
	}
	return mt, true
}

// rematchMoved re-identifies a file that changed path since the last scan. When
// a scan hits a rel_path not in the DB, a previously-catalogued file with the
// same (size, modtime) that went missing is almost certainly this same file,
// just moved/renamed — so we move the existing row to the new path instead of
// inserting a duplicate, preserving all your edits (title/notes/rating/cover/
// meta/imdb). Returns true if it adopted a missing row.
//
// ponytail: size+modtime signature, not a content hash. Cheap and good enough;
// two distinct files sharing byte-size AND modtime to the second is rare. If
// that ever bites, hash only on (size,modtime) collision as the tiebreak.
// It runs mid-walk, before markMissing, so the moved row is still 'present' from
// a prior scan: the tell is last_seen_scan<>runID (not yet seen this run) at a
// path the walk hasn't produced. Adopt that row for the new path.
func rematchMoved(db *sql.DB, libraryKey, newRel string, size, modtime, runID int64) (bool, error) {
	if size == 0 {
		return false, nil // empty files carry no distinguishing signature
	}
	res, err := db.Exec(`
		UPDATE items SET rel_path=?, scan_state='present', last_seen_scan=?
		WHERE id IN (
		  SELECT id FROM items
		  WHERE library_key=? AND size=? AND modtime=? AND last_seen_scan<>? AND rel_path<>?
		  LIMIT 1)`,
		newRel, runID, libraryKey, size, modtime, runID, newRel)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func markSeen(db *sql.DB, libraryKey, relPath string, runID int64) error {
	_, err := db.Exec(`UPDATE items SET scan_state='present', last_seen_scan=? WHERE library_key=? AND rel_path=?`, runID, libraryKey, relPath)
	return err
}

func markMissing(db *sql.DB, libraryKey string, runID int64) error {
	_, err := db.Exec(`UPDATE items SET scan_state='missing' WHERE library_key=? AND last_seen_scan<>?`, libraryKey, runID)
	return err
}

// facets returns the distinct genres and the year range present in a library, so
// the filter UI can offer real choices instead of free text. Genres stored as
// "Action, Comedy" are split and de-duped.
func facets(db *sql.DB, libraryKey string) (genres []string, yearMin, yearMax int) {
	seen := map[string]bool{}
	rows, err := db.Query(`SELECT DISTINCT genre FROM items WHERE library_key=? AND scan_state='present' AND genre<>''`, libraryKey)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var g string
			if rows.Scan(&g) != nil {
				continue
			}
			for _, part := range strings.Split(g, ",") {
				p := strings.TrimSpace(part)
				if p != "" && !seen[p] {
					seen[p] = true
					genres = append(genres, p)
				}
			}
		}
	}
	sort.Strings(genres)
	db.QueryRow(`SELECT COALESCE(MIN(year),0), COALESCE(MAX(year),0) FROM items WHERE library_key=? AND scan_state='present' AND year>0`, libraryKey).Scan(&yearMin, &yearMax)
	return genres, yearMin, yearMax
}

// LibraryInfo describes one indexed catalogue for the switcher UI.
type LibraryInfo struct {
	Key      string `json:"key"`
	Root     string `json:"root"`     // path used to resolve files (key if it's a path, else "")
	Total    int    `json:"total"`    // present items
	Reachable bool  `json:"reachable"` // the root exists on this machine right now
	Current  bool   `json:"current"`  // the one being viewed
}

// listLibraries returns every catalogue in the DB. A default library_key is the
// scan root's absolute path, so we treat a key that resolves to an existing dir
// as its own root; custom -library names carry no path (Root "", not reachable).
func listLibraries(db *sql.DB, currentKey string) ([]LibraryInfo, error) {
	rows, err := db.Query(`SELECT library_key, COUNT(*) FROM items WHERE scan_state='present' GROUP BY library_key ORDER BY library_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LibraryInfo
	for rows.Next() {
		var li LibraryInfo
		if err := rows.Scan(&li.Key, &li.Total); err != nil {
			return nil, err
		}
		if dirExists(li.Key) { // key is a path that still exists here
			li.Root = li.Key
			li.Reachable = true
		}
		li.Current = li.Key == currentKey
		out = append(out, li)
	}
	return out, rows.Err()
}

// itemColumns is every items column except the autoincrement id, used to copy
// rows between databases (export a library / merge an import) without clashing ids.
const itemColumns = `library_key, kind, rel_path, size, modtime, title, artist, album,
	year, genre, duration, season, episode, cover, meta, imdb_id, notes, rating,
	section, section_source, section_reason, scan_state, last_seen_scan, enrich_state,
	added_at, last_opened, updated_at`

// exportLibrary writes a standalone gobby.db to destPath containing only the given
// library's items (plus the whole watchlist — it isn't per-library). Uses ATTACH so
// it's a straight row copy, no re-encoding.
func exportLibrary(db *sql.DB, libraryKey, destPath string) error {
	_ = os.Remove(destPath)
	if _, err := db.Exec(`ATTACH DATABASE ? AS exp`, destPath); err != nil {
		return err
	}
	defer db.Exec(`DETACH DATABASE exp`)
	if _, err := db.Exec(`CREATE TABLE exp.items AS SELECT * FROM items WHERE 0`); err != nil {
		return err
	}
	if _, err := db.Exec(`CREATE TABLE exp.watchlist AS SELECT * FROM watchlist WHERE 0`); err != nil {
		return err
	}
	if _, err := db.Exec(`INSERT INTO exp.items SELECT * FROM items WHERE library_key=?`, libraryKey); err != nil {
		return err
	}
	_, err := db.Exec(`INSERT INTO exp.watchlist SELECT * FROM watchlist`)
	return err
}

// mergeImport pulls libraries from an uploaded gobby.db (at srcPath) into this one,
// adding their rows instead of replacing. A library_key already present here is
// skipped (kept as-is), so an import never clobbers a catalogue you already have.
// Returns the keys actually added. Watchlist rows are appended (delete-dupe by title).
func mergeImport(db *sql.DB, srcPath string) ([]string, error) {
	if _, err := db.Exec(`ATTACH DATABASE ? AS imp`, srcPath); err != nil {
		return nil, err
	}
	defer db.Exec(`DETACH DATABASE imp`)

	rows, err := db.Query(`SELECT DISTINCT library_key FROM imp.items
		WHERE library_key NOT IN (SELECT DISTINCT library_key FROM items)`)
	if err != nil {
		return nil, err
	}
	var add []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return nil, err
		}
		add = append(add, k)
	}
	rows.Close()
	for _, k := range add {
		if _, err := db.Exec(`INSERT INTO items (`+itemColumns+`) SELECT `+itemColumns+` FROM imp.items WHERE library_key=?`, k); err != nil {
			return nil, err
		}
	}
	// watchlist: add titles not already present
	_, _ = db.Exec(`INSERT INTO watchlist (kind, title, note, poster, year, done, created_at)
		SELECT kind, title, note, poster, year, done, created_at FROM imp.watchlist
		WHERE title NOT IN (SELECT title FROM watchlist)`)
	return add, nil
}

// rebindLibrary re-points an existing catalogue at a new root: it rewrites the
// library_key so the rows the user already indexed show up for the folder Gobby
// now lives in (the "I moved the whole Gobby folder" case). Only the key changes;
// every rel_path and edit is kept.
func rebindLibrary(db *sql.DB, oldKey, newKey string) error {
	if oldKey == newKey {
		return nil
	}
	_, err := db.Exec(`UPDATE items SET library_key=? WHERE library_key=?`, newKey, oldKey)
	return err
}

func pendingEnrichmentCount(db *sql.DB, libraryKey string) (int, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM items WHERE library_key=? AND scan_state='present' AND enrich_state='pending'`, libraryKey).Scan(&count)
	return count, err
}

// nonEmptySections returns the set of UI tabs that currently hold at least one
// present item, so the front-end can hide tabs with nothing in them. "audio"
// mirrors the stored "music" section (the tab is named differently).
func nonEmptySections(db *sql.DB, libraryKey string) map[string]bool {
	out := map[string]bool{}
	rows, err := db.Query(`SELECT DISTINCT section FROM items WHERE library_key=? AND scan_state='present'`, libraryKey)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		if rows.Scan(&s) == nil {
			out[s] = true
			if s == "music" {
				out["audio"] = true
			}
		}
	}
	return out
}

func automaticSection(it Item) string {
	switch it.Kind {
	case "audio":
		return "music"
	case "book":
		return "book"
	case "video":
		if it.Album != "" {
			return "series"
		}
		return "movie"
	default:
		return "files"
	}
}

// listItems returns items filtered by kind (empty = all) and a title/artist
// substring search (empty = no filter).
func listItems(db *sql.DB, libraryKey, kind, q string) ([]Item, error) {
	rows, err := db.Query(`
		SELECT id, kind, rel_path, COALESCE(size,0), COALESCE(modtime,0), section, section_source, scan_state, enrich_state,
		       COALESCE(title,''), COALESCE(artist,''), COALESCE(album,''),
		       COALESCE(year,0), COALESCE(genre,''), COALESCE(duration,0),
		       COALESCE(season,0), COALESCE(episode,0),
		       (cover IS NOT NULL), COALESCE(notes,''), COALESCE(rating,0)
		FROM items
		WHERE library_key=? AND scan_state='present' AND (?='' OR kind=?)
		  AND (?='' OR title LIKE '%'||?||'%' OR artist LIKE '%'||?||'%' OR album LIKE '%'||?||'%')
		ORDER BY album, season, episode, title`,
		libraryKey, kind, kind, q, q, q, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Item
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Kind, &it.RelPath, &it.Size, &it.ModTime, &it.Section, &it.SectionSource, &it.State, &it.EnrichState,
			&it.Title, &it.Artist, &it.Album, &it.Year, &it.Genre, &it.Duration,
			&it.Season, &it.Episode, &it.HasCover, &it.Notes, &it.Rating); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// markOpened records that a file was streamed, powering "continue" / "recent".
func markOpened(db *sql.DB, libraryKey string, id int64) error {
	_, err := db.Exec(`UPDATE items SET last_opened=? WHERE id=? AND library_key=?`, time.Now().Unix(), id, libraryKey)
	return err
}

// homeShelf runs a bounded query for a Home row (recently added / recently
// opened). `order` is a trusted, fixed ORDER BY clause — never user input.
// CoverID resolves the sibling that actually holds the artwork: for a series
// only one episode carries the cover, so an episode row points at it and Home
// can still show the poster.
func homeShelf(db *sql.DB, libraryKey, where, order string, limit int) ([]Item, error) {
	rows, err := db.Query(`
		SELECT i.id, i.kind, i.rel_path, COALESCE(i.size,0), COALESCE(i.modtime,0), i.section, i.section_source, i.scan_state, i.enrich_state,
		       COALESCE(i.title,''), COALESCE(i.artist,''), COALESCE(i.album,''),
		       COALESCE(i.year,0), COALESCE(i.genre,''), COALESCE(i.duration,0),
		       COALESCE(i.season,0), COALESCE(i.episode,0),
		       (i.cover IS NOT NULL), COALESCE(i.notes,''), COALESCE(i.rating,0),
		       COALESCE((
		         CASE WHEN i.cover IS NOT NULL THEN i.id
		         WHEN i.album<>'' THEN (SELECT s.id FROM items s
		           WHERE s.library_key=i.library_key AND s.album=i.album AND s.kind=i.kind
		             AND s.cover IS NOT NULL LIMIT 1)
		         END), 0)
		FROM items i
		WHERE i.library_key=? AND i.scan_state='present' AND `+where+`
		ORDER BY `+order+` LIMIT ?`, libraryKey, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		var coverID int64
		if err := rows.Scan(&it.ID, &it.Kind, &it.RelPath, &it.Size, &it.ModTime, &it.Section, &it.SectionSource, &it.State, &it.EnrichState,
			&it.Title, &it.Artist, &it.Album, &it.Year, &it.Genre, &it.Duration,
			&it.Season, &it.Episode, &it.HasCover, &it.Notes, &it.Rating, &coverID); err != nil {
			return nil, err
		}
		it.CoverID = coverID
		if coverID != 0 {
			it.HasCover = true
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// getItem returns one item with its rich meta decoded (nil if none).
func getItem(db *sql.DB, libraryKey string, id int64) (Item, error) {
	var it Item
	var metaJSON, imdbID sql.NullString
	err := db.QueryRow(`
		SELECT id, kind, rel_path, COALESCE(size,0), COALESCE(modtime,0), section, section_source, scan_state, enrich_state,
		       COALESCE(title,''), COALESCE(artist,''), COALESCE(album,''),
		       COALESCE(year,0), COALESCE(genre,''), COALESCE(duration,0),
		       COALESCE(season,0), COALESCE(episode,0),
		       (cover IS NOT NULL), COALESCE(notes,''), COALESCE(rating,0), meta, imdb_id
		FROM items WHERE id=? AND library_key=?`, id, libraryKey).Scan(
		&it.ID, &it.Kind, &it.RelPath, &it.Size, &it.ModTime, &it.Section, &it.SectionSource, &it.State, &it.EnrichState,
		&it.Title, &it.Artist, &it.Album, &it.Year, &it.Genre, &it.Duration,
		&it.Season, &it.Episode, &it.HasCover, &it.Notes, &it.Rating, &metaJSON, &imdbID)
	if err != nil {
		return it, err
	}
	it.ImdbID = imdbID.String
	if metaJSON.Valid && metaJSON.String != "" {
		var m Meta
		if json.Unmarshal([]byte(metaJSON.String), &m) == nil {
			it.Meta = &m
		}
	}
	return it, nil
}

// setMeta stores remote metadata, preserving the locally-parsed Tech block that
// a scan may already have written. It also backfills the year column from the
// remote year when the local parse left it empty (series episodes like "S01E01"
// carry no year, so the shelf/detail would otherwise show nothing) — filling the
// whole series so every episode agrees.
func setMeta(db *sql.DB, libraryKey string, id int64, m Meta, imdbID string) error {
	prev, prevErr := getItem(db, libraryKey, id)
	if prevErr == nil && prev.Meta != nil && prev.Meta.Tech != nil {
		m.Tech = prev.Meta.Tech
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if _, err = db.Exec(`UPDATE items SET meta=?, imdb_id=? WHERE id=? AND library_key=?`, string(b), imdbID, id, libraryKey); err != nil {
		return err
	}
	// A series' remote meta (synopsis/cast/genres) is the same for every episode,
	// but enrich only processes one episode per series — so the collection page,
	// which reads items[0], showed nothing when that wasn't the enriched one.
	// Copy the remote fields to the sibling episodes (preserving each one's own
	// Tech, recovered from the filename) so the series page always has them.
	if prevErr == nil && prev.Album != "" {
		propagateSeriesMeta(db, libraryKey, prev.Kind, prev.Album, m, imdbID, id)
	}
	if y := firstYear(m.Year); y > 0 && prevErr == nil {
		if prev.Album != "" {
			db.Exec(`UPDATE items SET year=? WHERE library_key=? AND kind=? AND album=? AND (year IS NULL OR year=0)`, y, libraryKey, prev.Kind, prev.Album)
		} else {
			db.Exec(`UPDATE items SET year=? WHERE id=? AND library_key=? AND (year IS NULL OR year=0)`, y, id, libraryKey)
		}
	}
	return nil
}

// propagateSeriesMeta writes the remote meta `m` onto every OTHER episode of the
// same series, keeping each sibling's own Tech block. imdbID is shared too so any
// episode can drive a re-fetch. Best-effort: a sibling that fails to update is
// skipped, not fatal.
func propagateSeriesMeta(db *sql.DB, libraryKey, kind, album string, m Meta, imdbID string, exceptID int64) {
	rows, err := db.Query(`SELECT id, meta FROM items WHERE library_key=? AND kind=? AND album=? AND id<>? AND scan_state='present'`,
		libraryKey, kind, album, exceptID)
	if err != nil {
		return
	}
	type sib struct {
		id   int64
		tech *TechInfo
	}
	var sibs []sib
	for rows.Next() {
		var sid int64
		var raw sql.NullString
		if rows.Scan(&sid, &raw) != nil {
			continue
		}
		var tech *TechInfo
		if raw.Valid && raw.String != "" {
			var old Meta
			if json.Unmarshal([]byte(raw.String), &old) == nil {
				tech = old.Tech
			}
		}
		sibs = append(sibs, sib{sid, tech})
	}
	rows.Close()
	for _, s := range sibs {
		mm := m
		mm.Tech = s.tech // keep this episode's own technical metadata
		if b, err := json.Marshal(mm); err == nil {
			db.Exec(`UPDATE items SET meta=?, imdb_id=? WHERE id=? AND library_key=?`, string(b), imdbID, s.id, libraryKey)
		}
	}
}

// firstYear pulls the leading 4-digit year out of "2008", "2008–2013", etc.
func firstYear(s string) int {
	digits := ""
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits += string(r)
			if len(digits) == 4 {
				break
			}
		} else if digits != "" {
			break
		}
	}
	if len(digits) == 4 {
		n := 0
		fmt.Sscanf(digits, "%d", &n)
		return n
	}
	return 0
}

// setMetaSource records which provider supplied the artwork/metadata, merged
// into the existing meta JSON so Tech and remote fields are preserved.
func setMetaSource(db *sql.DB, libraryKey string, id int64, source string) error {
	it, err := getItem(db, libraryKey, id)
	if err != nil {
		return err
	}
	m := Meta{}
	if it.Meta != nil {
		m = *it.Meta
	}
	m.Source = source
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE items SET meta=? WHERE id=? AND library_key=?`, string(b), id, libraryKey)
	return err
}

// setTitle overwrites the editable title (used when a manual IMDb identify
// brings the official name — the filename-derived title was wrong).
func setTitle(db *sql.DB, libraryKey string, id int64, title string) error {
	_, err := db.Exec(`UPDATE items SET title=?, updated_at=? WHERE id=? AND library_key=?`, title, time.Now().Unix(), id, libraryKey)
	return err
}

func setImdbID(db *sql.DB, libraryKey string, id int64, imdbID string) error {
	_, err := db.Exec(`UPDATE items SET imdb_id=? WHERE id=? AND library_key=?`, imdbID, id, libraryKey)
	return err
}

// Group is a folder-based collection (series / album / author).
type Group struct {
	Name  string `json:"name"`
	Items []Item `json:"items"`
}

// BrowseView splits a kind into folder groups + standalone (loose) items.
// Grouped = has a non-empty album (series name / album / author folder);
// everything else is loose (standalone movies, tagless singles).
type BrowseView struct {
	Groups     []Group `json:"groups"`
	Loose      []Item  `json:"loose"`
	LooseTotal int     `json:"loose_total"` // full loose count before paging
	Page       int     `json:"page"`
	PageSize   int     `json:"page_size"`
}

const browsePageSize = 60

// Filters narrows a browse result. Empty fields mean "no constraint".
type Filters struct {
	Ext       string // file extension without dot, e.g. "mkv"
	YearMin   int
	YearMax   int
	Cover     string // "with" | "without" | ""
	RatingMin int    // your personal rating 0-5
	Genre     string // substring, case-insensitive
}

func (f Filters) empty() bool {
	return f.Ext == "" && f.YearMin == 0 && f.YearMax == 0 && f.Cover == "" && f.RatingMin == 0 && f.Genre == ""
}

func (f Filters) match(it Item) bool {
	if f.Ext != "" && !strings.EqualFold(strings.TrimPrefix(filepathExt(it.RelPath), "."), f.Ext) {
		return false
	}
	if f.YearMin > 0 && it.Year < f.YearMin {
		return false
	}
	if f.YearMax > 0 && (it.Year == 0 || it.Year > f.YearMax) {
		return false
	}
	if f.Cover == "with" && !it.HasCover {
		return false
	}
	if f.Cover == "without" && it.HasCover {
		return false
	}
	if f.RatingMin > 0 && it.Rating < f.RatingMin {
		return false
	}
	if f.Genre != "" && !strings.Contains(strings.ToLower(it.Genre), strings.ToLower(f.Genre)) {
		return false
	}
	return true
}

// browseView groups items for a tab. Virtual kinds "series" and "movie" both map
// to DB kind "video": series = grouped episodes, movie = standalone (no album).
// Groups are returned whole (few); loose items are paginated (Files can be many).
func browseView(db *sql.DB, libraryKey, section, q string, f Filters, page int) (BrowseView, error) {
	dbKind := map[string]string{"series": "video", "movie": "video", "music": "audio", "audio": "audio", "book": "book"}[section]
	items, err := listItems(db, libraryKey, dbKind, q)
	if err != nil {
		return BrowseView{}, err
	}
	var bv BrowseView
	seen := map[string]int{}
	for _, it := range items {
		if !f.empty() && !f.match(it) {
			continue
		}
		// "files" intentionally has no technical kind filter: it is the safe
		// landing area for ambiguous or manually reclassified media.
		if it.Section != section && !(section == "audio" && it.Section == "music") {
			continue
		}
		// Files live in the Files tab's size tree, not in these browse groups.
		if it.Album == "" || section == "movie" || section == "files" {
			bv.Loose = append(bv.Loose, it)
			continue
		}
		i, ok := seen[it.Album]
		if !ok {
			bv.Groups = append(bv.Groups, Group{Name: it.Album})
			i = len(bv.Groups) - 1
			seen[it.Album] = i
		}
		bv.Groups[i].Items = append(bv.Groups[i].Items, it)
	}
	// paginate the loose list (groups stay whole — there are few of them)
	if page < 1 {
		page = 1
	}
	bv.LooseTotal = len(bv.Loose)
	bv.Page = page
	bv.PageSize = browsePageSize
	start := (page - 1) * browsePageSize
	if start >= len(bv.Loose) {
		bv.Loose = []Item{}
	} else {
		end := start + browsePageSize
		if end > len(bv.Loose) {
			end = len(bv.Loose)
		}
		bv.Loose = bv.Loose[start:end]
	}
	return bv, nil
}

// SearchResult is one hit in the global search: an item, plus which section it
// belongs to and (for series/albums) the collection name so the UI can route a
// click to the collection page instead of a lone episode.
type SearchResult struct {
	Item
	Group string `json:"group,omitempty"` // series/album name (empty = standalone)
}

// searchAll runs one query across EVERY kind (movies, series, music, books,
// files), so the header search works from anywhere. Series/albums collapse to a
// single hit (their collection), the rest come through as individual items.
// Capped so a broad query can't return the whole library.
func searchAll(db *sql.DB, libraryKey, q string, limit int) ([]SearchResult, error) {
	if strings.TrimSpace(q) == "" {
		return []SearchResult{}, nil
	}
	items, err := listItems(db, libraryKey, "", q)
	if err != nil {
		return nil, err
	}
	var out []SearchResult
	seenGroup := map[string]bool{} // section|album → already emitted the collection
	for _, it := range items {
		// collapse a series/album to one result (its collection)
		if it.Album != "" && it.Section != "movie" && it.Section != "files" {
			key := it.Section + "|" + it.Album
			if seenGroup[key] {
				continue
			}
			seenGroup[key] = true
			out = append(out, SearchResult{Item: it, Group: it.Album})
		} else {
			out = append(out, SearchResult{Item: it})
		}
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func filepathExt(p string) string {
	if i := strings.LastIndex(p, "."); i >= 0 {
		return p[i:]
	}
	return ""
}


// TreeNode is one node of the whole-library size tree: a folder (with children)
// or a leaf file. Size is the recursive total for folders, the file size for
// leaves. Built entirely from the catalogue — no disk walk.
type TreeNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"` // slash path from scan root
	Size     int64       `json:"size"`
	Count    int         `json:"count"` // files under here (leaves = 1)
	IsDir    bool        `json:"dir"`
	ID       int64       `json:"id,omitempty"`      // leaf item id (for opening)
	Kind     string      `json:"kind,omitempty"`    // leaf kind
	Section  string      `json:"section,omitempty"` // leaf section
	Children []*TreeNode `json:"children,omitempty"`
}

// fullTree builds the complete folder tree across ALL kinds (media + files)
// with recursive byte sizes, so the UI can render an expandable treeview and a
// size treemap. One query, assembled in memory; folders are sorted largest-first
// so the heavy stuff surfaces at the top.
func fullTree(db *sql.DB, libraryKey string) (*TreeNode, error) {
	rows, err := db.Query(`
		SELECT id, rel_path, COALESCE(size,0), COALESCE(title,''), kind, section
		FROM items WHERE library_key=? AND scan_state='present'`, libraryKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	root := &TreeNode{Name: "", Path: "", IsDir: true}
	for rows.Next() {
		var id, size int64
		var rel, title, kind, section string
		if err := rows.Scan(&id, &rel, &size, &title, &kind, &section); err != nil {
			return nil, err
		}
		parts := strings.Split(rel, "/")
		cur := root
		cur.Size += size
		cur.Count++
		for i, p := range parts {
			leaf := i == len(parts)-1
			var child *TreeNode
			for _, c := range cur.Children {
				if c.Name == p {
					child = c
					break
				}
			}
			if child == nil {
				child = &TreeNode{Name: p, Path: strings.Join(parts[:i+1], "/"), IsDir: !leaf}
				cur.Children = append(cur.Children, child)
			}
			child.Size += size
			child.Count++
			if leaf {
				child.IsDir = false
				child.ID, child.Kind, child.Section = id, kind, section
				if title != "" {
					child.Name = title // show the friendly title, not the raw filename
				}
			}
			cur = child
		}
	}
	sortTree(root)
	sectionOf(root) // fill each folder's dominant section (by bytes) for UI coloring
	return root, rows.Err()
}

// sectionOf sets each folder's Section to whichever section owns the most bytes
// beneath it, so the treeview can color a folder by the kind of content it holds
// (movies blue, music another hue, …). Leaves already carry their own section.
func sectionOf(n *TreeNode) map[string]int64 {
	if !n.IsDir {
		return map[string]int64{n.Section: n.Size}
	}
	tally := map[string]int64{}
	for _, c := range n.Children {
		for s, b := range sectionOf(c) {
			tally[s] += b
		}
	}
	var best string
	var max int64
	for s, b := range tally {
		if b > max {
			best, max = s, b
		}
	}
	n.Section = best
	return tally
}

// sortTree orders each folder's children largest-first (dirs and files mixed by
// size), matching how a disk-usage view reads: biggest consumers on top.
func sortTree(n *TreeNode) {
	sort.Slice(n.Children, func(i, j int) bool {
		return n.Children[i].Size > n.Children[j].Size
	})
	for _, c := range n.Children {
		if c.IsDir {
			sortTree(c)
		}
	}
}

func coverOf(db *sql.DB, libraryKey string, id int64) ([]byte, error) {
	var b []byte
	err := db.QueryRow(`SELECT cover FROM items WHERE id=? AND library_key=? AND scan_state='present'`, id, libraryKey).Scan(&b)
	return b, err
}

func setCover(db *sql.DB, libraryKey string, id int64, cover []byte) error {
	_, err := db.Exec(`UPDATE items SET cover=? WHERE id=? AND library_key=?`, cover, id, libraryKey)
	return err
}

func setEnrichState(db *sql.DB, libraryKey string, id int64, state string) error {
	_, err := db.Exec(`UPDATE items SET enrich_state=? WHERE id=? AND library_key=?`, state, id, libraryKey)
	return err
}

// Albums and series share a single remote identity in the current catalogue
// model. Completing the representative also settles its untouched siblings.
func setGroupEnrichState(db *sql.DB, libraryKey string, it Item, state string) error {
	if it.Album == "" {
		return setEnrichState(db, libraryKey, it.ID, state)
	}
	_, err := db.Exec(`UPDATE items SET enrich_state=? WHERE library_key=? AND kind=? AND album=? AND enrich_state='pending'`, state, libraryKey, it.Kind, it.Album)
	return err
}

// updateItem saves your editable fields. Beyond title/notes/rating it now also
// lets you correct the descriptive metadata (year/artist/album/genre) when the
// automatic parse got it wrong — for any kind, not just movies.
func updateItem(db *sql.DB, libraryKey string, id int64, m ItemEdit) error {
	_, err := db.Exec(`UPDATE items SET title=?, notes=?, rating=?, year=?, artist=?, album=?, genre=?, updated_at=? WHERE id=? AND library_key=?`,
		m.Title, m.Notes, m.Rating, m.Year, m.Artist, m.Album, m.Genre, time.Now().Unix(), id, libraryKey)
	return err
}

// ItemEdit is the set of user-editable descriptive fields.
type ItemEdit struct {
	Title  string `json:"title"`
	Notes  string `json:"notes"`
	Rating int    `json:"rating"`
	Year   int    `json:"year"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
	Genre  string `json:"genre"`
}

func setSection(db *sql.DB, libraryKey string, id int64, section string) error {
	if section == "auto" {
		it, err := getItem(db, libraryKey, id)
		if err != nil {
			return err
		}
		_, err = db.Exec(`UPDATE items SET section=?, section_source='auto', section_reason='extension/path/tags', updated_at=? WHERE id=? AND library_key=?`, automaticSection(it), time.Now().Unix(), id, libraryKey)
		return err
	}
	switch section {
	case "movie", "series", "music", "book", "files":
	default:
		return fmt.Errorf("invalid section %q", section)
	}
	_, err := db.Exec(`UPDATE items SET section=?, section_source='manual', section_reason='manual edit', updated_at=? WHERE id=? AND library_key=?`, section, time.Now().Unix(), id, libraryKey)
	return err
}

// Watch is one watch/listen/read-later entry. Poster/Year are populated when the
// entry was added from an online search; blank for free-text entries.
// WatchField is one user-defined key/value on a watchlist entry (e.g.
// "Platform": "Netflix"). Stored as a JSON array in the fields column.
type WatchField struct {
	K string `json:"k"`
	V string `json:"v"`
}

type Watch struct {
	ID     int64        `json:"id"`
	Kind   string       `json:"kind"`
	Title  string       `json:"title"`
	Note   string       `json:"note"`
	Poster string       `json:"poster"`
	Year   string       `json:"year"`
	Done   bool         `json:"done"`
	Fields []WatchField `json:"fields"`
}

func addWatch(db *sql.DB, kind, title, note, poster, year string) error {
	_, err := db.Exec(`INSERT INTO watchlist (kind, title, note, poster, year, created_at) VALUES (?,?,?,?,?,?)`,
		kind, title, note, poster, year, time.Now().Unix())
	return err
}

func listWatch(db *sql.DB) ([]Watch, error) {
	rows, err := db.Query(`SELECT id, kind, title, COALESCE(note,''), COALESCE(poster,''), COALESCE(year,''), done, COALESCE(fields,'') FROM watchlist ORDER BY done, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Watch
	for rows.Next() {
		var w Watch
		var fieldsJSON string
		if err := rows.Scan(&w.ID, &w.Kind, &w.Title, &w.Note, &w.Poster, &w.Year, &w.Done, &fieldsJSON); err != nil {
			return nil, err
		}
		if fieldsJSON != "" {
			_ = json.Unmarshal([]byte(fieldsJSON), &w.Fields)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func deleteWatch(db *sql.DB, id int64) error {
	_, err := db.Exec(`DELETE FROM watchlist WHERE id=?`, id)
	return err
}

func setWatchDone(db *sql.DB, id int64, done bool) error {
	_, err := db.Exec(`UPDATE watchlist SET done=? WHERE id=?`, done, id)
	return err
}

func setWatchNote(db *sql.DB, id int64, note string) error {
	_, err := db.Exec(`UPDATE watchlist SET note=? WHERE id=?`, note, id)
	return err
}

func setWatchPoster(db *sql.DB, id int64, poster string) error {
	_, err := db.Exec(`UPDATE watchlist SET poster=? WHERE id=?`, poster, id)
	return err
}

func setWatchFields(db *sql.DB, id int64, fields []WatchField) error {
	b, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE watchlist SET fields=? WHERE id=?`, string(b), id)
	return err
}
