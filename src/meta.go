package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/dhowden/tag"
)

// kindFor maps a file extension to a media kind, or "" if we don't care about it.
func kindFor(ext string) string {
	switch strings.ToLower(ext) {
	case ".mp4", ".mkv", ".avi", ".mov", ".m4v", ".webm":
		return "video"
	case ".mp3", ".flac", ".m4a", ".ogg", ".opus", ".wav":
		return "audio"
	case ".epub", ".pdf", ".mobi", ".azw3":
		return "book"
	}
	if usefulFileExt(ext) {
		return "file" // lands in the "Files" section, not a media tab
	}
	return ""
}

// usefulFileExt marks non-media files worth cataloguing in the Files tab:
// documents, images, archives, disk images, installers. Deliberately excludes
// system/junk (exe/dll aside — installers kept, loose binaries skipped) so a
// scan of a real disk stays about *your* files, not the OS.
func usefulFileExt(ext string) bool {
	switch strings.ToLower(ext) {
	case // images
		".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".svg", ".tiff", ".heic",
		// documents
		".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".txt", ".md", ".rtf", ".odt", ".csv",
		// archives
		".zip", ".rar", ".7z", ".tar", ".gz", ".bz2", ".xz",
		// disk images / installers / packages
		".iso", ".img", ".apk", ".exe", ".msi", ".dmg", ".deb", ".rpm", ".appimage",
		// comics / subtitles / misc
		".cbz", ".cbr", ".srt", ".sub", ".nfo":
		return true
	}
	return false
}

// extractMeta fills an Item's metadata from the file. It always sets a title
// (falling back to a cleaned filename) so nothing is ever lost. Returns the
// embedded cover art if present.
func extractMeta(path, kind string) (Item, []byte) {
	it := Item{Kind: kind}

	// folder is the containing directory name, used as the grouping key
	// (album/author/series) when a tag is missing. Never the media root itself.
	folder := cleanName(filepath.Base(filepath.Dir(path)))

	// Audio is the only kind with reliably-embedded tags via a pure-Go lib.
	if kind == "audio" {
		it.Title = cleanName(filepath.Base(path))
		it.Album = folder // fallback; overwritten by tag below if present
		if f, err := os.Open(path); err == nil {
			defer f.Close()
			if m, err := tag.ReadFrom(f); err == nil {
				if t := m.Title(); t != "" {
					it.Title = t
				}
				it.Artist = m.Artist()
				if a := m.Album(); a != "" {
					it.Album = a
				}
				it.Year = m.Year()
				it.Genre = m.Genre()
				if p := m.Picture(); p != nil {
					return it, p.Data
				}
			}
		}
		return it, nil
	}

	// Generic files: just a clean name, no release/episode parsing.
	if kind == "file" {
		it.Title = cleanName(filepath.Base(path))
		it.Album = "" // standalone, listed loose in the Files tab
		return it, nil
	}

	// Video/book: parse the filename (and parent dirs for series/authors).
	p := parseName(path)
	it.Title = p.Title
	it.Year = p.Year
	it.Season = p.Season
	it.Episode = p.Episode
	if !p.Tech.empty() {
		t := p.Tech
		it.Meta = &Meta{Tech: &t}
	}
	switch {
	case kind == "video" && p.Series != "":
		it.Album = p.Series
		it.Artist = p.Series // so search by series name works
	case kind == "book":
		it.Album = folder // group books by their author folder
	default:
		it.Album = ""
	}

	// MP4 shares its container with m4a, so the same lib can read an embedded
	// title cheaply. MKV is unsupported and releases rarely tag it usefully, so
	// we don't add a Matroska parser. A tag only wins over the parsed name if it
	// looks cleaner (no release junk), never for episodes (SxxExx is better).
	if kind == "video" && p.Series == "" && strings.EqualFold(filepath.Ext(path), ".mp4") {
		if f, err := os.Open(path); err == nil {
			defer f.Close()
			if m, err := tag.ReadFrom(f); err == nil {
				if embedded := strings.TrimSpace(m.Title()); embedded != "" && cleanTitleScore(embedded) > cleanTitleScore(it.Title) {
					it.Title = embedded
				}
			}
		}
	}
	return it, nil
}

// cleanTitleScore ranks a candidate title: fewer release-tag words and no glued
// resolution suffix scores higher. Used to decide if an embedded MP4 title
// beats the name parsed from the filename.
func cleanTitleScore(s string) int {
	score := 100
	for _, w := range strings.Fields(s) {
		if techWord.MatchString(strings.Trim(w, "-+()[]")) {
			score -= 30
		}
	}
	if reGluedRes.MatchString(s) {
		score -= 30
	}
	return score
}

