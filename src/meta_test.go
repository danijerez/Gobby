package main

import (
	"path/filepath"
	"testing"
)

func TestParseName(t *testing.T) {
	cases := []struct {
		path    string
		title   string
		year    int
		series  string
		season  int
		episode int
	}{

		{`films/(1940) - Sample Movie Bdrip 1080P Hevc 10B-Ac3 By Grp99.mkv`, "Sample Movie", 1940, "", 0, 0},
		{`films/Example Title [www.example.com][tag01].mkv`, "Example Title", 0, "", 0, 0},
		{`films/Some.Film.(1961)(Spanish.English.Subs).HD.1080p.x264-AC3.by.Grp.mkv`, "Some Film", 1961, "", 0, 0},
		{`films/Another Movie 2022 1080p Korean WEB-DL HEVC x265 5.1 GRP.mkv`, "Another Movie", 2022, "", 0, 0},
		{`films/GluedTitleM1080.mkv`, "GluedTitle", 0, "", 0, 0},
		{`films/SecondTitle1M1080.www.example.com.mkv`, "SecondTitle", 0, "", 0, 0},
		{`films/thirdtitleM1080.mkv`, "thirdtitle", 0, "", 0, 0},

		{`series/Demo Show/Season 02/Demo Show_S02E01.mp4`, "S02E01", 0, "Demo Show", 2, 1},
		{`series/Demo Show/Season 02/Demo Show_S02E08.mp4`, "S02E08", 0, "Demo Show", 2, 8},

		{`series/Sample Series - The Long Name/Season 02/Sample Series The Long Name - Libro 2 La Tierra - Capitulo 05 - Episode Title.avi`, "S02E05", 0, "Sample Series - The Long Name", 2, 5},

		{`series/Long Title Show/Season 02/Long Title Show [HDTV 1080p][Cap.201]/Long Title Show [HDTV 1080p][Cap.201].mkv`, "S02E201", 0, "Long Title Show", 2, 201},
	}
	for _, c := range cases {
		p := parseName(filepath.FromSlash(c.path))
		if p.Title != c.title || p.Year != c.year || p.Series != c.series || p.Season != c.season || p.Episode != c.episode {
			t.Errorf("parseName(%q)\n got  title=%q year=%d series=%q S%dE%d\n want title=%q year=%d series=%q S%dE%d",
				c.path, p.Title, p.Year, p.Series, p.Season, p.Episode,
				c.title, c.year, c.series, c.season, c.episode)
		}
	}
}
