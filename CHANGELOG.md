# Changelog

All notable changes to Gobby are documented here.
Format based on [Keep a Changelog](https://keepachangelog.com/); versions follow [SemVer](https://semver.org/).

## [0.4.0]

### Added
- **Chat with Gobby** — a floating chat bubble that talks to any OpenAI-compatible
  LLM (Ollama, LM Studio, OpenAI, Gemini, Groq, LiteLLM, OpenRouter, OmniRoute…)
  and answers about your library using Gobby's own tools, in a shy house-elf voice.
  Configure providers from the UI (preset picker, load-models to test the
  connection), keep several and switch with a click, ask by voice, stop mid-reply.
  API keys are stored per provider, redacted from responses and excluded from
  database export; `GOBBY_LLM_KEY` still works as an override.
- **Photo gallery** — images get their own tab: thumbnail grid, lightbox, and
  per-image details (name, format, resolution, size).
- **Filter by colour** — the dominant colour of every cover and photo is computed
  and stored, so the library can be filtered by colour.

### Fixed
- Tolerate LLM error responses in any shape (string, object, or array) instead of
  crashing on unmarshal (e.g. Gemini's OpenAI layer).
- LM Studio: tool declarations now send `required: []` instead of null.
- Loading a provider's model list uses its stored key (no more 404 after editing).

## [0.3.0]

### Added
- **ffmpeg embedded** in the release binary as WebAssembly
  ([go-ffmpreg](https://codeberg.org/gruf/go-ffmpreg)) — one self-contained file,
  nothing to download. A native ffmpeg beside Gobby (or on PATH) is still used
  when present (faster, no startup cost, no transcode limits).
- Player shows a spinner ("Preparing…") while a stream starts; the wasm runtime
  is warmed up in the background at launch so the first playback isn't slow.
- **Continue-watching remembers the audio track and subtitle** you had per item.
- File-size chip on the item detail.
- Cinemeta genres now fill the `genre` column too, so the genre filter works.

### Changed
- **Licence:** the release binary is now **GPLv3** (it embeds GPL ffmpeg). Gobby's
  own code stays MIT; build without `-tags embedffmpeg` for a non-GPL binary that
  downloads a slim LGPL ffmpeg on demand.
- `publish.sh` embeds ffmpeg by default.

## [0.2.1]

### Added
- **Delete from disk** (host-only) — remove a file and its catalogue row, with a
  confirmation step and path-traversal guard. Only from the machine running Gobby.
- **Files filter** — a search box that narrows the size-tree as you type,
  auto-expanding branches that match.
- **HTTPS listener** on the next port (`port+1`) with a self-signed certificate
  (localhost + LAN IP), so the browser's Cast SDK works over the LAN.
- **Ambient background** tinted from the item's cover (or the watchlist poster),
  with a theme scrim so metadata stays legible; neutral in menus.

### Changed
- Header collapses to one row: logo (home) + tabs + search + menu; the Home tab
  is gone (the logo is home).
- Item actions collapse into a **More** submenu, leaving only the primaries visible.
- On mobile the footer controls move into the ⋮ menu; tabs stay a fixed bottom bar.
- The connect QR now points at the HTTPS URL (Cast-ready).
- Cast forces HTTP and gained a **Stop** button.

### Fixed
- Back button no longer double-pops the view stack; switching tabs no longer
  leaves a stale `#item` hash.
- TLS-handshake log noise from self-signed rejections is silenced.

## [0.2.0]

### Added
- **Container support** — `Dockerfile` + `docker-compose.yml`; media mounted
  read-only, `gobby.db` on a persistent volume, ffmpeg baked into the image.
- `-data` flag and `GOBBY_PATH` / `GOBBY_DATA` / `GOBBY_PORT` / `GOBBY_LIBRARY`
  env vars, so the database can live apart from the (read-only) media.
- **Playback resume** with a progress bar; **auto-play** the next episode/track.
- Keyboard player controls (space, arrows, `f`, `esc`).
- **Inline epub reader** (epub.js) that remembers your page.
- **Voice search** via the Web Speech API.
- **Reveal & upload** files (host-only), plus drag-and-drop into a folder.
- Per-tunnel access key: the public URL carries `?k=<token>`.
- MCP toolset grown to 15 tools (browse, recent, update, section, progress,
  enrich, title search, watchlist management); `play_media` returns a web URL.
- Release workflow builds the three platform binaries and a GHCR image on a `v*` tag.

### Changed
- Dependencies updated (MCP Go SDK 1.7.0, modernc.org/sqlite 1.56, others).
- `/tracks` now runs a single ffmpeg probe instead of two.

## [0.1.0]

### Added
- First release: one portable Go binary that indexes movies, series, music,
  books and loose files into SQLite, with a web UI and an MCP server on the
  same port.
- Offline scan, keyless cover/metadata fetch, size-tree Files tab, watchlist.
- Browser playback with on-the-fly mkv/avi remux, audio/subtitle selection.
- Cloudflare quick-tunnel for remote access, Google Cast, self-update.