// Parsed is the structured result of interpreting a media filename.
type Parsed struct {
	Title           string
	Year            int
	Series          string
	Season, Episode int
	Tech            TechInfo // release tags recovered from the filename (local metadata)
}

// TechInfo is the technical metadata recovered from a release filename — these
// are not junk to discard but real, key-free metadata: resolution, source,
// codecs and audio languages. The audio codec in particular tells us whether a
// browser can play the file's sound (AC3/DTS/EAC3 cannot).
type TechInfo struct {
	Resolution string   `json:"resolution,omitempty"` // 1080p, 2160p, 4K
	Source     string   `json:"source,omitempty"`     // WEB-DL, BluRay, HDTV…
	VideoCodec string   `json:"video_codec,omitempty"` // x265/HEVC, x264/H.264
	AudioCodec string   `json:"audio_codec,omitempty"` // AC3, EAC3, DTS, AAC…
	Channels   string   `json:"channels,omitempty"`    // 5.1, 7.1, 2.0
	Languages  []string `json:"languages,omitempty"`   // Castellano, Ingles…
}

func (t TechInfo) empty() bool {
	return t.Resolution == "" && t.Source == "" && t.VideoCodec == "" &&
		t.AudioCodec == "" && t.Channels == "" && len(t.Languages) == 0
}

var (
	// SxxExx (also 1x02) episode marker. Use (?:^|[^a-z0-9]) instead of \b so it
	// still fires in "Arcane_S02E01" where "_" defeats a word boundary.
	reEpisode = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])S(\d{1,2})E(\d{1,3})(?:[^a-z0-9]|$)|(?:^|[^a-z0-9])(\d{1,2})x(\d{2,3})(?:[^a-z0-9]|$)`)
	// A 4-digit year 1900-2099, optionally in brackets/parens.
	reYear = regexp.MustCompile(`[\(\[]?\b(19\d{2}|20\d{2})\b[\)\]]?`)
	reSiteTag  = regexp.MustCompile(`(?i)\[?\s*www\.[^\]\s]+\s*\]?`) // [www.newpct.com]
	reBrackets = regexp.MustCompile(`\[[^\]]*\]|\([^)]*\)`)          // leftover [..] (..) incl [3649] (r1.6)
	reSpaces   = regexp.MustCompile(`\s{2,}`)
	// Glued release suffix at the end: "OblivionM1080", "Atlantis1M1080", "planetatesoroM1080".
	reGluedRes = regexp.MustCompile(`(?i)\d?[mp]\s?(1080|720|480)p?$`)
	// Loose episode marker anywhere in a name: "Capitulo 05", "Cap.201", "[Cap 5]",
	// "Episodio 3", "Ep 12". Separator before the number may be space or dot.
	reLooseEp = regexp.MustCompile(`(?i)(?:cap[ií]tulo|cap|episodio|epis|ep)[\s.]*(\d{1,4})`)
	// Season/book number from a folder name: "Season 02", "Temporada 3", "Libro 2".
	reSeasonNum = regexp.MustCompile(`(?i)\b(?:season|temporada|libro|book|s|t)\s*(\d{1,2})\b`)

	// techWord matches the FIRST word that marks the start of the release-tag
	// tail: everything from here on is technical metadata, not the title. The
	// title is whatever precedes the earliest such marker. Kept anchored to word
	// starts so it never fires inside a real title word.
	// Anchored to the WHOLE word (^…$) so "multi" never truncates "Multiverse"
	// and "hd" never eats "HDsomething" — a marker must be the entire token.
	techWord = regexp.MustCompile(`(?i)^(` +
		`\d{3,4}p|[24]160p|4k|uhd|hd|sd|` + // resolution
		`hevc|x264|x265|h264|h265|av1|xvid|divx|10b|10bit|8bit|10bits|` + // video codec
		`bdrip|brrip|bd-?rip|blu-?ray|bluray|web-?dl|webdl|webrip|web|hdtv|dvdrip|dvdscr|hdrip|cam|hdcam|remux|` + // source
		`ac3|e-?ac3|eac3|ddp?5\.?1|dd5\.?1|dts(-hd)?|truehd|atmos|aac5?\.?1?|aac|mp3|flac|opus|` + // audio codec
		`spanish|english|castellano|latino|ingl[eé]s|espa[nñ]ol|japon[eé]s|japanese|korean|coreano|french|franc[eé]s|italiano|multi|dual|vose|vos|subs?|forzados?|` + // language / subs
		`imax|60fps|proper|extended|remastered|micro\d*|m\d{3,4}|bd\d{3,4}|[257]\.[01]|` + // misc + channel layout (2.0/5.1/7.1)
		`open|matte|ext|uncut` + // "Open Matte", "V Ext" (versión extendida)
		`)$`)

	// Extractors that pull the technical facts back OUT of the tail so they
	// survive as local metadata instead of being thrown away.
	reResolution = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|480p|4k|uhd)\b`)
	reVideoCodec = regexp.MustCompile(`(?i)\b(x265|h\.?265|hevc|x264|h\.?264|av1|xvid|divx)\b`)
	reSource     = regexp.MustCompile(`(?i)\b(web-?dl|webdl|webrip|web|blu-?ray|bluray|bd-?rip|bdrip|brrip|hdtv|dvdrip|hdrip|remux)\b`)
	reAudioCodec = regexp.MustCompile(`(?i)\b(e-?ac-?3|eac3|ddp?5\.?1|dd5\.?1|atmos|truehd|dts-?hd|dts|ac-?3|aac5?|aac|flac|opus|mp3)\b`)
	reChannels   = regexp.MustCompile(`(?i)\b([257]\.[01])\b`) // 5.1, 7.1, 2.0
	reLang       = regexp.MustCompile(`(?i)\b(castellano|espa[nñ]ol|spanish|ingl[eé]s|english|latino|japon[eé]s|japanese|coreano|korean|franc[eé]s|french|italiano|dual|multi)\b`)
)

