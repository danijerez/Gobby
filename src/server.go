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

type Client struct {
	IP       string `json:"ip"`
	Agent    string `json:"agent"`
	Requests int    `json:"requests"`
	FirstAt  int64  `json:"first_at"`
	LastAt   int64  `json:"last_at"`
	Self     bool   `json:"self"`
}

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

func requestIsLocal(r *http.Request) bool {
	if viaPublicTunnel(r) {
		return false
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}

	return isLoopback(ip) || isOwnIP(ip)
}

func isOwnIP(ip string) bool {
	target := net.ParseIP(ip)
	if target == nil {
		return false
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.Equal(target) {
			return true
		}
	}
	return false
}

func safeJoin(root, rel string) (string, error) {
	full := filepath.Join(root, filepath.FromSlash(rel))
	r, err := filepath.Rel(root, full)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", errors.New("ruta fuera de la biblioteca")
	}
	return full, nil
}

func requestIsOwner(r *http.Request) bool {
	return !viaPublicTunnel(r)
}

func tunnelKeyOK(r *http.Request, want string) bool {
	if r.URL.Query().Get("k") == want {
		return true
	}
	if c, err := r.Cookie("gobby_k"); err == nil && c.Value == want {
		return true
	}
	return false
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

func isWrite(method, path string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
	default:
		return false
	}
	if strings.HasSuffix(path, "/opened") {
		return false
	}
	return true
}

func looksLikeImage(b []byte) bool {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return true
	case len(b) >= 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n":
		return true
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return true
	case len(b) >= 6 && (string(b[:6]) == "GIF87a" || string(b[:6]) == "GIF89a"):
		return true
	}
	return false
}

