package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/dhowden/tag"
)

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
		return "file"
	}
	return ""
}

func usefulFileExt(ext string) bool {
	switch strings.ToLower(ext) {
	case
		".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".svg", ".tiff", ".heic",

		".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".txt", ".md", ".rtf", ".odt", ".csv",

		".zip", ".rar", ".7z", ".tar", ".gz", ".bz2", ".xz",

		".iso", ".img", ".apk", ".exe", ".msi", ".dmg", ".deb", ".rpm", ".appimage",

		".cbz", ".cbr", ".srt", ".sub", ".nfo":
		return true
	}
	return false
}

func extractMeta(path, kind string) (Item, []byte) {
	it := Item{Kind: kind}

	folder := cleanName(filepath.Base(filepath.Dir(path)))

	if kind == "audio" {
		it.Title = cleanName(filepath.Base(path))
		it.Album = folder
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

	if kind == "file" {
		it.Title = cleanName(filepath.Base(path))
		it.Album = ""
		return it, nil
	}

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
		it.Artist = p.Series
	case kind == "book":
		it.Album = folder
	default:
		it.Album = ""
	}

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

type Parsed struct {
	Title           string
	Year            int
	Series          string
	Season, Episode int
	Tech            TechInfo
}

type TechInfo struct {
	Resolution string   `json:"resolution,omitempty"`
	Source     string   `json:"source,omitempty"`
	VideoCodec string   `json:"video_codec,omitempty"`
	AudioCodec string   `json:"audio_codec,omitempty"`
	Channels   string   `json:"channels,omitempty"`
	Languages  []string `json:"languages,omitempty"`
}

func (t TechInfo) empty() bool {
	return t.Resolution == "" && t.Source == "" && t.VideoCodec == "" &&
		t.AudioCodec == "" && t.Channels == "" && len(t.Languages) == 0
}

var (
	reEpisode   = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])S(\d{1,2})E(\d{1,3})(?:[^a-z0-9]|$)|(?:^|[^a-z0-9])(\d{1,2})x(\d{2,3})(?:[^a-z0-9]|$)`)
	reYear      = regexp.MustCompile(`[\(\[]?\b(19\d{2}|20\d{2})\b[\)\]]?`)
	reSiteTag   = regexp.MustCompile(`(?i)\[?\s*www\.[^\]\s]+\s*\]?`)
	reBrackets  = regexp.MustCompile(`\[[^\]]*\]|\([^)]*\)`)
	reSpaces    = regexp.MustCompile(`\s{2,}`)
	reGluedRes  = regexp.MustCompile(`(?i)\d?[mp]\s?(1080|720|480)p?$`)
	reLooseEp   = regexp.MustCompile(`(?i)(?:cap[ií]tulo|cap|episodio|episode|epis|ep|e)[\s._-]*(\d{1,4})`)
	reTailNum   = regexp.MustCompile(`(?:^|[\s._-])(\d{1,3})\s*$`)
	reSeasonNum = regexp.MustCompile(`(?i)\b(?:season|temporada|libro|book|s|t)\s*(\d{1,2})\b`)

	techWord = regexp.MustCompile(`(?i)^(` +
		`\d{3,4}p|[24]160p|4k|uhd|hd|sd|` +
		`hevc|x264|x265|h264|h265|av1|xvid|divx|10b|10bit|8bit|10bits|` +
		`bdrip|brrip|bd-?rip|blu-?ray|bluray|web-?dl|webdl|webrip|web|hdtv|dvdrip|dvdscr|hdrip|cam|hdcam|remux|` +
		`ac3|e-?ac3|eac3|ddp?5\.?1|dd5\.?1|dts(-hd)?|truehd|atmos|aac5?\.?1?|aac|mp3|flac|opus|` +
		`spanish|english|castellano|latino|ingl[eé]s|espa[nñ]ol|japon[eé]s|japanese|korean|coreano|french|franc[eé]s|italiano|multi|dual|vose|vos|subs?|forzados?|` +
		`imax|60fps|proper|extended|remastered|micro\d*|m\d{3,4}|bd\d{3,4}|[257]\.[01]|` +
		`open|matte|ext|uncut` +
		`)$`)

	reResolution = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|480p|4k|uhd)\b`)
	reVideoCodec = regexp.MustCompile(`(?i)\b(x265|h\.?265|hevc|x264|h\.?264|av1|xvid|divx)\b`)
	reSource     = regexp.MustCompile(`(?i)\b(web-?dl|webdl|webrip|web|blu-?ray|bluray|bd-?rip|bdrip|brrip|hdtv|dvdrip|hdrip|remux)\b`)
	reAudioCodec = regexp.MustCompile(`(?i)\b(e-?ac-?3|eac3|ddp?5\.?1|dd5\.?1|atmos|truehd|dts-?hd|dts|ac-?3|aac5?|aac|flac|opus|mp3)\b`)
	reChannels   = regexp.MustCompile(`(?i)\b([257]\.[01])\b`)
	reLang       = regexp.MustCompile(`(?i)\b(castellano|espa[nñ]ol|spanish|ingl[eé]s|english|latino|japon[eé]s|japanese|coreano|korean|franc[eé]s|french|italiano|dual|multi)\b`)
)