// parseName extracts a clean title, year, and (for series) name/season/episode
// from a file path. It leans on parent directory names, which are usually much
// cleaner than the episode filename (e.g. .../Arcane/Season 02/Arcane_S02E01.mp4).
func parseName(path string) Parsed {
	base := stripExt(filepath.Base(path))

	var p Parsed

	// Explicit SxxExx / 1x02 marker in the filename.
	if m := reEpisode.FindStringSubmatch(base); m != nil {
		p.Season, p.Episode = episodeNums(m)
		p.Series = seriesName(path)
		p.Title = seasonEpisodeLabel(p.Season, p.Episode, base)
		return p
	}

	// Jellyfin's convention: a "Season NN" (or "Temporada"/"Libro") folder anywhere
	// above the file marks it as an episode, even if the filename has no SxxExx —
	// covers "Season 02/One Piece [Cap.201]/One Piece [Cap.201].mkv".
	if seasonDir, series := findSeasonDir(path); seasonDir != "" {
		p.Series = series
		if m := reSeasonNum.FindStringSubmatch(seasonDir); m != nil {
			p.Season, _ = strconv.Atoi(m[1])
		}
		// Episode number: look in the filename, then in intermediate folder names.
		if m := reLooseEp.FindStringSubmatch(base); m != nil {
			p.Episode, _ = strconv.Atoi(m[1])
		} else if m := reLooseEp.FindStringSubmatch(filepath.Base(filepath.Dir(path))); m != nil {
			p.Episode, _ = strconv.Atoi(m[1])
		}
		p.Title = seasonEpisodeLabel(p.Season, p.Episode, base)
		return p
	}

	// Movie / book: pull the year out first, then scrub the rest.
	if y := reYear.FindStringSubmatch(base); y != nil {
		p.Year, _ = strconv.Atoi(y[1])
	}
	p.Title, p.Tech = scrub(base)
	if p.Title == "" {
		p.Title = cleanName(base) // never return empty
	}
	return p
}

// findSeasonDir walks up from the file looking for a "Season NN"/"Temporada"/
// "Libro" folder. Returns that folder's name and the series name (the folder
// directly above it). Returns ("","") if none found.
func findSeasonDir(path string) (seasonDir, series string) {
	dir := filepath.Dir(path)
	for {
		name := filepath.Base(dir)
		parent := filepath.Dir(dir)
		if name == "" || name == "." || parent == dir {
			return "", ""
		}
		if isSeasonDir(name) {
			return name, cleanName(filepath.Base(parent))
		}
		dir = parent
	}
}

// scrub turns a release filename into a clean title plus the technical metadata
// it carried. Strategy: the title is everything BEFORE the first release-tag
// word (year, resolution, source, codec…); the whole string is then mined for
// tech facts so nothing useful is lost. Turns
// "(1940) - Pinocho Bdrip 1080P Hevc 10B-Ac3 By Byp58" into ("Pinocho", {…}) and
// "John.Wick.Chapter.4.2023.2160p.4K.WEB.x265.10bit.AAC5.1-[YTS.MX]" into
// ("John Wick Chapter 4", {Resolution:2160p, Source:WEB, VideoCodec:x265, AudioCodec:AAC, Channels:5.1}).
func scrub(s string) (title string, tech TechInfo) {
	tech = releaseTech(s) // mine tech facts from the raw string first

	s = reSiteTag.ReplaceAllString(s, " ") // strip www.x so a glued suffix ends the string
	s = strings.Trim(s, " .")
	s = reGluedRes.ReplaceAllString(s, "") // OblivionM1080 -> Oblivion
	s = reBrackets.ReplaceAllString(s, " ") // (1940), [3649], (r1.6), (Spanish...), [YTS.MX]
	s = reChannels.ReplaceAllString(s, " ") // drop 5.1/2.0 while the dot is intact
	s = strings.NewReplacer(".", " ", "_", " ").Replace(s)
	s = reSpaces.ReplaceAllString(s, " ")

	// Truncate at the first word that is a release marker: everything from there
	// on is tags, not title. This is far more robust than deleting known tokens
	// one by one — an unknown release group after the tail (BONE, EVO) never
	// reaches the title because a real marker (year/1080p/WEB) precedes it.
	words := strings.Fields(s)
	cut := len(words)
	for i, w := range words {
		wc := strings.Trim(w, "-+()[]")
		if reYear.MatchString(w) || techWord.MatchString(wc) {
			cut = i
			break
		}
	}
	title = strings.Join(words[:cut], " ")
	title = strings.Trim(title, " -+([")
	title = reSpaces.ReplaceAllString(title, " ")
	return strings.TrimSpace(title), tech
}

