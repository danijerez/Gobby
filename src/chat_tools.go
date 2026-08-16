package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
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
	str := map[string]any{"type": "string"}
	return []any{
		tool("search_media", "Search the whole library (movies, series, music, books) by a text query.",
			map[string]any{"query": str}, []string{"query"}),
		tool("browse_library", "List items in a section without a query. kind is one of: movie, series, music, book.",
			map[string]any{"kind": str}, []string{"kind"}),
		tool("list_watchlist", "List the user's watch/read/listen-later list.",
			map[string]any{}, nil),
		tool("recent_media", "What was recently added and what the user is in the middle of watching.",
			map[string]any{}, nil),
		tool("library_info", "Summary of the library: scanned folder, total items, which sections have content.",
			map[string]any{}, nil),
	}
}

func runChatTool(db *sql.DB, libraryKey, uiBase, name, argsJSON string) string {
	var args map[string]any
	_ = json.Unmarshal([]byte(argsJSON), &args)
	getStr := func(k string) string {
		if v, ok := args[k].(string); ok {
			return v
		}
		return ""
	}
	jsonOut := func(v any, err error) string {
		if err != nil {
			return `{"error":` + strconv.Quote(err.Error()) + `}`
		}
		b, _ := json.Marshal(v)
		return string(b)
	}

	switch name {
	case "search_media":
		res, err := searchAll(db, libraryKey, getStr("query"), 20)
		return jsonOut(res, err)
	case "browse_library":
		kind := getStr("kind")
		if kind == "" {
			kind = "movie"
		}
		bv, err := browseView(db, libraryKey, kind, "", Filters{}, 1)
		return jsonOut(bv, err)
	case "list_watchlist":
		items, err := listWatch(db)
		return jsonOut(items, err)
	case "recent_media":
		cont, _ := homeShelf(db, libraryKey, "last_opened>0 AND kind<>'file'", "last_opened DESC", 10)
		added, err := homeShelf(db, libraryKey, "kind<>'file'", "modtime DESC, added_at DESC, id DESC", 12)
		return jsonOut(map[string]any{"continue": cont, "added": added}, err)
	case "library_info":
		var total int
		db.QueryRow(`SELECT COUNT(*) FROM items WHERE library_key=? AND scan_state='present'`, libraryKey).Scan(&total)
		return jsonOut(map[string]any{"total": total, "sections": nonEmptySections(db, libraryKey)}, nil)
	}
	return fmt.Sprintf(`{"error":"unknown tool %q"}`, name)
}
