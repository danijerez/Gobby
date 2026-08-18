package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Item struct {
	ID            int64  `json:"id"`
	Section       string `json:"section"`
	SectionSource string `json:"section_source"`
	Kind          string `json:"kind"`
	RelPath       string `json:"rel_path"`
	Size          int64  `json:"size"`
	ModTime       int64  `json:"modtime"`
	Title         string `json:"title"`
	Artist        string `json:"artist"`
	Album         string `json:"album"`
	Year          int    `json:"year"`
	Genre         string `json:"genre"`
	Duration      int    `json:"duration"`
	Season        int    `json:"season"`
	Episode       int    `json:"episode"`
	HasCover      bool   `json:"has_cover"`
	CoverID       int64  `json:"cover_id,omitempty"`
	Notes         string `json:"notes"`
	Rating        int    `json:"rating"`
	ImdbID        string `json:"imdb_id"`
	Meta          *Meta  `json:"meta,omitempty"`
	State         string `json:"state"`
	EnrichState   string `json:"enrich_state"`
	Progress      int    `json:"progress"`
	AudioIdx      int    `json:"audio_idx"`
	SubIdx        int    `json:"sub_idx"`
	Color         string `json:"color"`
}

type Meta struct {
	Description string    `json:"description,omitempty"`
	Cast        []string  `json:"cast,omitempty"`
	Director    []string  `json:"director,omitempty"`
	Runtime     string    `json:"runtime,omitempty"`
	Genres      []string  `json:"genres,omitempty"`
	ImdbRating  string    `json:"imdb_rating,omitempty"`
	Year        string    `json:"year,omitempty"`
	Name        string    `json:"-"`
	Tech        *TechInfo `json:"tech,omitempty"`
	Source      string    `json:"source,omitempty"`
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
  progress   INTEGER DEFAULT 0,
  audio_idx  INTEGER DEFAULT 0,
  sub_idx    INTEGER DEFAULT -1,
  color      TEXT,
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

CREATE TABLE IF NOT EXISTS settings (
  key   TEXT PRIMARY KEY,
  value TEXT
);
`

func getSetting(db *sql.DB, key string) string {
	var v string
	db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	return v
}

func setSetting(db *sql.DB, key, value string) error {
	_, err := db.Exec(`INSERT INTO settings (key, value) VALUES (?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

const itemIndexes = `
CREATE INDEX IF NOT EXISTS idx_items_library_kind ON items(library_key, kind);
CREATE INDEX IF NOT EXISTS idx_items_library_section ON items(library_key, section, scan_state);
`

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}

	db.Exec(`PRAGMA journal_mode=WAL`)
	db.Exec(`PRAGMA busy_timeout=5000`)
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}

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
	db.Exec(`ALTER TABLE watchlist ADD COLUMN fields TEXT`)
	db.Exec(`ALTER TABLE items ADD COLUMN added_at INTEGER DEFAULT 0`)
	db.Exec(`ALTER TABLE items ADD COLUMN last_opened INTEGER DEFAULT 0`)
	db.Exec(`ALTER TABLE items ADD COLUMN progress INTEGER DEFAULT 0`)
	db.Exec(`ALTER TABLE items ADD COLUMN audio_idx INTEGER DEFAULT 0`)
	db.Exec(`ALTER TABLE items ADD COLUMN sub_idx INTEGER DEFAULT -1`)
	db.Exec(`ALTER TABLE items ADD COLUMN color TEXT`)

	db.Exec(`UPDATE items SET added_at=COALESCE(updated_at,0) WHERE added_at=0`)

	db.Exec(`UPDATE items SET enrich_state='not_found' WHERE kind='file' AND enrich_state='pending'`)

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

	backfillGenres(db)

	_, _ = db.Exec(`UPDATE items SET section=CASE
		WHEN kind='audio' THEN 'music' WHEN kind='book' THEN 'book'
		WHEN kind='video' AND album<>'' THEN 'series'
		WHEN kind='video' THEN 'movie' ELSE 'files' END
		WHERE section='' OR section='files' AND section_source='auto'`)

	_, _ = db.Exec(`UPDATE items SET enrich_state='found' WHERE enrich_state='pending' AND cover IS NOT NULL`)

	_, _ = db.Exec(`UPDATE items SET section='photos', section_source='auto'
		WHERE kind='file' AND section_source='auto' AND section<>'photos' AND (
		  LOWER(rel_path) LIKE '%.jpg' OR LOWER(rel_path) LIKE '%.jpeg' OR
		  LOWER(rel_path) LIKE '%.png' OR LOWER(rel_path) LIKE '%.gif' OR
		  LOWER(rel_path) LIKE '%.webp' OR LOWER(rel_path) LIKE '%.bmp' OR
		  LOWER(rel_path) LIKE '%.heic' OR LOWER(rel_path) LIKE '%.avif' OR
		  LOWER(rel_path) LIKE '%.tiff')`)
	return db, nil
}

