package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const userAgent = "Gobby/0.1 (personal media library)"

var coverHTTP = &http.Client{Timeout: 15 * time.Second}

func getJSON(u string, v any) error {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := coverHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func getBytes(u string) ([]byte, error) {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", userAgent)
	resp, err := coverHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

type cinemetaMeta struct {
	ID          string `json:"id"`
	Poster      string `json:"poster"`
	Name        string `json:"name"`
	ReleaseInfo string `json:"releaseInfo"` // "2023" or "2019-2022"
}

// cinemetaSearch finds the best movie/series match by title via Stremio's
// Cinemeta (no key). kind is "movie" or "series". When year>0, a candidate
// whose release year matches is preferred over the raw top hit — this fixes
// wrong posters for remakes/same-title films. Returns poster URL and imdb id.
func cinemetaSearch(kind, title string, year int) (poster, imdbID string, err error) {
	var res struct {
		Metas []cinemetaMeta `json:"metas"`
	}
	u := fmt.Sprintf("https://v3-cinemeta.strem.io/catalog/%s/top/search=%s.json", kind, url.QueryEscape(title))
	if err := getJSON(u, &res); err != nil {
		return "", "", err
	}
	if len(res.Metas) == 0 {
		return "", "", nil
	}
	best := res.Metas[0]
	if year > 0 {
		for _, m := range res.Metas {
			if strings.HasPrefix(m.ReleaseInfo, fmt.Sprint(year)) {
				best = m
				break
			}
		}
	}
	return best.Poster, best.ID, nil
}

func cinemetaDetail(kind, imdbID string) (Meta, error) {
	m, _, err := cinemetaMetaFull(kind, imdbID)
	return m, err
}

func cinemetaPosterFor(kind, imdbID string) string {
	_, poster, _ := cinemetaMetaFull(kind, imdbID)
	return poster
}

func cinemetaMetaFull(kind, imdbID string) (Meta, string, error) {
	var res struct {
		Meta struct {
			Name        string   `json:"name"`
			Poster      string   `json:"poster"`
			Description string   `json:"description"`
			Cast        []string `json:"cast"`
			Director    []string `json:"director"`
			Runtime     string   `json:"runtime"`
			Genres      []string `json:"genres"`
			ImdbRating  string   `json:"imdbRating"`
			Year        string   `json:"year"`        // "2008" or "2008–2013"
			ReleaseInfo string   `json:"releaseInfo"` // same idea, fallback
		} `json:"meta"`
	}
	u := fmt.Sprintf("https://v3-cinemeta.strem.io/meta/%s/%s.json", kind, imdbID)
	if err := getJSON(u, &res); err != nil {
		return Meta{}, "", err
	}
	yr := res.Meta.Year
	if yr == "" {
		yr = res.Meta.ReleaseInfo
	}
	return Meta{
		Name:        res.Meta.Name,
		Description: res.Meta.Description,
		Cast:        res.Meta.Cast,
		Director:    res.Meta.Director,
		Runtime:     res.Meta.Runtime,
		Genres:      res.Meta.Genres,
		ImdbRating:  res.Meta.ImdbRating,
		Year:        yr,
		Source:      "cinemeta",
	}, res.Meta.Poster, nil
}

// itunesPoster is the keyless fallback for anything Cinemeta/Open Library miss.
// Apple's iTunes Search API needs no key and covers movies, music and ebooks.
// media is "movie", "music" or "ebook". Returns an upscaled artwork URL.
func itunesPoster(media, term string) (poster string, err error) {
	var res struct {
		Results []struct {
			ArtworkURL100 string `json:"artworkUrl100"`
		} `json:"results"`
	}
	u := fmt.Sprintf("https://itunes.apple.com/search?media=%s&limit=1&term=%s", media, url.QueryEscape(term))
	if err := getJSON(u, &res); err != nil {
		return "", err
	}
	if len(res.Results) == 0 || res.Results[0].ArtworkURL100 == "" {
		return "", nil
	}
	// 100px thumbnails upscale to 600px by swapping the size segment.
	return strings.Replace(res.Results[0].ArtworkURL100, "100x100bb", "600x600bb", 1), nil
}

// bookQuery builds a clean Open Library query: author folder + the book title
// with a leading "Surname, Name - " prefix stripped.
func bookQuery(author, title string) string {
	if i := strings.LastIndex(title, " - "); i >= 0 {
		title = title[i+3:]
	}
	return strings.TrimSpace(author + " " + title)
}

// openLibraryPoster finds a book cover by query, returning the poster URL and
// the Open Library cover id (used later for a forced re-fetch).
func openLibraryPoster(query string) (poster, coverID string, err error) {
	var res struct {
		Docs []struct {
			CoverID int `json:"cover_i"`
		} `json:"docs"`
	}
	u := "https://openlibrary.org/search.json?limit=1&q=" + url.QueryEscape(query)
	if err := getJSON(u, &res); err != nil {
		return "", "", err
	}
	if len(res.Docs) == 0 || res.Docs[0].CoverID == 0 {
		return "", "", nil
	}
	id := fmt.Sprint(res.Docs[0].CoverID)
	return fmt.Sprintf("https://covers.openlibrary.org/b/id/%s-L.jpg", id), id, nil
}

// SearchHit is one candidate for the watchlist search picker.
type SearchHit struct {
	Kind   string `json:"kind"`   // movie | series | book
	Title  string `json:"title"`
	Year   string `json:"year"`   // may be a range like "2019-2022"
	Poster string `json:"poster"` // remote URL, shown in the picker
	ExtID  string `json:"ext_id"` // imdb id / open-library cover id (for later enrich)
}

// searchTitles queries the free providers for ONLY the requested kinds, so a
// search scoped to "book" never returns movies. movie/series → Cinemeta,
// book → Open Library, audio → iTunes. Unknown/keyless kinds (game, other) have
// no provider and are simply skipped (added manually in the UI). Errors from any
// single provider are swallowed — a partial list beats no list.
func searchTitles(q string, kinds []string) []SearchHit {
	want := map[string]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	if len(want) == 0 { // default: search everything with a provider
		want = map[string]bool{"movie": true, "series": true, "book": true, "audio": true, "game": true}
	}
	var out []SearchHit

	for _, kind := range []string{"movie", "series"} {
		if !want[kind] {
			continue
		}
		var res struct {
			Metas []cinemetaMeta `json:"metas"`
		}
		u := fmt.Sprintf("https://v3-cinemeta.strem.io/catalog/%s/top/search=%s.json", kind, url.QueryEscape(q))
		if getJSON(u, &res) == nil {
			for i, m := range res.Metas {
				if i >= 6 {
					break
				}
				out = append(out, SearchHit{Kind: kind, Title: m.Name, Year: m.ReleaseInfo, Poster: m.Poster, ExtID: m.ID})
			}
		}
	}

	if want["book"] {
		var books struct {
			Docs []struct {
				Title   string   `json:"title"`
				Year    int      `json:"first_publish_year"`
				CoverID int      `json:"cover_i"`
				Author  []string `json:"author_name"`
			} `json:"docs"`
		}
		if getJSON("https://openlibrary.org/search.json?limit=6&q="+url.QueryEscape(q), &books) == nil {
			for _, d := range books.Docs {
				h := SearchHit{Kind: "book", Title: d.Title}
				if len(d.Author) > 0 {
					h.Title = d.Title + " — " + d.Author[0]
				}
				if d.Year > 0 {
					h.Year = fmt.Sprint(d.Year)
				}
				if d.CoverID > 0 {
					h.ExtID = fmt.Sprint(d.CoverID)
					h.Poster = fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-M.jpg", d.CoverID)
				}
				out = append(out, h)
			}
		}
	}

	if want["game"] {
		// CheapShark — free, no key. Covers PC + console titles (Steam's own search
		// misses Nintendo etc.). Its `thumb` is a wide 231×87 capsule that looks
		// squashed in a poster tile, so when the game is on Steam we swap it for
		// Steam's vertical library art (600×900). Needs a descriptive User-Agent,
		// which getJSON already sends.
		var games []struct {
			GameID     string `json:"gameID"`
			Name       string `json:"external"`
			Thumb      string `json:"thumb"`
			SteamAppID string `json:"steamAppID"`
		}
		u := "https://www.cheapshark.com/api/1.0/games?limit=6&title=" + url.QueryEscape(q)
		if getJSON(u, &games) == nil {
			for _, g := range games {
				poster := g.Thumb
				if g.SteamAppID != "" {
					poster = "https://steamcdn-a.akamaihd.net/steam/apps/" + g.SteamAppID + "/library_600x900.jpg"
				}
				out = append(out, SearchHit{Kind: "game", Title: g.Name, Poster: poster, ExtID: g.GameID})
			}
		}
	}

	if want["audio"] {
		var res struct {
			Results []struct {
				ArtistName    string `json:"artistName"`
				CollectionName string `json:"collectionName"`
				ArtworkURL100 string `json:"artworkUrl100"`
				ReleaseDate   string `json:"releaseDate"` // "2019-06-21T07:00:00Z"
			} `json:"results"`
		}
		u := "https://itunes.apple.com/search?media=music&entity=album&limit=6&term=" + url.QueryEscape(q)
		if getJSON(u, &res) == nil {
			for _, r := range res.Results {
				h := SearchHit{Kind: "audio", Title: r.CollectionName}
				if r.ArtistName != "" {
					h.Title = r.CollectionName + " — " + r.ArtistName
				}
				if len(r.ReleaseDate) >= 4 {
					h.Year = r.ReleaseDate[:4]
				}
				if r.ArtworkURL100 != "" {
					h.Poster = strings.Replace(r.ArtworkURL100, "100x100bb", "300x300bb", 1)
				}
				out = append(out, h)
			}
		}
	}
	return out
}

// coverArtPoster finds an album cover via MusicBrainz, returning the poster URL
// and the release-group MBID (used later for a forced re-fetch).
func coverArtPoster(artist, album string) (poster, mbid string, err error) {
	q := fmt.Sprintf("release:%s", album)
	if artist != "" {
		q = fmt.Sprintf("artist:%s AND release:%s", artist, album)
	}
	var mb struct {
		Groups []struct {
			ID string `json:"id"`
		} `json:"release-groups"`
	}
	u := "https://musicbrainz.org/ws/2/release-group?fmt=json&limit=1&query=" + url.QueryEscape(q)
	if err := getJSON(u, &mb); err != nil {
		return "", "", err
	}
	if len(mb.Groups) == 0 {
		return "", "", nil
	}
	return fmt.Sprintf("https://coverartarchive.org/release-group/%s/front", mb.Groups[0].ID), mb.Groups[0].ID, nil
}

// Progress is the live state of a background run (scan or enrich), polled by
// the UI. Total==0 while Running means an indeterminate run (the scanner does
// not know the file count up front and won't waste a pass counting).
type Progress struct {
	mu      sync.Mutex
	Running bool   `json:"running"`
	stop    bool   // set by requestStop, checked each iteration
	Total   int    `json:"total"`
	Done    int    `json:"done"`
	Found   int    `json:"found"`
	LastID  int64  `json:"last_id"`
	Phase   string `json:"phase"` // "scan" | "enrich" — labels the UI bar
}

func (p *Progress) snapshot() Progress {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Progress{Running: p.Running, Total: p.Total, Done: p.Done, Found: p.Found, LastID: p.LastID, Phase: p.Phase}
}

func (p *Progress) requestStop() {
	p.mu.Lock()
	p.stop = true
	p.mu.Unlock()
}

var enrichProgress = &Progress{}
var scanProgress = &Progress{}

// fetchCover looks up and saves a cover (+ rich meta for video) for one item.
// idOverride, if set, skips the search and uses that external id directly. The
// id's meaning depends on kind: IMDb tt-id (video), Open Library cover id (book),
// MusicBrainz release-group MBID (audio). Returns whether a cover was saved.
func fetchCover(db *sql.DB, libraryKey string, it Item, idOverride string) bool {
	var posterURL, extID string
	var err error
	cmKind := "movie"
	if it.Kind == "video" && it.Album != "" {
		cmKind = "series"
	}

	switch {
	case it.Kind == "video" && idOverride != "":
		extID = idOverride
		if m, e := cinemetaDetail(cmKind, extID); e == nil {
			posterURL = cinemetaPosterFor(cmKind, extID)
			setMeta(db, libraryKey, it.ID, m, extID)
			// manual identify by IMDb id → adopt the official title, fixing a
			// mangled filename-derived one ("N0s0tros m1080p" → "Nosotros").
			if m.Name != "" {
				setTitle(db, libraryKey, it.ID, m.Name)
			}
		}
	case it.Kind == "video" && it.Album != "":
		posterURL, extID, err = cinemetaSearch("series", it.Album, 0)
	case it.Kind == "video":
		posterURL, extID, err = cinemetaSearch("movie", it.Title, it.Year)
	case it.Kind == "book" && idOverride != "":
		extID = idOverride
		posterURL = fmt.Sprintf("https://covers.openlibrary.org/b/id/%s-L.jpg", idOverride)
	case it.Kind == "book":
		posterURL, extID, err = openLibraryPoster(bookQuery(it.Album, it.Title))
	case it.Kind == "audio" && idOverride != "":
		extID = idOverride
		posterURL = fmt.Sprintf("https://coverartarchive.org/release-group/%s/front", idOverride)
	case it.Kind == "audio":
		posterURL, extID, err = coverArtPoster(it.Artist, it.Album)
	}

	if it.Kind == "video" && idOverride == "" && err == nil && extID != "" {
		if m, e := cinemetaDetail(cmKind, extID); e == nil {
			setMeta(db, libraryKey, it.ID, m, extID)
		}
	}
	if extID != "" {
		setImdbID(db, libraryKey, it.ID, extID) // persist whichever external id we used
	}

	// Keyless fallback: if the primary source found no poster, try iTunes.
	if posterURL == "" && idOverride == "" {
		media := map[string]string{"video": "movie", "audio": "music", "book": "ebook"}[it.Kind]
		term := it.Title
		if it.Album != "" {
			term = it.Album // series / album name is the better search term
		}
		if p, e := itunesPoster(media, term); e == nil && p != "" {
			posterURL = p
			setMetaSource(db, libraryKey, it.ID, "itunes")
		}
	}

	if err != nil || posterURL == "" {
		return false
	}
	img, e := getBytes(posterURL)
	if e != nil || len(img) == 0 {
		return false
	}
	return setCover(db, libraryKey, it.ID, img) == nil
}

// fetchMetaOnly looks up an item's external id (and rich meta) WITHOUT touching
// its cover — for files that already carry embedded artwork but have no id yet,
// so the id field and its reference link work. Video/series pull Cinemeta detail;
// audio just records the MusicBrainz release-group id.
func fetchMetaOnly(db *sql.DB, libraryKey string, it Item) {
	switch {
	case it.Kind == "video":
		cmKind := "movie"
		var extID string
		if it.Album != "" {
			cmKind = "series"
			_, extID, _ = cinemetaSearch("series", it.Album, 0)
		} else {
			_, extID, _ = cinemetaSearch("movie", it.Title, it.Year)
		}
		if extID == "" {
			return
		}
		if m, e := cinemetaDetail(cmKind, extID); e == nil {
			setMeta(db, libraryKey, it.ID, m, extID)
		} else {
			setImdbID(db, libraryKey, it.ID, extID)
		}
	case it.Kind == "audio":
		if _, mbid, _ := coverArtPoster(it.Artist, it.Album); mbid != "" {
			setImdbID(db, libraryKey, it.ID, mbid)
		}
	}
}

// enrich fetches covers for a whole tab. pendingOnly is used by the startup
// synchronizer so it only touches new or changed files. The UI can still
// explicitly retry unresolved titles.
func enrich(db *sql.DB, libraryKey, kind string, force, pendingOnly bool) (int, error) {
	dbKind := kind
	onlyGroups, onlyLoose := false, false
	switch kind {
	case "series":
		dbKind, onlyGroups = "video", true
	case "movie":
		dbKind, onlyLoose = "video", true
	case "all":
		dbKind = ""
	}

	items, err := listItems(db, libraryKey, dbKind, "")
	if err != nil {
		return 0, err
	}

	var jobs []Item
	doneGroups := map[string]bool{}
	for _, it := range items {
		// generic files (zip/iso/docs…) aren't identifiable media — enriching
		// them just pulls unrelated book/movie posters. Never enrich them.
		if it.Kind == "file" {
			continue
		}
		if pendingOnly && it.EnrichState != "pending" {
			continue
		}
		if it.HasCover && !force {
			// The file already carries artwork (embedded tags), so we won't refetch
			// the cover. But if it has no external id yet, still look one up so the
			// id field / references work — meta only, the cover is left untouched.
			if (it.Kind == "audio" || it.Kind == "video") && it.ImdbID == "" && !onlyLoose {
				if it.Album == "" || !doneGroups[it.Album] {
					if it.Album != "" {
						doneGroups[it.Album] = true
					}
					fetchMetaOnly(db, libraryKey, it)
				}
			}
			if pendingOnly {
				_ = setGroupEnrichState(db, libraryKey, it, "found")
			}
			continue
		}
		if it.Album == "" && onlyGroups {
			continue
		}
		if it.Album != "" {
			if onlyLoose || doneGroups[it.Album] {
				continue
			}
			doneGroups[it.Album] = true
		}
		jobs = append(jobs, it)
	}

	enrichProgress.mu.Lock()
	enrichProgress.Running = true
	enrichProgress.stop = false
	enrichProgress.Phase = "enrich"
	enrichProgress.Total = len(jobs)
	enrichProgress.Done, enrichProgress.Found, enrichProgress.LastID = 0, 0, 0
	enrichProgress.mu.Unlock()

	found := 0
	for _, it := range jobs {
		enrichProgress.mu.Lock()
		stopped := enrichProgress.stop
		enrichProgress.mu.Unlock()
		if stopped {
			break
		}

		if fetchCover(db, libraryKey, it, "") {
			found++
			if pendingOnly {
				_ = setGroupEnrichState(db, libraryKey, it, "found")
			} else {
				_ = setEnrichState(db, libraryKey, it.ID, "found")
			}
			enrichProgress.mu.Lock()
			enrichProgress.Found = found
			enrichProgress.LastID = it.ID
			enrichProgress.mu.Unlock()
		} else if pendingOnly {
			_ = setGroupEnrichState(db, libraryKey, it, "not_found")
		}
		enrichProgress.mu.Lock()
		enrichProgress.Done++
		enrichProgress.mu.Unlock()
		time.Sleep(200 * time.Millisecond)
	}

	enrichProgress.mu.Lock()
	stopped := enrichProgress.stop
	enrichProgress.Running = false
	enrichProgress.mu.Unlock()

	// If an automatic (pendingOnly) pass ran to completion, settle everything
	// still pending as not_found. Otherwise group members that never became a
	// job, or items whose lookup found nothing, stay pending forever and re-run
	// enrichment on every startup. A user-stopped run is left alone to retry.
	if pendingOnly && !stopped {
		_, _ = db.Exec(`UPDATE items SET enrich_state='not_found' WHERE library_key=? AND scan_state='present' AND enrich_state='pending'`, libraryKey)
	}
	return found, nil
}

// enrichOne re-fetches a single item, optionally forcing a specific imdb id.
func enrichOne(db *sql.DB, libraryKey string, id int64, imdbID string) (bool, error) {
	it, err := getItem(db, libraryKey, id)
	if err != nil {
		return false, err
	}
	found := fetchCover(db, libraryKey, it, imdbID)
	if found {
		_ = setEnrichState(db, libraryKey, id, "found")
	} else {
		_ = setEnrichState(db, libraryKey, id, "not_found")
	}
	return found, nil
}