func baseURL(r *http.Request) string {
	if viaPublicTunnel(r) || r.Header.Get("X-Forwarded-Host") != "" {
		scheme := r.Header.Get("X-Forwarded-Proto")
		if scheme == "" {
			scheme = "https"
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

func serve(ctx context.Context, db *sql.DB, addr, version, dbPath string, lb *lib) error {
	mux := http.NewServeMux()
	clients := &clientTracker{m: map[string]*Client{}}
	binDir := filepath.Dir(dbPath)

	mux.HandleFunc("GET /api/info", func(w http.ResponseWriter, r *http.Request) {
		var total int
		db.QueryRow(`SELECT COUNT(*) FROM items WHERE library_key=? AND scan_state='present'`, lb.Key()).Scan(&total)
		abs, _ := filepath.Abs(lb.Root())
		genres, yMin, yMax := facets(db, lb.Key())
		writeJSON(w, map[string]any{"root": abs, "total": total, "base": baseURL(r), "sections": nonEmptySections(db, lb.Key()), "owner": requestIsOwner(r), "local": requestIsLocal(r), "version": version, "genres": genres, "yearMin": yMin, "yearMax": yMax}, nil)
	})

	mux.HandleFunc("GET /api/scan/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, scanProgress.snapshot(), nil)
	})

	mux.HandleFunc("GET /api/library/search", func(w http.ResponseWriter, r *http.Request) {
		res, err := searchAll(db, lb.Key(), r.URL.Query().Get("q"), 60)
		writeJSON(w, res, err)
	})

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

	mux.HandleFunc("GET /api/tree", func(w http.ResponseWriter, r *http.Request) {
		tree, err := fullTree(db, lb.Key())
		writeJSON(w, tree, err)
	})

	mux.HandleFunc("POST /api/item/{id}/opened", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if t := r.URL.Query().Get("t"); t != "" {
			secs, _ := strconv.Atoi(t)
			_ = setProgress(db, lb.Key(), id, secs)
		}
		writeJSON(w, map[string]string{"status": "ok"}, markOpened(db, lb.Key(), id))
	})

	mux.HandleFunc("GET /api/home", func(w http.ResponseWriter, r *http.Request) {
		cont, _ := homeShelf(db, lb.Key(), "last_opened>0 AND kind<>'file'", "last_opened DESC", 12)

		added, err := homeShelf(db, lb.Key(), "kind<>'file'", "modtime DESC, added_at DESC, id DESC", 18)
		writeJSON(w, map[string]any{"continue": cont, "added": added}, err)
	})

	mux.HandleFunc("GET /api/item/{id}/stream", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		it, err := getItem(db, lb.Key(), id)
		if err != nil || it.State != "present" {
			http.NotFound(w, r)
			return
		}
		root := lb.Root()
		full := filepath.Join(root, filepath.FromSlash(it.RelPath))

		if rel, err := filepath.Rel(root, full); err != nil || strings.HasPrefix(rel, "..") {
			http.Error(w, "forbidden", 403)
			return
		}
		if r.URL.Query().Get("dl") == "1" {

			w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(full)+"\"")
			http.ServeFile(w, r, full)
			return
		}

		if needsRemux(full) {

			startSec, _ := strconv.ParseFloat(r.URL.Query().Get("t"), 64)
			audioIdx, _ := strconv.Atoi(r.URL.Query().Get("audio"))
			if err := streamRemux(r.Context(), w, binDir, full, startSec, audioIdx); err != nil && !errors.Is(err, errStreamStarted) {
				http.Error(w, "no se pudo reproducir: "+err.Error(), 500)
			}
			return
		}

		http.ServeFile(w, r, full)
	})

	mux.HandleFunc("GET /api/item/{id}/next", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if it, ok := nextInAlbum(db, lb.Key(), id); ok {
			writeJSON(w, it, nil)
			return
		}
		writeJSON(w, nil, nil)
	})

	mux.HandleFunc("GET /api/item/{id}/tracks", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		it, err := getItem(db, lb.Key(), id)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		full := filepath.Join(lb.Root(), filepath.FromSlash(it.RelPath))
		dur, tr := probeAll(r.Context(), binDir, full)
		writeJSON(w, map[string]any{"seconds": dur, "audio": tr.Audio, "subs": tr.Subs}, nil)
	})

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

	mux.HandleFunc("POST /api/item/{id}/cover", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		img, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<20))
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

	mux.HandleFunc("POST /api/reveal", func(w http.ResponseWriter, r *http.Request) {
		if !requestIsLocal(r) {
			http.Error(w, "solo disponible en el equipo que ejecuta Gobby", http.StatusForbidden)
			return
		}
		var in struct{ Path string }
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		full, err := safeJoin(lb.Root(), in.Path)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		fi, err := os.Stat(full)
		if err != nil {
			http.Error(w, "no existe", 404)
			return
		}
		if err := revealInExplorer(full, fi.IsDir()); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, map[string]string{"status": "ok"}, nil)
	})

	mux.HandleFunc("POST /api/upload", func(w http.ResponseWriter, r *http.Request) {
		if !requestIsLocal(r) {
			http.Error(w, "solo disponible en el equipo que ejecuta Gobby", http.StatusForbidden)
			return
		}
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, "subida inválida", 400)
			return
		}
		dir, err := safeJoin(lb.Root(), r.FormValue("dir"))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			http.Error(w, "carpeta destino inválida", 400)
			return
		}
		file, hdr, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "falta el fichero", 400)
			return
		}
		defer file.Close()
		dest := filepath.Join(dir, filepath.Base(hdr.Filename))
		out, err := os.Create(dest)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if _, err := io.Copy(out, file); err != nil {
			out.Close()
			http.Error(w, err.Error(), 500)
			return
		}
		out.Close()
		go func(root, key string) { scan(db, root, key) }(lb.Root(), lb.Key())
		writeJSON(w, map[string]string{"status": "ok", "name": filepath.Base(dest)}, nil)
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

	mux.HandleFunc("GET /api/libraries", func(w http.ResponseWriter, r *http.Request) {
		libs, err := listLibraries(db, lb.Key())
		writeJSON(w, libs, err)
	})

	mux.HandleFunc("POST /api/library/switch", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Key string }
		json.NewDecoder(r.Body).Decode(&in)
		if in.Key == "" {
			http.Error(w, "falta key", 400)
			return
		}
		root := in.Key
		if !dirExists(root) {
			root = lb.Root()
		}
		lb.Set(root, in.Key)
		if dirExists(root) {
			go func(root, key string) { scan(db, root, key) }(root, in.Key)
		}
		writeJSON(w, map[string]string{"status": "ok"}, nil)
	})

	mux.HandleFunc("POST /api/library/rebind", func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Key string }
		json.NewDecoder(r.Body).Decode(&in)
		if in.Key == "" {
			http.Error(w, "falta key", 400)
			return
		}
		newRoot, _ := filepath.Abs(binaryDir())
		newKey := rootKey(newRoot)
		if err := rebindLibrary(db, in.Key, newKey); err != nil {
			writeJSON(w, nil, err)
			return
		}
		lb.Set(newRoot, newKey)
		go func(root, key string) { scan(db, root, key) }(newRoot, newKey)
		writeJSON(w, map[string]string{"status": "ok", "root": newRoot}, nil)
	})

	mux.HandleFunc("GET /api/db/export", func(w http.ResponseWriter, r *http.Request) {
		tmp := dbPath + ".export"
		if err := exportLibrary(db, lb.Key(), tmp); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer os.Remove(tmp)
		f, err := os.Open(tmp)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="gobby.db"`)
		io.Copy(w, f)
	})

	mux.HandleFunc("POST /api/db/import", func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 512<<20))
		if err != nil {
			http.Error(w, "subida demasiado grande o inválida", 400)
			return
		}
		if len(body) < 16 || string(body[:15]) != "SQLite format 3" {
			http.Error(w, "no parece una base de datos Gobby", 400)
			return
		}
		src := dbPath + ".import"
		if err := os.WriteFile(src, body, 0o644); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer os.Remove(src)
		added, err := mergeImport(db, src)
		if err != nil {
			writeJSON(w, nil, err)
			return
		}
		writeJSON(w, map[string]any{"status": "ok", "added": added}, nil)
	})

	tun := &tunnel{}
	_, port, _ := net.SplitHostPort(addr)
	mux.HandleFunc("POST /api/tunnel/start", func(w http.ResponseWriter, r *http.Request) {
		go tun.start(ctx, binDir, port)
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

	mux.HandleFunc("GET /api/clients", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"base": baseURL(r), "clients": clients.list()}, nil)
	})

	_, mcpPort, _ := net.SplitHostPort(addr)
	uiBase := "http://localhost:" + mcpPort
	if ip := lanIP(); ip != "" {
		uiBase = "http://" + net.JoinHostPort(ip, mcpPort)
	}
	mux.Handle("/mcp", mcpHandler(db, version, lb.Root(), lb.Key(), uiBase))

	ui, err := fs.Sub(webFS, "web")
	if err != nil {
		return err
	}
	fileServer := http.FileServer(http.FS(ui))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		w.Header().Set("Cache-Control", "no-store, must-revalidate")
		fileServer.ServeHTTP(w, r)
	}))

	tracked := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path

		if viaPublicTunnel(r) {
			want := tun.Token()
			if want == "" || !tunnelKeyOK(r, want) {
				http.Error(w, "enlace no válido o caducado", http.StatusForbidden)
				return
			}
			if r.URL.Query().Get("k") == want {
				http.SetCookie(w, &http.Cookie{Name: "gobby_k", Value: want, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
			}
		}
		if p != "/api/clients" && p != "/api/qr" && !strings.HasSuffix(p, "/status") {
			clients.note(r)
		}

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
