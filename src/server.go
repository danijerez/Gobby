package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"rsc.io/qr"
)

//go:embed all:web
var webFS embed.FS

// lib holds the currently-scanned folder + its library key. Mutable at runtime
// (the user can switch folders live), so every read goes through the mutex.
type lib struct {
	mu   sync.RWMutex
	root string
	key  string
}

func (l *lib) Root() string { l.mu.RLock(); defer l.mu.RUnlock(); return l.root }
func (l *lib) Key() string  { l.mu.RLock(); defer l.mu.RUnlock(); return l.key }
func (l *lib) Set(root, key string) {
	l.mu.Lock()
	l.root, l.key = root, key
	l.mu.Unlock()
}

// Client is one device seen talking to the server (by IP).
type Client struct {
	IP       string `json:"ip"`
	Agent    string `json:"agent"`
	Requests int    `json:"requests"`
	FirstAt  int64  `json:"first_at"`
	LastAt   int64  `json:"last_at"`
	Self     bool   `json:"self"` // the machine running Gobby (localhost)
}

// clientTracker remembers which devices have connected, for the "connected
// devices" panel. In-memory only (resets on restart) — this is presence info,
// not something worth persisting.
type clientTracker struct {
	mu sync.Mutex
	m  map[string]*Client
}

func (t *clientTracker) note(r *http.Request) {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	now := time.Now().Unix()
	t.mu.Lock()
	c := t.m[ip]
	if c == nil {
		c = &Client{IP: ip, FirstAt: now, Self: isLoopback(ip)}
		t.m[ip] = c
	}
	c.Agent = r.UserAgent()
	c.Requests++
	c.LastAt = now
	t.mu.Unlock()
}

func (t *clientTracker) list() []Client {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Client, 0, len(t.m))
	for _, c := range t.m {
		out = append(out, *c)
	}
	return out
}

func isLoopback(ip string) bool {
	return ip == "127.0.0.1" || ip == "::1" || strings.HasPrefix(ip, "127.")
}

// requestIsOwner reports whether a request may edit. Everyone on the local
// network (and the host itself) can edit; only requests arriving through the
// public Cloudflare tunnel are read-only guests. The tunnel is detected by the
// headers Cloudflare injects and its trycloudflare.com Host — local requests
// never carry those.
func requestIsOwner(r *http.Request) bool {
	return !viaPublicTunnel(r)
}

func viaPublicTunnel(r *http.Request) bool {
	if r.Header.Get("Cf-Ray") != "" || r.Header.Get("Cf-Connecting-Ip") != "" {
		return true
	}
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return strings.HasSuffix(strings.ToLower(host), ".trycloudflare.com")
}

// isWrite reports whether a request mutates state. Read verbs and one harmless
// exception (marking an item opened, which powers a guest's "continue" shelf)
// are allowed for everyone; everything else is owner-only.
func isWrite(method, path string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
	default:
		return false
	}
	if strings.HasSuffix(path, "/opened") {
		return false // inocuous: lets guests get a "continue watching" shelf too
	}
	return true
}

// looksLikeImage sniffs the magic bytes of the common cover formats.
func looksLikeImage(b []byte) bool {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF: // JPEG
		return true
	case len(b) >= 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n": // PNG
		return true
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP": // WebP
		return true
	case len(b) >= 6 && (string(b[:6]) == "GIF87a" || string(b[:6]) == "GIF89a"): // GIF
		return true
	}
	return false
}

// baseURL is the address a client should use to reach Gobby. Behind the public
// tunnel (or any reverse proxy) we honor the forwarded scheme/host verbatim, so
// the URL stays https://…trycloudflare.com. On the LAN we rewrite the host to the
// machine's LAN IP so phones don't get "localhost".
func baseURL(r *http.Request) string {
	if viaPublicTunnel(r) || r.Header.Get("X-Forwarded-Host") != "" {
		scheme := r.Header.Get("X-Forwarded-Proto")
		if scheme == "" {
			scheme = "https" // Cloudflare always terminates TLS
		}
		host := r.Header.Get("X-Forwarded-Host")
		if host == "" {
			host = r.Host
		}
		return scheme + "://" + host
	}
	base := "http://" + r.Host
	if ip := lanIP(); ip != "" {
		if _, port, _ := net.SplitHostPort(r.Host); port != "" {
			base = "http://" + net.JoinHostPort(ip, port)
		}
	}
	return base
}