func migrateLibraries(db *sql.DB) error {
	var tableSQL string
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='items'`).Scan(&tableSQL); err != nil {
		return err
	}

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

	_, err = db.Exec(`ALTER TABLE items ADD COLUMN library_key TEXT NOT NULL DEFAULT 'legacy'`)
	return err
}

func upsertScanned(db *sql.DB, libraryKey string, runID int64, it Item, cover []byte) error {
	it.Section = automaticSection(it)
	enrichState := "pending"
	if len(cover) > 0 {
		enrichState = "found"
	} else if it.Kind == "file" {

		enrichState = "not_found"
	}
	var metaJSON any
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

func modTimeOf(db *sql.DB, libraryKey, relPath string) (int64, bool) {
	var mt int64
	err := db.QueryRow(`SELECT modtime FROM items WHERE library_key=? AND rel_path=?`, libraryKey, relPath).Scan(&mt)
	if err != nil {
		return 0, false
	}
	return mt, true
}

func rematchMoved(db *sql.DB, libraryKey, newRel string, size, modtime, runID int64) (bool, error) {
	if size == 0 {
		return false, nil
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

func backfillColors(db *sql.DB, libraryKey, root string) {
	rows, err := db.Query(`SELECT id, rel_path, (cover IS NOT NULL) FROM items
		WHERE library_key=? AND scan_state='present' AND (color IS NULL OR color='')`, libraryKey)
	if err != nil {
		return
	}
	type item struct {
		id     int64
		rel    string
		hasCov bool
	}
	var items []item
	for rows.Next() {
		var it item
		if rows.Scan(&it.id, &it.rel, &it.hasCov) == nil {
			items = append(items, it)
		}
	}
	rows.Close()
	for _, it := range items {
		var data []byte
		if it.hasCov {
			db.QueryRow(`SELECT cover FROM items WHERE id=? AND library_key=?`, it.id, libraryKey).Scan(&data)
		} else if isImageExt(it.rel) {
			full := filepath.Join(root, filepath.FromSlash(it.rel))
			if rel, e := filepath.Rel(root, full); e == nil && !strings.HasPrefix(rel, "..") {
				data, _ = os.ReadFile(full)
			}
		}
		if len(data) == 0 {
			continue
		}
		if c := dominantColor(data); c != "" {
			db.Exec(`UPDATE items SET color=? WHERE id=? AND library_key=?`, c, it.id, libraryKey)
		}
	}
}

func backfillGenres(db *sql.DB) {
	rows, err := db.Query(`SELECT id, meta FROM items WHERE (genre IS NULL OR genre='') AND meta IS NOT NULL AND meta<>''`)
	if err != nil {
		return
	}
	type upd struct {
		id    int64
		genre string
	}
	var updates []upd
	for rows.Next() {
		var id int64
		var raw string
		if rows.Scan(&id, &raw) != nil {
			continue
		}
		var m Meta
		if json.Unmarshal([]byte(raw), &m) == nil && len(m.Genres) > 0 {
			updates = append(updates, upd{id, strings.Join(m.Genres, ", ")})
		}
	}
	rows.Close()
	for _, u := range updates {
		db.Exec(`UPDATE items SET genre=? WHERE id=? AND (genre IS NULL OR genre='')`, u.genre, u.id)
	}
}

func backfillEpisodes(db *sql.DB, root string) {
	rows, err := db.Query(`SELECT id, rel_path FROM items WHERE section='series' AND episode=0 AND scan_state='present'`)
	if err != nil {
		return
	}
	type upd struct {
		id     int64
		season int
		ep     int
		title  string
	}
	var updates []upd
	for rows.Next() {
		var id int64
		var rel string
		if rows.Scan(&id, &rel) != nil {
			continue
		}
		p := parseName(filepath.Join(root, filepath.FromSlash(rel)))
		if p.Episode > 0 {
			updates = append(updates, upd{id, p.Season, p.Episode, p.Title})
		}
	}
	rows.Close()
	for _, u := range updates {
		db.Exec(`UPDATE items SET season=?, episode=?, title=? WHERE id=? AND episode=0`, u.season, u.ep, u.title, u.id)
	}
}

func facets(db *sql.DB, libraryKey string) (genres, colors []string, yearMin, yearMax int) {
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

	crows, err := db.Query(`SELECT DISTINCT color FROM items WHERE library_key=? AND scan_state='present' AND color IS NOT NULL AND color<>''`, libraryKey)
	if err == nil {
		defer crows.Close()
		for crows.Next() {
			var c string
			if crows.Scan(&c) == nil && c != "" {
				colors = append(colors, c)
			}
		}
	}

	db.QueryRow(`SELECT COALESCE(MIN(year),0), COALESCE(MAX(year),0) FROM items WHERE library_key=? AND scan_state='present' AND year>0`, libraryKey).Scan(&yearMin, &yearMax)
	return genres, colors, yearMin, yearMax
}

type LibraryInfo struct {
	Key       string `json:"key"`
	Root      string `json:"root"`
	Total     int    `json:"total"`
	Reachable bool   `json:"reachable"`
	Current   bool   `json:"current"`
}

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
		if dirExists(li.Key) {
			li.Root = li.Key
			li.Reachable = true
		}
		li.Current = li.Key == currentKey
		out = append(out, li)
	}
	return out, rows.Err()
}

const itemColumns = `library_key, kind, rel_path, size, modtime, title, artist, album,
	year, genre, duration, season, episode, cover, meta, imdb_id, notes, rating,
	section, section_source, section_reason, scan_state, last_seen_scan, enrich_state,
	added_at, last_opened, progress, updated_at`

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

	_, _ = db.Exec(`INSERT INTO watchlist (kind, title, note, poster, year, done, created_at)
		SELECT kind, title, note, poster, year, done, created_at FROM imp.watchlist
		WHERE title NOT IN (SELECT title FROM watchlist)`)
	return add, nil
}

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
		if isImageExt(it.RelPath) {
			return "photos"
		}
		return "files"
	}
}

func isImageExt(path string) bool {
	switch strings.ToLower(filepathExt(path)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".heic", ".avif", ".tiff":
		return true
	}
	return false
}

func listItems(db *sql.DB, libraryKey, kind, q string) ([]Item, error) {
	rows, err := db.Query(`
		SELECT i.id, i.kind, i.rel_path, COALESCE(i.size,0), COALESCE(i.modtime,0), i.section, i.section_source, i.scan_state, i.enrich_state,
		       COALESCE(i.title,''), COALESCE(i.artist,''), COALESCE(i.album,''),
		       COALESCE(i.year,0), COALESCE(i.genre,''), COALESCE(i.duration,0),
		       COALESCE(i.season,0), COALESCE(i.episode,0),
		       (i.cover IS NOT NULL), COALESCE(i.notes,''), COALESCE(i.rating,0), COALESCE(i.progress,0), COALESCE(i.color,''),
		       COALESCE((
		         CASE WHEN i.cover IS NOT NULL THEN i.id
		         WHEN i.album<>'' THEN (SELECT s.id FROM items s
		           WHERE s.library_key=i.library_key AND s.album=i.album AND s.kind=i.kind
		             AND s.cover IS NOT NULL LIMIT 1)
		         END), 0)
		FROM items i
		WHERE i.library_key=? AND i.scan_state='present' AND (?='' OR i.kind=?)
		  AND (?='' OR i.title LIKE '%'||?||'%' OR i.artist LIKE '%'||?||'%' OR i.album LIKE '%'||?||'%')
		ORDER BY i.album, i.season, i.episode, i.title`,
		libraryKey, kind, kind, q, q, q, q)
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
			&it.Season, &it.Episode, &it.HasCover, &it.Notes, &it.Rating, &it.Progress, &it.Color, &coverID); err != nil {
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

func nextInAlbum(db *sql.DB, libraryKey string, id int64) (Item, bool) {
	cur, err := getItem(db, libraryKey, id)
	if err != nil || cur.Album == "" {
		return Item{}, false
	}
	items, err := listItems(db, libraryKey, cur.Kind, "")
	if err != nil {
		return Item{}, false
	}
	var sibs []Item
	for _, it := range items {
		if it.Album == cur.Album {
			sibs = append(sibs, it)
		}
	}
	sort.Slice(sibs, func(i, j int) bool {
		if sibs[i].Season != sibs[j].Season {
			return sibs[i].Season < sibs[j].Season
		}
		if sibs[i].Episode != sibs[j].Episode {
			return sibs[i].Episode < sibs[j].Episode
		}
		return sibs[i].Title < sibs[j].Title
	})
	for i, it := range sibs {
		if it.ID == id && i+1 < len(sibs) {
			return sibs[i+1], true
		}
	}
	return Item{}, false
}

func markOpened(db *sql.DB, libraryKey string, id int64) error {
	_, err := db.Exec(`UPDATE items SET last_opened=? WHERE id=? AND library_key=?`, time.Now().Unix(), id, libraryKey)
	return err
}

func deleteItemRow(db *sql.DB, libraryKey string, id int64) error {
	_, err := db.Exec(`DELETE FROM items WHERE id=? AND library_key=?`, id, libraryKey)
	return err
}

func setProgress(db *sql.DB, libraryKey string, id int64, secs int) error {
	if secs < 0 {
		secs = 0
	}
	_, err := db.Exec(`UPDATE items SET progress=? WHERE id=? AND library_key=?`, secs, id, libraryKey)
	return err
}

func setTracks(db *sql.DB, libraryKey string, id int64, audioIdx, subIdx int) error {
	_, err := db.Exec(`UPDATE items SET audio_idx=?, sub_idx=? WHERE id=? AND library_key=?`, audioIdx, subIdx, id, libraryKey)
	return err
}

func homeShelf(db *sql.DB, libraryKey, where, order string, limit int) ([]Item, error) {
	rows, err := db.Query(`
		SELECT i.id, i.kind, i.rel_path, COALESCE(i.size,0), COALESCE(i.modtime,0), i.section, i.section_source, i.scan_state, i.enrich_state,
		       COALESCE(i.title,''), COALESCE(i.artist,''), COALESCE(i.album,''),
		       COALESCE(i.year,0), COALESCE(i.genre,''), COALESCE(i.duration,0),
		       COALESCE(i.season,0), COALESCE(i.episode,0),
		       (i.cover IS NOT NULL), COALESCE(i.notes,''), COALESCE(i.rating,0), COALESCE(i.progress,0), COALESCE(i.color,''),
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
			&it.Season, &it.Episode, &it.HasCover, &it.Notes, &it.Rating, &it.Progress, &it.Color, &coverID); err != nil {
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

func getItem(db *sql.DB, libraryKey string, id int64) (Item, error) {
	var it Item
	var metaJSON, imdbID sql.NullString
	err := db.QueryRow(`
		SELECT id, kind, rel_path, COALESCE(size,0), COALESCE(modtime,0), section, section_source, scan_state, enrich_state,
		       COALESCE(title,''), COALESCE(artist,''), COALESCE(album,''),
		       COALESCE(year,0), COALESCE(genre,''), COALESCE(duration,0),
		       COALESCE(season,0), COALESCE(episode,0),
		       (cover IS NOT NULL), COALESCE(notes,''), COALESCE(rating,0), COALESCE(progress,0),
		       COALESCE(audio_idx,0), COALESCE(sub_idx,-1), meta, imdb_id
		FROM items WHERE id=? AND library_key=?`, id, libraryKey).Scan(
		&it.ID, &it.Kind, &it.RelPath, &it.Size, &it.ModTime, &it.Section, &it.SectionSource, &it.State, &it.EnrichState,
		&it.Title, &it.Artist, &it.Album, &it.Year, &it.Genre, &it.Duration,
		&it.Season, &it.Episode, &it.HasCover, &it.Notes, &it.Rating, &it.Progress,
		&it.AudioIdx, &it.SubIdx, &metaJSON, &imdbID)
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
	if it.HasCover {
		it.CoverID = it.ID
	} else if it.Album != "" {
		var cid int64
		db.QueryRow(`SELECT id FROM items WHERE library_key=? AND kind=? AND album=? AND cover IS NOT NULL AND scan_state='present' LIMIT 1`,
			libraryKey, it.Kind, it.Album).Scan(&cid)
		if cid != 0 {
			it.CoverID = cid
			it.HasCover = true
		}
	}
	return it, nil
}

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
	if g := strings.Join(m.Genres, ", "); g != "" && prevErr == nil {
		if prev.Album != "" {
			db.Exec(`UPDATE items SET genre=? WHERE library_key=? AND kind=? AND album=? AND (genre IS NULL OR genre='')`, g, libraryKey, prev.Kind, prev.Album)
		} else {
			db.Exec(`UPDATE items SET genre=? WHERE id=? AND library_key=? AND (genre IS NULL OR genre='')`, g, id, libraryKey)
		}
	}
	return nil
}

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
		mm.Tech = s.tech
		if b, err := json.Marshal(mm); err == nil {
			db.Exec(`UPDATE items SET meta=?, imdb_id=? WHERE id=? AND library_key=?`, string(b), imdbID, s.id, libraryKey)
		}
	}
}

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

