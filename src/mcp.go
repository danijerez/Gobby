package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func mcpHandler(db *sql.DB, version, root, libraryKey, uiBase string) http.Handler {
	s := mcp.NewServer(&mcp.Implementation{Name: "gobby", Version: version}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "search_media",
		Description: "Search the whole library at once (movies, series, music, books, files). Series and albums collapse to a single result. Returns id, title, section, year, and (for series/albums) the collection name.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchInput) (*mcp.CallToolResult, any, error) {
		res, err := searchAll(db, libraryKey, in.Query, 60)
		return jsonResult(res, err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_media",
		Description: "Full detail for one catalog item by id: synopsis, cast, genres, rating, plus stream_path/download_path (relative URLs — join with the server's base URL to play or download it remotely).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in playInput) (*mcp.CallToolResult, any, error) {
		it, err := getItem(db, libraryKey, in.ID)
		if err != nil {
			return jsonResult(nil, fmt.Errorf("item %d not found", in.ID))
		}
		out := map[string]any{
			"item":          it,
			"stream_path":   fmt.Sprintf("/api/item/%d/stream", it.ID),
			"download_path": fmt.Sprintf("/api/item/%d/stream?dl=1", it.ID),
		}
		return jsonResult(out, nil)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "search_titles",
		Description: "Search free providers (Cinemeta for movies/series, Open Library for books, iTunes for music) for a title to add to the watchlist. Returns candidates with title, year and poster. Use this first, then pass the chosen one to add_to_watchlist so it gets a cover and year.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in titleSearchInput) (*mcp.CallToolResult, any, error) {
		var kinds []string
		if in.Kind != "" {
			kinds = []string{in.Kind}
		}
		return jsonResult(searchTitles(in.Query, kinds), nil)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_to_watchlist",
		Description: "Add something to watch/listen/read later. Pass poster and year (from search_titles) when you have them so the entry shows a cover.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in watchInput) (*mcp.CallToolResult, any, error) {
		err := addWatch(db, in.Kind, in.Title, in.Note, in.Poster, in.Year)
		return jsonResult(map[string]string{"status": "ok"}, err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_watchlist",
		Description: "List the watchlist — everything the user noted to watch/listen/read later, pending first.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		items, err := listWatch(db)
		return jsonResult(items, err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "remove_from_watchlist",
		Description: "Remove a watchlist entry by its id (from list_watchlist).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in idInput) (*mcp.CallToolResult, any, error) {
		if err := deleteWatch(db, in.ID); err != nil {
			return jsonResult(nil, err)
		}
		return jsonResult(map[string]string{"status": "ok"}, nil)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_watchlist_done",
		Description: "Mark a watchlist entry as done/watched (or pending again) by its id.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in watchDoneInput) (*mcp.CallToolResult, any, error) {
		if err := setWatchDone(db, in.ID, in.Done); err != nil {
			return jsonResult(nil, err)
		}
		return jsonResult(map[string]string{"status": "ok"}, nil)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "library_info",
		Description: "What Gobby is serving: scanned folder, total item count, and which sections (movies/series/music/books/files) currently hold content.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		var total int
		db.QueryRow(`SELECT COUNT(*) FROM items WHERE library_key=? AND scan_state='present'`, libraryKey).Scan(&total)
		return jsonResult(map[string]any{
			"root":     root,
			"total":    total,
			"sections": nonEmptySections(db, libraryKey),
		}, nil)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "browse_library",
		Description: "List catalog items by section without a search query: kind is one of movie, series, music, book, files. Series/albums come back grouped. Use this to answer 'what series do I have', 'list my movies', etc. Optional page (60 per page).",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in browseInput) (*mcp.CallToolResult, any, error) {
		kind := in.Kind
		if kind == "" {
			kind = "movie"
		}
		bv, err := browseView(db, libraryKey, kind, in.Query, Filters{}, in.Page)
		return jsonResult(bv, err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "recent_media",
		Description: "The Home shelves: recently added items and the 'continue watching' list (things with saved playback progress). Good for 'what did I add lately' or 'what was I watching'.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		cont, _ := homeShelf(db, libraryKey, "last_opened>0 AND kind<>'file'", "last_opened DESC", 12)
		added, err := homeShelf(db, libraryKey, "kind<>'file'", "modtime DESC, added_at DESC, id DESC", 18)
		return jsonResult(map[string]any{"continue": cont, "added": added}, err)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_media",
		Description: "Edit a catalog item's user fields: title, notes, rating (0-5), year, artist, album, genre. Only the fields you pass are changed. Pass the item id from search_media.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in updateInput) (*mcp.CallToolResult, any, error) {
		cur, err := getItem(db, libraryKey, in.ID)
		if err != nil {
			return jsonResult(nil, fmt.Errorf("item %d not found", in.ID))
		}
		edit := ItemEdit{Title: cur.Title, Notes: cur.Notes, Rating: cur.Rating, Year: cur.Year, Artist: cur.Artist, Album: cur.Album, Genre: cur.Genre}
		if in.Title != nil {
			edit.Title = *in.Title
		}
		if in.Notes != nil {
			edit.Notes = *in.Notes
		}
		if in.Rating != nil {
			edit.Rating = *in.Rating
		}
		if in.Year != nil {
			edit.Year = *in.Year
		}
		if in.Artist != nil {
			edit.Artist = *in.Artist
		}
		if in.Album != nil {
			edit.Album = *in.Album
		}
		if in.Genre != nil {
			edit.Genre = *in.Genre
		}
		if err := updateItem(db, libraryKey, in.ID, edit); err != nil {
			return jsonResult(nil, err)
		}
		it, _ := getItem(db, libraryKey, in.ID)
		return jsonResult(it, nil)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_media_section",
		Description: "Reclassify an item into a section: movie, series, music, book, files — or 'auto' to restore the automatic guess. Pass the item id from search_media.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in sectionInput) (*mcp.CallToolResult, any, error) {
		if err := setSection(db, libraryKey, in.ID, in.Section); err != nil {
			return jsonResult(nil, err)
		}
		return jsonResult(map[string]string{"status": "ok"}, nil)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_media_progress",
		Description: "Set playback progress (seconds watched) for an item, powering resume + 'continue watching'. Pass seconds=0 to mark it unwatched, or a large value / the runtime to mark it watched. Item id from search_media.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in progressInput) (*mcp.CallToolResult, any, error) {
		if err := setProgress(db, libraryKey, in.ID, in.Seconds); err != nil {
			return jsonResult(nil, err)
		}
		return jsonResult(map[string]string{"status": "ok"}, nil)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "enrich_media",
		Description: "Fetch cover art and metadata (synopsis, cast, genres) for one item from the free providers. Optional imdb_id (tt…) forces a specific match. Pass the item id from search_media.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in enrichInput) (*mcp.CallToolResult, any, error) {
		if in.ImdbID != "" {
			setImdbID(db, libraryKey, in.ID, in.ImdbID)
		}
		ok, err := enrichOne(db, libraryKey, in.ID, in.ImdbID)
		if err != nil {
			return jsonResult(nil, err)
		}
		it, _ := getItem(db, libraryKey, in.ID)
		return jsonResult(map[string]any{"found": ok, "item": it}, nil)
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:        "play_media",
		Description: "Open a catalog item in the default player (e.g. VLC) ON THE MACHINE RUNNING GOBBY, and return web_url — the link to play it in the Gobby web UI on any device (share this with the user so they can watch on their own screen). Pass the item id from search_media.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in playInput) (*mcp.CallToolResult, any, error) {
		it, err := getItem(db, libraryKey, in.ID)
		if err != nil {
			return jsonResult(nil, fmt.Errorf("item %d not found", in.ID))
		}
		path := filepath.Join(root, filepath.FromSlash(it.RelPath))
		out := map[string]any{"title": it.Title, "path": path, "web_url": uiBase + "/#item/" + fmt.Sprint(it.ID)}
		if err := openInPlayer(path); err != nil {

			out["host_player"] = "unavailable: " + err.Error()
		} else {
			out["host_player"] = "opened"
		}
		return jsonResult(out, nil)
	})

	return mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil)
}

func openInPlayer(path string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("cmd", "/c", "start", "", path).Start()
	case "darwin":
		return exec.Command("open", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

func revealInExplorer(path string, isDir bool) error {
	switch runtime.GOOS {
	case "windows":
		if isDir {
			return exec.Command("explorer", path).Start()
		}
		return exec.Command("explorer", "/select,", path).Start()
	case "darwin":
		if isDir {
			return exec.Command("open", path).Start()
		}
		return exec.Command("open", "-R", path).Start()
	default:
		dir := path
		if !isDir {
			dir = filepath.Dir(path)
		}
		return exec.Command("xdg-open", dir).Start()
	}
}

type searchInput struct {
	Query string `json:"query" jsonschema:"substring to match against title, artist or album across the whole library"`
}

type watchInput struct {
	Kind   string `json:"kind,omitempty" jsonschema:"movie, series, book, audio, game or other"`
	Title  string `json:"title" jsonschema:"what to watch/listen/read"`
	Note   string `json:"note,omitempty" jsonschema:"optional note"`
	Poster string `json:"poster,omitempty" jsonschema:"cover image URL (from search_titles)"`
	Year   string `json:"year,omitempty" jsonschema:"release year (from search_titles)"`
}

type titleSearchInput struct {
	Query string `json:"query" jsonschema:"title to look up"`
	Kind  string `json:"kind,omitempty" jsonschema:"limit to one of: movie, series, book, audio, game"`
}

type idInput struct {
	ID int64 `json:"id" jsonschema:"watchlist entry id (from list_watchlist)"`
}

type watchDoneInput struct {
	ID   int64 `json:"id" jsonschema:"watchlist entry id (from list_watchlist)"`
	Done bool  `json:"done" jsonschema:"true = watched/done, false = pending"`
}

type playInput struct {
	ID int64 `json:"id" jsonschema:"catalog item id (from search_media)"`
}

type browseInput struct {
	Kind  string `json:"kind,omitempty" jsonschema:"movie, series, music, book or files"`
	Query string `json:"query,omitempty" jsonschema:"optional title filter within the section"`
	Page  int    `json:"page,omitempty" jsonschema:"1-based page (60 loose items per page)"`
}

type updateInput struct {
	ID     int64   `json:"id" jsonschema:"catalog item id (from search_media)"`
	Title  *string `json:"title,omitempty"`
	Notes  *string `json:"notes,omitempty"`
	Rating *int    `json:"rating,omitempty" jsonschema:"0-5 personal rating"`
	Year   *int    `json:"year,omitempty"`
	Artist *string `json:"artist,omitempty" jsonschema:"artist / director / author"`
	Album  *string `json:"album,omitempty" jsonschema:"album / series / collection"`
	Genre  *string `json:"genre,omitempty"`
}

type sectionInput struct {
	ID      int64  `json:"id" jsonschema:"catalog item id (from search_media)"`
	Section string `json:"section" jsonschema:"movie, series, music, book, files or auto"`
}

type progressInput struct {
	ID      int64 `json:"id" jsonschema:"catalog item id (from search_media)"`
	Seconds int   `json:"seconds" jsonschema:"playback position in seconds (0 = unwatched)"`
}

type enrichInput struct {
	ID     int64  `json:"id" jsonschema:"catalog item id (from search_media)"`
	ImdbID string `json:"imdb_id,omitempty" jsonschema:"optional tt… id to force a specific match"`
}

func jsonResult(payload any, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil, nil
	}
	text, _ := json.Marshal(payload)

	structured := map[string]any{"result": payload}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(text)}},
	}, structured, nil
}
