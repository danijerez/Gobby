<div align="center">
  <img src=".github/logo.svg" alt="Gobby" width="120">

  <h1>Gobby</h1>

  <p><em>Gobby is a free elf. Gobby serves your media, and Gobby serves it only to you.</em></p>

  <p>
    <img alt="Go" src="https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white">
    <img alt="SQLite" src="https://img.shields.io/badge/SQLite-003B57?logo=sqlite&logoColor=white">
    <img alt="Alpine.js" src="https://img.shields.io/badge/Alpine.js-8BC0D0?logo=alpinedotjs&logoColor=black">
    <img alt="MCP" src="https://img.shields.io/badge/MCP-server-6E56CF">
    <img alt="Cloudflare" src="https://img.shields.io/badge/Cloudflare-Tunnel-F38020?logo=cloudflare&logoColor=white">
    <img alt="version" src="https://img.shields.io/badge/version-0.1.0-2ea44f">
    <img alt="license" src="https://img.shields.io/badge/license-MIT-blue">
  </p>

  <p><strong>English</strong> · <a href="README.es.md">Español</a></p>
</div>

---

Hello! Gobby is here to help, oh yes.

Gobby is one little Go binary — no masters to install, no databases to feed. You
point Gobby at a folder and Gobby scans your movies, series, music, books and
loose files, remembers them in a tidy SQLite drawer, and opens two doors on **the
same port**:

- a **web page** for your phone or browser (Gobby prints a link and a QR in the terminal), and
- an **MCP server** 🧙‍♂️ so a **supreme intelligence** can search your library.

Gobby never *owns* anything. Gobby only **explores** what you already have,
**fetches** the little covers and details from free services, and — if you ask
him nicely — **opens a tunnel** so you can watch from afar. No copyright lives in
Gobby's drawer, oh no. Gobby is a free elf, not a pirate.

Your little edits — titles, notes, ratings, the watchlist — live **only in the
drawer**. Gobby would iron his hands before touching your original files.

> Made with **vibe coding** ✨ — Gobby was written hand-in-hand with a **supreme
> intelligence**, then read over line by line. A free elf and a clever wizard, oh yes. 🧦🪄

## Getting Gobby 📥

