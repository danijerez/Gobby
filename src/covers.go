package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
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
	ReleaseInfo string `json:"releaseInfo"`
}

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
			Year        string   `json:"year"`
			ReleaseInfo string   `json:"releaseInfo"`
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

func enrichWatchHit(kind, extID string) (note, poster string, fields []WatchField) {
	if extID == "" || (kind != "movie" && kind != "series") {
		return "", "", nil
	}
	m, p, err := cinemetaMetaFull(kind, extID)
	if err != nil {
		return "", "", nil
	}
	add := func(k, v string) {
		if v != "" {
			fields = append(fields, WatchField{K: k, V: v})
		}
	}
	add("Duración", m.Runtime)
	add("Géneros", strings.Join(m.Genres, ", "))
	add("IMDb", m.ImdbRating)
	add("Dirección", strings.Join(m.Director, ", "))
	return m.Description, p, fields
}

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

	return strings.Replace(res.Results[0].ArtworkURL100, "100x100bb", "600x600bb", 1), nil
}

func bookQuery(author, title string) string {
	if i := strings.LastIndex(title, " - "); i >= 0 {
		title = title[i+3:]
	}
	return strings.TrimSpace(author + " " + title)
}

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

type SearchHit struct {
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Year   string `json:"year"`
	Poster string `json:"poster"`
	ExtID  string `json:"ext_id"`
}

type EANHit struct {
	Title  string `json:"title"`
	Poster string `json:"poster"`
	Kind   string `json:"kind"`
	Found  bool   `json:"found"`
}

func lookupEAN(code string) EANHit {
	code = strings.TrimSpace(code)
	if code == "" {
		return EANHit{}
	}
	if strings.HasPrefix(code, "978") || strings.HasPrefix(code, "979") {
		if h, ok := openLibraryByISBN(code); ok {
			return h
		}
	}
	return upcItemDB(code)
}

func openLibraryByISBN(isbn string) (EANHit, bool) {
	var res struct {
		Title  string `json:"title"`
		Covers []int  `json:"covers"`
	}
	u := "https://openlibrary.org/isbn/" + url.QueryEscape(isbn) + ".json"
	if err := getJSON(u, &res); err != nil || res.Title == "" {
		return EANHit{}, false
	}
	poster := ""
	if len(res.Covers) > 0 && res.Covers[0] > 0 {
		poster = fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-L.jpg", res.Covers[0])
	}
	return EANHit{Title: res.Title, Poster: poster, Kind: "book", Found: true}, true
}

func upcItemDB(code string) EANHit {
	var res struct {
		Items []struct {
			Title  string   `json:"title"`
			Images []string `json:"images"`
		} `json:"items"`
	}
	u := "https://api.upcitemdb.com/prod/trial/lookup?upc=" + url.QueryEscape(code)
	if err := getJSON(u, &res); err != nil || len(res.Items) == 0 {
		return EANHit{}
	}
	it := res.Items[0]
	poster := ""
	if len(it.Images) > 0 {
		poster = it.Images[0]
	}
	kind := guessKind(it.Title)
	title := it.Title
	if kind == "movie" || kind == "series" {
		title = cleanEANTitle(title)
	}
	return EANHit{Title: title, Poster: poster, Kind: kind, Found: it.Title != ""}
}

var reEANFormat = regexp.MustCompile(`(?i)\s*[\(\[](blu-?ray|dvd|4k|uhd|ultra hd|steelbook|combo|digital|region [a-z0-9]+)[^\)\]]*[\)\]]`)

func cleanEANTitle(t string) string {
	t = reEANFormat.ReplaceAllString(t, "")
	return strings.TrimSpace(t)
}

func guessKind(title string) string {
	t := strings.ToLower(title)
	switch {
	case strings.Contains(t, "blu-ray") || strings.Contains(t, "blu ray") || strings.Contains(t, "dvd") || strings.Contains(t, "4k") || strings.Contains(t, "uhd"):
		return "movie"
	case strings.Contains(t, "ps5") || strings.Contains(t, "ps4") || strings.Contains(t, "xbox") || strings.Contains(t, "nintendo") || strings.Contains(t, "switch"):
		return "game"
	case strings.Contains(t, "vinyl") || strings.Contains(t, "cd") || strings.Contains(t, "lp"):
		return "audio"
	}
	return "other"
}

func searchTitles(q string, kinds []string) []SearchHit {
	want := map[string]bool{}
	for _, k := range kinds {
		want[k] = true
	}
	if len(want) == 0 {
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
				ArtistName     string `json:"artistName"`
				CollectionName string `json:"collectionName"`
				ArtworkURL100  string `json:"artworkUrl100"`
				ReleaseDate    string `json:"releaseDate"`
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

type Progress struct {
	mu      sync.Mutex
	Running bool `json:"running"`
	stop    bool
	Total   int    `json:"total"`
	Done    int    `json:"done"`
	Found   int    `json:"found"`
	LastID  int64  `json:"last_id"`
	Phase   string `json:"phase"`
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
		setImdbID(db, libraryKey, it.ID, extID)
	}

	if posterURL == "" && idOverride == "" {
		media := map[string]string{"video": "movie", "audio": "music", "book": "ebook"}[it.Kind]
		term := it.Title
		if it.Album != "" {
			term = it.Album
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

		if it.Kind == "file" {
			continue
		}
		if pendingOnly && it.EnrichState != "pending" {
			continue
		}
		if it.HasCover && !force {

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

	if pendingOnly && !stopped {
		_, _ = db.Exec(`UPDATE items SET enrich_state='not_found' WHERE library_key=? AND scan_state='present' AND enrich_state='pending'`, libraryKey)
	}
	return found, nil
}

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
