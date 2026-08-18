package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type matrixProvider struct {
	name  string
	url   string
	model string
	key   string
}

// Each provider is opted in via env: GOBBY_TEST_<NAME>_URL / _MODEL / _KEY.
// A local Ollama is used by default if reachable.
func matrixProviders() []matrixProvider {
	var ps []matrixProvider
	for _, name := range strings.Fields(os.Getenv("GOBBY_TEST_PROVIDERS")) {
		up := strings.ToUpper(name)
		url := os.Getenv("GOBBY_TEST_" + up + "_URL")
		if url == "" {
			continue
		}
		ps = append(ps, matrixProvider{
			name:  name,
			url:   url,
			model: os.Getenv("GOBBY_TEST_" + up + "_MODEL"),
			key:   os.Getenv("GOBBY_TEST_" + up + "_KEY"),
		})
	}
	return ps
}

func TestProviderMatrix(t *testing.T) {
	providers := matrixProviders()
	if len(providers) == 0 {
		t.Skip("no providers configured; set GOBBY_TEST_PROVIDERS + GOBBY_TEST_<NAME>_URL/_MODEL/_KEY")
	}
	dir := t.TempDir()
	db, err := openDB(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, s := range []struct {
		id    int64
		sec   string
		title string
		year  int
	}{{1, "movie", "Zorbo", 2021}, {2, "movie", "Frobnax", 1999}, {3, "series", "Wibblewatch", 2020}} {
		db.Exec(`INSERT INTO items (id, library_key, section, section_source, kind, rel_path, title, year, scan_state, enrich_state)
			VALUES (?, 'test', ?, 'manual', 'video', ?, ?, ?, 'present','not_found')`, s.id, s.sec, s.title+".mkv", s.title, s.year)
	}

	scenarios := []struct {
		name   string
		msg    []string
		expect func(r chatResult) string
	}{
		{"count", []string{"cuantas peliculas tengo?"}, func(r chatResult) string {
			if len(r.Tools) == 0 {
				return "no tool used"
			}
			return ""
		}},
		{"play", []string{"reproduce Zorbo"}, func(r chatResult) string {
			if r.OpenID == 0 {
				return "open_media not triggered (open_id=0)"
			}
			return ""
		}},
		{"context", []string{"que peliculas tengo?", "y de esas cual es de 1999?"}, func(r chatResult) string {
			if !strings.Contains(strings.ToLower(r.Reply), "frobnax") {
				return "lost context: reply did not mention Frobnax"
			}
			return ""
		}},
	}

	for _, p := range providers {
		p := p
		t.Run(p.name, func(t *testing.T) {
			for _, sc := range scenarios {
				sc := sc
				t.Run(sc.name, func(t *testing.T) {
					os.Setenv("GOBBY_LLM_URL", p.url)
					os.Setenv("GOBBY_LLM_MODEL", p.model)
					os.Setenv("GOBBY_LLM_KEY", p.key)
					sid := p.name + "-" + sc.name
					resetSession(sid)
					var res chatResult
					var err error
					for _, m := range sc.msg {
						ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
						res, err = runChat(ctx, db, "test", "http://localhost", sid, m, false)
						cancel()
						if err != nil {
							t.Fatalf("[%s/%s] runChat: %v", p.name, sc.name, err)
						}
					}
					if msg := sc.expect(res); msg != "" {
						t.Errorf("[%s/%s] %s | reply=%q tools=%v open=%d", p.name, sc.name, msg, res.Reply, res.Tools, res.OpenID)
					} else {
						t.Logf("[%s/%s] OK reply=%q tools=%v open=%d", p.name, sc.name, res.Reply, res.Tools, res.OpenID)
					}
				})
			}
		})
	}
}