func setTitle(db *sql.DB, libraryKey string, id int64, title string) error {
	_, err := db.Exec(`UPDATE items SET title=?, updated_at=? WHERE id=? AND library_key=?`, title, time.Now().Unix(), id, libraryKey)
	return err
}

func setImdbID(db *sql.DB, libraryKey string, id int64, imdbID string) error {
	_, err := db.Exec(`UPDATE items SET imdb_id=? WHERE id=? AND library_key=?`, imdbID, id, libraryKey)
	return err
}

type Group struct {
	Name  string `json:"name"`
	Items []Item `json:"items"`
}

type BrowseView struct {
	Groups     []Group `json:"groups"`
	Loose      []Item  `json:"loose"`
	LooseTotal int     `json:"loose_total"`
	Page       int     `json:"page"`
	PageSize   int     `json:"page_size"`
}

const browsePageSize = 60

type Filters struct {
	Ext       string
	YearMin   int
	YearMax   int
	Cover     string
	RatingMin int
	Genre     string
	Color     string
}

func (f Filters) empty() bool {
	return f.Ext == "" && f.YearMin == 0 && f.YearMax == 0 && f.Cover == "" && f.RatingMin == 0 && f.Genre == "" && f.Color == ""
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
	if f.Color != "" && it.Color != f.Color {
		return false
	}
	return true
}