// releaseTech pulls resolution/source/codecs/languages out of a raw release
// name. These are real, key-free metadata (and the audio codec decides whether
// a browser can play the sound at all).
func releaseTech(s string) TechInfo {
	norm := strings.NewReplacer("_", " ").Replace(s) // keep dots: "5.1" must survive
	var t TechInfo
	first := func(re *regexp.Regexp) string {
		if m := re.FindString(norm); m != "" {
			return strings.ToUpper(m)
		}
		return ""
	}
	t.Resolution = first(reResolution)
	t.Source = first(reSource)
	t.VideoCodec = first(reVideoCodec)
	t.AudioCodec = normAudio(first(reAudioCodec))
	t.Channels = first(reChannels)
	seen := map[string]bool{}
	for _, m := range reLang.FindAllString(norm, -1) {
		l := strings.Title(strings.ToLower(m))
		if !seen[l] {
			seen[l] = true
			t.Languages = append(t.Languages, l)
		}
	}
	return t
}

// normAudio collapses release spellings (AAC5.1, E-AC-3, DDP5.1) to a plain
// codec name, dropping any channel-count suffix.
func normAudio(a string) string {
	switch {
	case a == "":
		return ""
	case strings.HasPrefix(a, "AAC"):
		return "AAC"
	case strings.HasPrefix(a, "DDP") || strings.HasPrefix(a, "EAC") || strings.HasPrefix(a, "E-AC"):
		return "EAC3"
	case strings.HasPrefix(a, "DD5"):
		return "AC3"
	case strings.HasPrefix(a, "DTS"):
		return "DTS"
	}
	return strings.ReplaceAll(a, "-", "")
}

// browserPlayableAudio reports whether a browser's built-in decoders can play
// this audio codec. AC3/EAC3/DTS/TrueHD cannot — the video plays silently.
func browserPlayableAudio(codec string) bool {
	switch strings.ToUpper(strings.ReplaceAll(codec, "-", "")) {
	case "AC3", "EAC3", "DTS", "DTSHD", "TRUEHD", "ATMOS", "DD51", "DDP51":
		return false
	}
	return true // AAC, MP3, FLAC(where supported), Opus, or unknown → assume ok
}

func episodeNums(m []string) (season, ep int) {
	if m[1] != "" { // SxxExx
		season, _ = strconv.Atoi(m[1])
		ep, _ = strconv.Atoi(m[2])
	} else { // NxNN
		season, _ = strconv.Atoi(m[3])
		ep, _ = strconv.Atoi(m[4])
	}
	return
}

// seriesName walks up from the file to find the series folder, skipping a
// "Season XX" / "Temporada XX" directory if present.
func seriesName(path string) string {
	parent := filepath.Base(filepath.Dir(path))
	if isSeasonDir(parent) {
		grand := filepath.Base(filepath.Dir(filepath.Dir(path)))
		if grand != "" && grand != "." && grand != string(filepath.Separator) {
			return cleanName(grand)
		}
	}
	return cleanName(parent)
}

var reSeasonDir = regexp.MustCompile(`(?i)^(season|temporada|libro|book|s|t)\s*\d+$`)

func isSeasonDir(name string) bool { return reSeasonDir.MatchString(strings.TrimSpace(name)) }

func seasonEpisodeLabel(s, e int, fallback string) string {
	if s == 0 && e == 0 {
		return cleanName(fallback)
	}
	return "S" + pad2(s) + "E" + pad2(e)
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func stripExt(name string) string { return strings.TrimSuffix(name, filepath.Ext(name)) }

// cleanName is the gentle fallback: just turn separators into spaces.
func cleanName(name string) string {
	name = stripExt(name)
	name = strings.NewReplacer(".", " ", "_", " ").Replace(name)
	return strings.TrimSpace(reSpaces.ReplaceAllString(name, " "))
}
