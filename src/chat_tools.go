package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
)

func chatTools() []any {
	tool := func(name, desc string, props map[string]any, required []string) any {
		if required == nil {
			required = []string{}
		}
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        name,
				"description": desc,
				"parameters": map[string]any{
					"type":       "object",
					"properties": props,
					"required":   required,
				},
			},
		}
	}
	pStr := func(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
	pInt := func(desc string) map[string]any { return map[string]any{"type": "integer", "description": desc} }
	pEnum := func(desc string, vals ...string) map[string]any {
		return map[string]any{"type": "string", "description": desc, "enum": vals}
	}
	return []any{
		tool("search_media", "Search the whole library (movies, series, music, books, photos) by a text query.",
			map[string]any{"query": pStr("text to match against title, artist or album")}, []string{"query"}),
		tool("browse_library", "List items in a section without a query.",
			map[string]any{"kind": pEnum("which section to list", "movie", "series", "music", "book", "photos")}, []string{"kind"}),
		tool("list_episodes", "List every episode of one series (or every track of one album) by its name, with each episode's id, season and episode number. Use this to open a specific episode like 'season 2 episode 3'.",
			map[string]any{"name": pStr("the series or album name, e.g. from search_media")}, []string{"name"}),
		tool("list_watchlist", "List the user's watch/read/listen-later list.",
			map[string]any{}, nil),
		tool("add_to_watchlist", "Add a title the user wants to watch/read/listen later.",
			map[string]any{
				"title": pStr("what to watch/read/listen"),
				"kind":  pEnum("category", "movie", "series", "book", "audio", "other"),
				"note":  pStr("optional note"),
			}, []string{"title"}),
		tool("recent_media", "What was recently added and what the user is in the middle of watching.",
			map[string]any{}, nil),
		tool("library_info", "Summary of the library: scanned folder, total items, which sections have content.",
			map[string]any{}, nil),
		tool("open_media", "Open/play an item for the user in the UI. Use after finding it via search_media. This actually starts playback or opens the reader.",
			map[string]any{"id": pInt("the item's numeric id from search_media")}, []string{"id"}),
	}
}

func runChatTool(db *sql.DB, libraryKey, uiBase, name, argsJSON string) (string, int64) {
	var args map[string]any
	_ = json.Unmarshal([]byte(argsJSON), &args)
	getStr := func(k string) string {
		if v, ok := args[k].(string); ok {
			return v
		}
		if v, ok := args[k].(float64); ok {
			return strconv.FormatInt(int64(v), 10)
		}
		return ""
	}
	jsonOut := func(v any, err error) string {
		if err != nil {
			return `{"error":` + strconv.Quote("tool failed: "+err.Error()) + `}`
		}
		b, _ := json.Marshal(v)
		return string(b)
	}
	out := func(s string) (string, int64) { return s, 0 }

	switch name {
	case "open_media":
		id, _ := strconv.ParseInt(getStr("id"), 10, 64)
		var title string
		err := db.QueryRow(`SELECT title FROM items WHERE id=? AND library_key=? AND scan_state='present'`, id, libraryKey).Scan(&title)
		if err != nil || id == 0 {
			return out(`{"error":"item not found, search first to get a valid id"}`)
		}
		return jsonOut(map[string]any{"opened": true, "title": title}, nil), id
	case "search_media":
		res, err := searchAll(db, libraryKey, getStr("query"), 0)
		if err != nil {
			return out(jsonOut(nil, err))
		}
		total := len(res)
		truncated := false
		if total > 20 {
			res = res[:20]
			truncated = true
		}
		return out(jsonOut(map[string]any{"results": res, "total_matches": total, "truncated": truncated}, nil))
	case "browse_library":
		kind := getStr("kind")
		if kind == "" {
			kind = "movie"
		}
		bv, err := browseView(db, libraryKey, kind, "", Filters{}, 1)
		return out(jsonOut(bv, err))
	case "list_episodes":
		name := getStr("name")
		var eps []map[string]any
		for _, section := range []string{"series", "music"} {
			bv, err := browseView(db, libraryKey, section, name, Filters{}, 1)
			if err != nil {
				continue
			}
			for _, g := range bv.Groups {
				for _, it := range g.Items {
					eps = append(eps, map[string]any{
						"id": it.ID, "title": it.Title, "season": it.Season, "episode": it.Episode,
						"series": g.Name, "file": filepath.Base(it.RelPath),
					})
				}
			}
			if len(eps) > 0 {
				break
			}
		}
		if len(eps) == 0 {
			return out(`{"error":"no episodes found for that name"}`)
		}
		return out(jsonOut(map[string]any{"episodes": eps}, nil))
	case "list_watchlist":
		items, err := listWatch(db)
		return out(jsonOut(items, err))
	case "add_to_watchlist":
		_, err := addWatch(db, getStr("kind"), getStr("title"), getStr("note"), "", "", "")
		return out(jsonOut(map[string]string{"status": "added"}, err))
	case "recent_media":
		cont, _ := homeShelf(db, libraryKey, "last_opened>0 AND kind<>'file'", "last_opened DESC", 10)
		added, err := homeShelf(db, libraryKey, "kind<>'file'", "modtime DESC, added_at DESC, id DESC", 12)
		return out(jsonOut(map[string]any{"continue": cont, "added": added}, err))
	case "library_info":
		var total int
		db.QueryRow(`SELECT COUNT(*) FROM items WHERE library_key=? AND scan_state='present'`, libraryKey).Scan(&total)
		return out(jsonOut(map[string]any{"total": total, "sections": nonEmptySections(db, libraryKey)}, nil))
	}
	return out(fmt.Sprintf(`{"error":"unknown tool %q"}`, name))
}
