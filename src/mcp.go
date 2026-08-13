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

// mcpHandler builds the MCP server (HTTP transport) exposing the catalog as
// tools an AI can call. Unlike goblin's stdio subprocess, this rides the same
// mux/port as the web UI — one process, one URL.
func mcpHandler(db *sql.DB, version, root, libraryKey string) http.Handler {
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
		Name:        "add_to_watchlist",
		Description: "Add something to watch/listen/read later.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in watchInput) (*mcp.CallToolResult, any, error) {
		err := addWatch(db, in.Kind, in.Title, in.Note, "", "")
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
		Name:        "play_media",
		Description: "Play a catalog item in the system's default player (e.g. VLC) on the machine running Gobby. Pass the item id from search_media.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in playInput) (*mcp.CallToolResult, any, error) {
		it, err := getItem(db, libraryKey, in.ID)
		if err != nil {
			return jsonResult(nil, fmt.Errorf("item %d not found", in.ID))
		}
		path := filepath.Join(root, filepath.FromSlash(it.RelPath))
		if err := openInPlayer(path); err != nil {
			return jsonResult(nil, err)
		}
		return jsonResult(map[string]string{"status": "playing", "title": it.Title, "path": path}, nil)
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

type searchInput struct {
	Query string `json:"query" jsonschema:"substring to match against title, artist or album across the whole library"`
}

type watchInput struct {
	Kind  string `json:"kind,omitempty" jsonschema:"video, audio, book or other"`
	Title string `json:"title" jsonschema:"what to watch/listen/read"`
	Note  string `json:"note,omitempty" jsonschema:"optional note"`
}

type playInput struct {
	ID int64 `json:"id" jsonschema:"catalog item id (from search_media)"`
}

func jsonResult(payload any, err error) (*mcp.CallToolResult, any, error) {
	if err != nil {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
		}, nil, nil
	}
	text, _ := json.Marshal(payload)
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(text)}},
	}, payload, nil
}