// serve mounts the UI, JSON API and MCP handler on one mux/port.
func serve(ctx context.Context, db *sql.DB, addr, version, dbPath string, lb *lib) error {
	mux := http.NewServeMux()
	clients := &clientTracker{m: map[string]*Client{}}
	binDir := filepath.Dir(dbPath) // where auto-downloaded binaries (cloudflared, ffmpeg) live

	// What Gobby is reading and how much it found — shown in the UI header.
	// `sections` lists only the tabs that currently hold something, so the UI
	// can hide empty ones.
	mux.HandleFunc("GET /api/info", func(w http.ResponseWriter, r *http.Request) {
		var total int
		db.QueryRow(`SELECT COUNT(*) FROM items WHERE library_key=? AND scan_state='present'`, lb.Key()).Scan(&total)
		abs, _ := filepath.Abs(lb.Root())
		writeJSON(w, map[string]any{"root": abs, "total": total, "base": baseURL(r), "sections": nonEmptySections(db, lb.Key()), "owner": requestIsOwner(r), "version": version}, nil)
	})

	mux.HandleFunc("GET /api/scan/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, scanProgress.snapshot(), nil)
	})

	// Global library search across every kind — powers the header search.
	// (/api/search is the online watchlist picker; this one is the local catalogue.)
	mux.HandleFunc("GET /api/library/search", func(w http.ResponseWriter, r *http.Request) {
		res, err := searchAll(db, lb.Key(), r.URL.Query().Get("q"), 60)
		writeJSON(w, res, err)
	})

	// Any kind split into folder groups (series/album/author) + loose items.
	mux.HandleFunc("GET /api/browse", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		atoi := func(s string) int { n, _ := strconv.Atoi(s); return n }
		f := Filters{
			Ext:       q.Get("ext"),
			YearMin:   atoi(q.Get("year_min")),
			YearMax:   atoi(q.Get("year_max")),
			Cover:     q.Get("cover"),
			RatingMin: atoi(q.Get("rating_min")),
			Genre:     q.Get("genre"),
		}
		bv, err := browseView(db, lb.Key(), q.Get("kind"), q.Get("q"), f, atoi(q.Get("page")))
		writeJSON(w, bv, err)
	})

	mux.HandleFunc("GET /api/item/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		it, err := getItem(db, lb.Key(), id)
		writeJSON(w, it, err)
	})

	// Whole-library size tree (all kinds) for the treeview + treemap.
	mux.HandleFunc("GET /api/tree", func(w http.ResponseWriter, r *http.Request) {
		tree, err := fullTree(db, lb.Key())
		writeJSON(w, tree, err)
	})

	// Explicit "I opened this" signal from the UI (play/share button), powering
	// the Home "continue watching" shelf.
	mux.HandleFunc("POST /api/item/{id}/opened", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		writeJSON(w, map[string]string{"status": "ok"}, markOpened(db, lb.Key(), id))
	})

	// Home shelves: recently opened ("continue") + recently added ("new").
	// Generic files (kind='file') are excluded — Home is about your media, not
	// the random zips/isos catalogued in the Files tab.
	mux.HandleFunc("GET /api/home", func(w http.ResponseWriter, r *http.Request) {
		cont, _ := homeShelf(db, lb.Key(), "last_opened>0 AND kind<>'file'", "last_opened DESC", 12)
		// Sort by the file's own modtime, not the insert instant: a whole-library
		// first scan stamps every row with the same added_at, so added_at DESC would
		// degrade to id DESC and pin the same few titles forever. modtime reflects
		// what's genuinely new on disk.
		added, err := homeShelf(db, lb.Key(), "kind<>'file'", "modtime DESC, added_at DESC, id DESC", 18)
		writeJSON(w, map[string]any{"continue": cont, "added": added}, err)
	})

	// Stream the actual media file (supports range requests → seek/play in browser).
	// ?dl=1 forces a download instead of inline playback.
	mux.HandleFunc("GET /api/item/{id}/stream", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		it, err := getItem(db, lb.Key(), id)
		if err != nil || it.State != "present" {
			http.NotFound(w, r)
			return
		}
		root := lb.Root()
		full := filepath.Join(root, filepath.FromSlash(it.RelPath))
		// Guard against path traversal: resolved file must stay under root.
		if rel, err := filepath.Rel(root, full); err != nil || strings.HasPrefix(rel, "..") {
			http.Error(w, "forbidden", 403)
			return
		}
		if r.URL.Query().Get("dl") == "1" {
			// Download always gets the original file untouched, never the remux.
			w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(full)+"\"")
			http.ServeFile(w, r, full)
			return
		}
		// mkv/avi don't play in browsers — remux to fragmented mp4 on the fly
		// (video copied, audio→aac). Everything else is browser-native: serve as-is
		// with Range support so seeking works.
		if needsRemux(full) {
			// ?t=<seconds> seeks: ffmpeg restarts the remux from there. The piped
			// fragmented mp4 has no byte-range seek, so the UI drives it with ?t=.
			startSec, _ := strconv.ParseFloat(r.URL.Query().Get("t"), 64)
			audioIdx, _ := strconv.Atoi(r.URL.Query().Get("audio")) // 0 = default track
			if err := streamRemux(r.Context(), w, binDir, full, startSec, audioIdx); err != nil && !errors.Is(err, errStreamStarted) {
				http.Error(w, "no se pudo reproducir: "+err.Error(), 500)
			}
			return
		}
		// NOTE: opens are recorded via POST /api/item/{id}/opened (an explicit user
		// action), NOT here — browsers prefetch/preload stream links, which would
		// otherwise fill "continue watching" with things never actually played.
		http.ServeFile(w, r, full)
	})

	// Playback metadata for the remux player: duration (for the seek bar — the piped
	// fragmented mp4 reports 0) plus selectable audio + subtitle tracks.
	mux.HandleFunc("GET /api/item/{id}/tracks", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		it, err := getItem(db, lb.Key(), id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		full := filepath.Join(lb.Root(), filepath.FromSlash(it.RelPath))
		_, _, dur := probeMedia(r.Context(), binDir, full)
		tr := probeTracks(r.Context(), binDir, full)
		writeJSON(w, map[string]any{"seconds": dur, "audio": tr.Audio, "subs": tr.Subs}, nil)
	})

	// One subtitle track as WebVTT, loaded by the player's <track> element.
	mux.HandleFunc("GET /api/item/{id}/sub", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		it, err := getItem(db, lb.Key(), id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		full := filepath.Join(lb.Root(), filepath.FromSlash(it.RelPath))
		idx, _ := strconv.Atoi(r.URL.Query().Get("track"))
		if err := streamSubtitle(r.Context(), w, binDir, full, idx); err != nil {
			http.Error(w, err.Error(), 500)
		}
	})

	mux.HandleFunc("GET /api/item/{id}/cover", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		b, err := coverOf(db, lb.Key(), id)
		if err != nil || len(b) == 0 {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(b)
	})

	// Upload your own cover image for any item (works for every kind — books,
	// music, files — not just what a provider can find). Marks it as enriched so
	// automatic fetching won't overwrite it.
	mux.HandleFunc("POST /api/item/{id}/cover", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		img, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<20)) // 16 MB cap
		if err != nil || len(img) < 4 {
			http.Error(w, "imagen inválida o demasiado grande", 400)
			return
		}
		if !looksLikeImage(img) {
			http.Error(w, "no parece una imagen (JPEG/PNG/WebP/GIF)", 400)
			return
		}
		if err := setCover(db, lb.Key(), id, img); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		_ = setEnrichState(db, lb.Key(), id, "found")
		writeJSON(w, map[string]string{"status": "ok"}, nil)
	})

	mux.HandleFunc("PUT /api/item/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		var in ItemEdit
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		writeJSON(w, map[string]string{"status": "ok"}, updateItem(db, lb.Key(), id, in))
	})

	// A manual section is an explicit override; "auto" returns to the stable
	// classifier built from extension, embedded tags and path parsing.
	mux.HandleFunc("POST /api/item/{id}/section", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		var in struct {
			Section string `json:"section"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, map[string]string{"status": "ok"}, setSection(db, lb.Key(), id, in.Section))
	})

	mux.HandleFunc("GET /api/watchlist", func(w http.ResponseWriter, r *http.Request) {
		items, err := listWatch(db)
		writeJSON(w, items, err)
	})

	mux.HandleFunc("POST /api/watchlist", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Kind, Title, Note, Poster, Year string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if strings.TrimSpace(in.Title) == "" {
			http.Error(w, "title required", 400)
			return
		}
		writeJSON(w, map[string]string{"status": "ok"}, addWatch(db, in.Kind, in.Title, in.Note, in.Poster, in.Year))
	})

	mux.HandleFunc("PUT /api/watchlist/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		var in struct {
			Done   *bool         `json:"done"`
			Note   *string       `json:"note"`
			Poster *string       `json:"poster"`
			Fields *[]WatchField `json:"fields"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if in.Done != nil {
			if err := setWatchDone(db, id, *in.Done); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		if in.Note != nil {
			if err := setWatchNote(db, id, *in.Note); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		if in.Poster != nil {
			if err := setWatchPoster(db, id, *in.Poster); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		if in.Fields != nil {
			if err := setWatchFields(db, id, *in.Fields); err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		}
		writeJSON(w, map[string]string{"status": "ok"}, nil)
	})

	mux.HandleFunc("DELETE /api/watchlist/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		writeJSON(w, map[string]string{"status": "ok"}, deleteWatch(db, id))
	})

	// Online title search for the watchlist picker (movies/series via Cinemeta,
	// books via Open Library). Returns [] on empty query.
	mux.HandleFunc("GET /api/search", func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if q == "" {
			writeJSON(w, []SearchHit{}, nil)
			return
		}
		var kinds []string
		if k := r.URL.Query().Get("kinds"); k != "" {
			kinds = strings.Split(k, ",")
		}
		writeJSON(w, searchTitles(q, kinds), nil)
	})

	// Start a cover-fetch run in the background; UI polls /api/enrich/status.
	// ?force=1 re-fetches items that already have a cover.
	mux.HandleFunc("POST /api/enrich", func(w http.ResponseWriter, r *http.Request) {
		if enrichProgress.snapshot().Running {
			writeJSON(w, map[string]string{"status": "already-running"}, nil)
			return
		}
		kind := r.URL.Query().Get("kind")
		if kind == "" {
			kind = "video"
		}
		force := r.URL.Query().Get("force") == "1"
		go enrich(db, lb.Key(), kind, force, false)
		writeJSON(w, map[string]string{"status": "started"}, nil)
	})

	mux.HandleFunc("GET /api/enrich/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, enrichProgress.snapshot(), nil)
	})

	mux.HandleFunc("POST /api/enrich/stop", func(w http.ResponseWriter, r *http.Request) {
		enrichProgress.requestStop()
		writeJSON(w, map[string]string{"status": "stopping"}, nil)
	})

	// Re-fetch one item; optional {"imdb_id":"tt..."} forces a specific match.
	mux.HandleFunc("POST /api/item/{id}/refetch", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		var in struct {
			ImdbID string `json:"imdb_id"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		if in.ImdbID != "" {
			setImdbID(db, lb.Key(), id, in.ImdbID)
		}
		ok, err := enrichOne(db, lb.Key(), id, in.ImdbID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		it, _ := getItem(db, lb.Key(), id)
		writeJSON(w, map[string]any{"found": ok, "item": it}, nil)
	})

	// Switch the scanned folder live: repoint the library and kick a background
	// rescan. The new folder gets its own library_key, so its catalogue is kept
	// separate from the previous one (switch back later and it's still there).
	mux.HandleFunc("POST /api/library/folder", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		p := strings.TrimSpace(in.Path)
		fi, err := os.Stat(p)
		if err != nil || !fi.IsDir() {
			http.Error(w, "no es una carpeta válida", 400)
			return
		}
		abs, _ := filepath.Abs(p)
		lb.Set(abs, rootKey(abs))
		go func(root, key string) {
			if _, err := scan(db, root, key); err != nil {
				return
			}
			if n, _ := pendingEnrichmentCount(db, key); n > 0 {
				_, _ = enrich(db, key, "all", false, true)
			}
		}(abs, rootKey(abs))
		writeJSON(w, map[string]string{"status": "ok", "root": abs}, nil)
	})

	// Download the whole SQLite database (backup / move to another machine).
	mux.HandleFunc("GET /api/db/export", func(w http.ResponseWriter, r *http.Request) {
		f, err := os.Open(dbPath)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="gobby.db"`)
		io.Copy(w, f)
	})

	// Replace the database with an uploaded one, then rescan so the catalogue
	// matches the current folder. The old DB is kept as gobby.db.bak.
	mux.HandleFunc("POST /api/db/import", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 512<<20)) // 512 MB cap
		if err != nil {
			http.Error(w, "subida demasiado grande o inválida", 400)
			return
		}
		if len(body) < 16 || string(body[:15]) != "SQLite format 3" {
			http.Error(w, "no parece una base de datos Gobby", 400)
			return
		}
		if err := os.WriteFile(dbPath+".import", body, 0o644); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		// The live *sql.DB still points at the running file; signal the caller to
		// restart. Swapping an open SQLite handle mid-flight is unsafe, so we stage
		// the file and let the user restart Gobby to load it.
		writeJSON(w, map[string]string{"status": "staged", "note": "reinicia Gobby para cargar la base importada"}, nil)
	})

	// Temporary Cloudflare tunnel: expose Gobby to the public internet with no
	// account. The URL is public + unauthenticated (the UI warns before starting).
	tun := &tunnel{}
	_, port, _ := net.SplitHostPort(addr)
	mux.HandleFunc("POST /api/tunnel/start", func(w http.ResponseWriter, r *http.Request) {
		go tun.start(ctx, binDir, port) // may download cloudflared first; poll status
		writeJSON(w, map[string]string{"status": "starting"}, nil)
	})
	mux.HandleFunc("POST /api/tunnel/stop", func(w http.ResponseWriter, r *http.Request) {
		tun.stop()
		writeJSON(w, map[string]string{"status": "ok"}, nil)
	})
	mux.HandleFunc("GET /api/tunnel/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, tun.status(), nil)
	})
	go func() { <-ctx.Done(); tun.stop() }()

	// Self-update from GitHub Releases: check compares the running version to the
	// latest tag; apply downloads and overwrites the binary (+ffmpeg) in place. Both
	// are POST, so the owner-only write guard already keeps public guests out.
	mux.HandleFunc("GET /api/update/check", func(w http.ResponseWriter, r *http.Request) {
		cctx, cancel := context.WithTimeout(r.Context(), updateTimeout)
		defer cancel()
		writeJSON(w, checkForUpdate(cctx, version), nil)
	})
	mux.HandleFunc("POST /api/update/apply", func(w http.ResponseWriter, r *http.Request) {
		cctx, cancel := context.WithTimeout(r.Context(), updateTimeout)
		defer cancel()
		if err := applyUpdate(cctx, version); err != nil {
			writeJSON(w, nil, err)
			return
		}
		writeJSON(w, map[string]string{"status": "ok", "note": "actualizado — reinicia Gobby para usar la nueva versión"}, nil)
	})

	// QR code (PNG) encoding the LAN URL, so another device can scan to connect.
	mux.HandleFunc("GET /api/qr", func(w http.ResponseWriter, r *http.Request) {
		target := r.URL.Query().Get("url")
		if target == "" {
			target = baseURL(r)
		}
		code, err := qr.Encode(target, qr.M)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(code.PNG())
	})

	// Devices seen connecting to this server (presence, in-memory).
	mux.HandleFunc("GET /api/clients", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"base": baseURL(r), "clients": clients.list()}, nil)
	})

	mux.Handle("/mcp", mcpHandler(db, version, lb.Root(), lb.Key()))

	// Embedded UI, registered last so it doesn't shadow /api or /mcp.
	// no-cache so a browser never serves a stale UI after an upgrade.
	ui, err := fs.Sub(webFS, "web")
	if err != nil {
		return err
	}
	fileServer := http.FileServer(http.FS(ui))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// no-store (not just no-cache): the UI ships inside the binary and changes
		// with every build, so never let a browser reuse a stale app.js/index.html.
		w.Header().Set("Cache-Control", "no-store, must-revalidate")
		fileServer.ServeHTTP(w, r)
	}))

	// track every device that talks to us (skip the presence endpoints themselves
	// and the progress poll, which would otherwise dominate the request counts).
	tracked := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p != "/api/clients" && p != "/api/qr" && !strings.HasSuffix(p, "/status") {
			clients.note(r)
		}
		// Writes are allowed for the host and everyone on the local network; only
		// visitors coming through the public tunnel are read-only (browse + play,
		// no editing/deleting/rescanning).
		if isWrite(r.Method, p) && !requestIsOwner(r) {
			http.Error(w, "solo lectura: la edición no está disponible por el enlace público", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
	srv := &http.Server{Addr: addr, Handler: tracked, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		sc, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sc)
	}()

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, payload any, err error) {
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(payload)
}