func browseView(db *sql.DB, libraryKey, section, q string, f Filters, page int) (BrowseView, error) {
	dbKind := map[string]string{"series": "video", "movie": "video", "music": "audio", "audio": "audio", "book": "book", "photos": "file"}[section]
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

		if it.Section != section && !(section == "audio" && it.Section == "music") {
			continue
		}

		if it.Album == "" || section == "movie" || section == "files" || section == "photos" {
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

type SearchResult struct {
	Item
	Group string `json:"group,omitempty"`
}

func searchAll(db *sql.DB, libraryKey, q string, limit int) ([]SearchResult, error) {
	if strings.TrimSpace(q) == "" {
		return []SearchResult{}, nil
	}
	items, err := listItems(db, libraryKey, "", q)
	if err != nil {
		return nil, err
	}
	var out []SearchResult
	seenGroup := map[string]bool{}
	for _, it := range items {

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
		if limit > 0 && len(out) >= limit {
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

type TreeNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	Size     int64       `json:"size"`
	Count    int         `json:"count"`
	IsDir    bool        `json:"dir"`
	ID       int64       `json:"id,omitempty"`
	Kind     string      `json:"kind,omitempty"`
	Section  string      `json:"section,omitempty"`
	Children []*TreeNode `json:"children,omitempty"`
}

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
					child.Name = title
				}
			}
			cur = child
		}
	}
	sortTree(root)
	sectionOf(root)
	return root, rows.Err()
}

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
	var kind, album string
	err := db.QueryRow(`SELECT cover, kind, album FROM items WHERE id=? AND library_key=? AND scan_state='present'`, id, libraryKey).Scan(&b, &kind, &album)
	if err != nil {
		return b, err
	}
	if len(b) == 0 && album != "" {
		db.QueryRow(`SELECT cover FROM items WHERE library_key=? AND kind=? AND album=? AND cover IS NOT NULL AND length(cover)>0 AND scan_state='present' LIMIT 1`,
			libraryKey, kind, album).Scan(&b)
	}
	return b, nil
}