func parseName(path string) Parsed {
	base := stripExt(filepath.Base(path))

	var p Parsed

	if m := reEpisode.FindStringSubmatch(base); m != nil {
		p.Season, p.Episode = episodeNums(m)
		p.Series = seriesName(path)
		p.Title = seasonEpisodeLabel(p.Season, p.Episode, base)
		return p
	}

	if seasonDir, series := findSeasonDir(path); seasonDir != "" {
		p.Series = series
		if m := reSeasonNum.FindStringSubmatch(seasonDir); m != nil {
			p.Season, _ = strconv.Atoi(m[1])
		}

		if m := reLooseEp.FindStringSubmatch(base); m != nil {
			p.Episode, _ = strconv.Atoi(m[1])
		} else if m := reTailNum.FindStringSubmatch(base); m != nil {
			p.Episode, _ = strconv.Atoi(m[1])
		} else if m := reLooseEp.FindStringSubmatch(filepath.Base(filepath.Dir(path))); m != nil {
			p.Episode, _ = strconv.Atoi(m[1])
		}
		p.Title = seasonEpisodeLabel(p.Season, p.Episode, base)
		return p
	}

	if y := reYear.FindStringSubmatch(base); y != nil {
		p.Year, _ = strconv.Atoi(y[1])
	}
	p.Title, p.Tech = scrub(base)
	if p.Title == "" {
		p.Title = cleanName(base)
	}
	return p
}

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

func scrub(s string) (title string, tech TechInfo) {
	tech = releaseTech(s)

	s = reSiteTag.ReplaceAllString(s, " ")
	s = strings.Trim(s, " .")
	s = reGluedRes.ReplaceAllString(s, "")
	s = reBrackets.ReplaceAllString(s, " ")
	s = reChannels.ReplaceAllString(s, " ")
	s = strings.NewReplacer(".", " ", "_", " ").Replace(s)
	s = reSpaces.ReplaceAllString(s, " ")

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

func releaseTech(s string) TechInfo {
	norm := strings.NewReplacer("_", " ").Replace(s)
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

func browserPlayableAudio(codec string) bool {
	switch strings.ToUpper(strings.ReplaceAll(codec, "-", "")) {
	case "AC3", "EAC3", "DTS", "DTSHD", "TRUEHD", "ATMOS", "DD51", "DDP51":
		return false
	}
	return true
}

func episodeNums(m []string) (season, ep int) {
	if m[1] != "" {
		season, _ = strconv.Atoi(m[1])
		ep, _ = strconv.Atoi(m[2])
	} else {
		season, _ = strconv.Atoi(m[3])
		ep, _ = strconv.Atoi(m[4])
	}
	return
}

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

func cleanName(name string) string {
	name = stripExt(name)
	name = strings.NewReplacer(".", " ", "_", " ").Replace(name)
	return strings.TrimSpace(reSpaces.ReplaceAllString(name, " "))
}