Grab the binary for your platform from the [Releases](https://github.com/danijerez/Gobby/releases)
page and drop it in the folder you want it to live in. That's it — one file, no install.

To play `.mkv`/`.avi` in the browser Gobby needs a slim **ffmpeg**. The first time
one is needed Gobby downloads it automatically from its own release into the
binary's folder (like it does with cloudflared) — nothing to install. If you'd
rather provide your own, drop an `ffmpeg` next to Gobby's binary or have one on
your `PATH` and Gobby uses that instead. Other formats download and cast fine
without ffmpeg — they just won't play inline.

## Building 🔨

Needs **Go 1.26+**. Pure-Go SQLite (modernc) means no CGO and no C toolchain.

```sh
go build -o gobby ./src         # one binary for your current platform
./build/publish.sh              # cross-compile Windows + Linux + macOS into bin/
./build/publish.sh v0.2.0       # ...stamping an explicit version
```

The slim **ffmpeg** is built separately (only if you want to regenerate it —
releases already ship one). It needs Docker and produces a ~20 MB static binary
with every demuxer/decoder but only the mp4/aac encoders Gobby actually uses:

```sh
./build/ffmpeg-slim/build.sh    # drops ffmpeg.exe next to the script
```

## How to call Gobby 📣

```sh
gobby                              # scans next to the binary (or one folder up if empty)
gobby -p D:\                       # scans this folder
gobby -p E:\ -library my-disk      # keeps the same library even if the drive letter changes
gobby -port 9000                   # a different door (8420 by default)
gobby -h                           # Gobby explains himself
```

Gobby prints his name, version and a link (with a QR) when he wakes up.

Then open the link Gobby prints, or scan the QR from your phone. Same house, same door.

## Gobby in a box 🐳

Gobby runs headless in a container just as happily. A `Dockerfile` and
`docker-compose.yml` ship in the repo. Point the `media` mount at your library,
then:

```sh
docker compose up
```

Two volumes, each with its own job:

- **`/media`** — your library, mounted **read-only** (`:ro`). Gobby only reads it.
- **`/data`** — where `gobby.db` lives, a **persistent** volume so your catalogue
  survives restarts.

ffmpeg is baked into the image, so mkv/avi play with nothing to download.
Configure with env vars (all optional): `GOBBY_PATH` (scan folder, default
`/media`), `GOBBY_DATA` (db folder, default `/data`), `GOBBY_PORT`, `GOBBY_LIBRARY`.

Running the plain binary still works exactly as before — the container is just
another way to wake Gobby, not a replacement.

## What Gobby can do 🎬

- **Scans offline** — reads filenames and embedded tags, no internet needed to catalogue.
- **Fetches covers & details** — from free, keyless services (see below) when you ask.
- **Series, albums, authors** — grouped neatly, with seasons, episodes and synopses.
- **Files tab** — a coloured size-tree of the whole disk, so you see what eats the space.
- **Watchlist** — note anything to watch/read/listen later, with custom fields and covers.
- **Plays in the browser** — mkv/avi are remuxed on the fly with a slim **ffmpeg**
  (video copied, audio to AAC), so they play where browsers normally refuse.
- **Pick the audio, drop in subtitles** — choose an audio track and load embedded
  subtitles as WebVTT, all decoded by the same little ffmpeg.
- **Resumes where you left off** — playback position is remembered, with a progress
  bar on the shelf; finishing an episode **auto-plays the next** (music too).
- **Keyboard player** — space to pause, ←/→ to seek, `f` for fullscreen.
- **Reads epub inline** — a paginated reader (epub.js) that remembers your page.
- **Voice search** — tap the mic and speak your query (where the browser supports it).
- **Reveal & upload** (on the machine running Gobby) — open a file's folder in your
  OS explorer, or drop a file into a folder from the Files tab.
- **Remote access** — one click opens a temporary **Cloudflare tunnel** (public link + QR),
  gated by a per-tunnel key so a stray URL alone can't reach your library.
- **Cast to a TV**, preview PDFs/audio/images inline, and a little sound for every tap.
- **Read-only for guests** — anyone arriving through the public tunnel can look, not edit.
- **Updates himself** — from the *About* screen Gobby checks GitHub for a newer
  release and swaps his own binary (and ffmpeg) in place. Restart and he's new.

## The free services Gobby borrows (no keys, ever) 🎁

| For | Service |
| --- | --- |
| Movies & series | Cinemeta |
| Books | Open Library |
| Music | MusicBrainz + Cover Art Archive |
| Games | CheapShark |
| Fallback covers | iTunes Search |
| Remote access | Cloudflare Tunnel |
| Cast to TV | Google Cast |

## What Gobby is built from 🧱

Go · SQLite (modernc) · Alpine.js · [cuelume](https://github.com/Danilaa1/cuelume) (sound) · MCP Go SDK · dhowden/tag · rsc.io/qr · a slim [ffmpeg](https://ffmpeg.org) (playback)

## MCP — for the wizards 🧙

Gobby exposes an MCP server at `/mcp` on the same port. Add it and your **supreme intelligence** 🧙‍♂️ can:

- **Browse & search** — `search_media`, `get_media`, `browse_library`, `recent_media`, `library_info`
- **Play** — `play_media` (opens on the host *and* returns a web link to watch on any device)
- **Edit** — `update_media` (title/notes/rating/…), `set_media_section`, `set_media_progress`, `enrich_media`
- **Watchlist** — `search_titles`, `add_to_watchlist`, `list_watchlist`, `remove_from_watchlist`, `set_watchlist_done`

```sh
claude mcp add --transport http gobby http://localhost:8420/mcp
```

## License & credits 📜

Gobby's own code is **MIT** — see [LICENSE](LICENSE). The bundled libraries and
the ffmpeg binary keep their own licenses:

| Component | License | Notes |
| --- | --- | --- |
| [ffmpeg](https://ffmpeg.org) (slim build) | **LGPL-2.1-or-later** | Built with **no** `--enable-gpl` / `--enable-nonfree`. Source: ffmpeg.org; exact build config in [build/ffmpeg-slim/](build/ffmpeg-slim/). Shipped as a separate, unmodified binary you can replace. |
| Go std + `golang.org/x/*` | BSD-3-Clause | |
| [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) (+ libc, memory, …) | BSD-3-Clause | pure-Go SQLite |
| [dhowden/tag](https://github.com/dhowden/tag) | BSD-2-Clause | audio metadata |
| [mdp/qrterminal](https://github.com/mdp/qrterminal), [rsc.io/qr](https://pkg.go.dev/rsc.io/qr) | MIT / BSD-3 | terminal + PNG QR |
| [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk), google/jsonschema-go, google/uuid | Apache-2.0 / BSD-3 | |
| [Alpine.js](https://alpinejs.dev) | MIT | vendored in `src/web/` |
| [cuelume](https://github.com/Danilaa1/cuelume) | MIT | tap sounds, vendored in `src/web/` |
| [epub.js](https://github.com/futurepress/epub.js) | BSD-2-Clause | inline epub reader, vendored in `src/web/` |
| [JSZip](https://stuk.github.io/jszip/) | MIT | used under MIT (JSZip is MIT-or-GPLv3); vendored in `src/web/` |

**Logo:** the goblin mark is [“Goblin” by Caro Asercion](https://game-icons.net/1x1/caro-asercion/goblin.html),
licensed [CC BY 3.0](https://creativecommons.org/licenses/by/3.0/) and recoloured / set on a
background for Gobby.

**On the media Gobby indexes:** Gobby ships **no** content and no copyrighted
material. It only reads files you already have on your own disk and fetches cover
art and metadata from public, keyless services — [Cinemeta](https://www.stremio.com/),
[Open Library](https://openlibrary.org), [MusicBrainz](https://musicbrainz.org) /
[Cover Art Archive](https://coverartarchive.org), [CheapShark](https://www.cheapshark.com),
and the [iTunes Search API](https://performance-partners.apple.com/search-api) —
used within their terms for read-only, personal cataloguing. The Cloudflare tunnel
and Google Cast are optional, user-initiated conveniences. What you do with your
own library is, as ever, on you — Gobby is a free elf, not your lawyer. 🧦

---

<div align="center"><sub>Gobby has no master. Gobby has a friend. 🧦</sub></div>