func setCover(db *sql.DB, libraryKey string, id int64, cover []byte) error {
	_, err := db.Exec(`UPDATE items SET cover=? WHERE id=? AND library_key=?`, cover, id, libraryKey)
	if err == nil {
		if c := dominantColor(cover); c != "" {
			db.Exec(`UPDATE items SET color=? WHERE id=? AND library_key=?`, c, id, libraryKey)
		}
	}
	return err
}

func setEnrichState(db *sql.DB, libraryKey string, id int64, state string) error {
	_, err := db.Exec(`UPDATE items SET enrich_state=? WHERE id=? AND library_key=?`, state, id, libraryKey)
	return err
}

func setGroupEnrichState(db *sql.DB, libraryKey string, it Item, state string) error {
	if it.Album == "" {
		return setEnrichState(db, libraryKey, it.ID, state)
	}
	_, err := db.Exec(`UPDATE items SET enrich_state=? WHERE library_key=? AND kind=? AND album=? AND enrich_state='pending'`, state, libraryKey, it.Kind, it.Album)
	return err
}

func updateItem(db *sql.DB, libraryKey string, id int64, m ItemEdit) error {
	_, err := db.Exec(`UPDATE items SET title=?, notes=?, rating=?, year=?, artist=?, album=?, genre=?, updated_at=? WHERE id=? AND library_key=?`,
		m.Title, m.Notes, m.Rating, m.Year, m.Artist, m.Album, m.Genre, time.Now().Unix(), id, libraryKey)
	return err
}

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
	case "movie", "series", "music", "book", "files", "photos":
	default:
		return fmt.Errorf("invalid section %q", section)
	}
	_, err := db.Exec(`UPDATE items SET section=?, section_source='manual', section_reason='manual edit', updated_at=? WHERE id=? AND library_key=?`, section, time.Now().Unix(), id, libraryKey)
	return err
}

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
