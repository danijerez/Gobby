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
		// Movies with release junk (real mock names from the test USB).
		{`D:\films\(1940) - Pinocho Bdrip 1080P Hevc 10B-Ac3 By Byp58.mkv`, "Pinocho", 1940, "", 0, 0},
		{`D:\films\28 Dias Despues [www.newpct.com][toni32].mkv`, "28 Dias Despues", 0, "", 0, 0},
		{`D:\films\101.Dalmatas.(1961)(Spanish.English.Subs).HD.1080p.x264-AC3.by.Geot.mkv`, "101 Dalmatas", 1961, "", 0, 0},
		// Title truncates at the first release marker, so trailing groups (BONE) never reach it.
		{`D:\films\Alienoid 2022 1080p Korean WEB-DL HEVC x265 5.1 BONE.mkv`, "Alienoid", 2022, "", 0, 0},

		// Glued release suffix Mxxxx / 1Mxxxx.
		{`D:\films\OblivionM1080.mkv`, "Oblivion", 0, "", 0, 0},
		{`D:\films\Atlantis1M1080.www.newpct.com.mkv`, "Atlantis", 0, "", 0, 0},
		{`D:\films\planetatesoroM1080.mkv`, "planetatesoro", 0, "", 0, 0},

		// Series episodes: series name comes from the folder, not the filename.
		{`D:\series\Arcane\Season 02\Arcane_S02E01.mp4`, "S02E01", 0, "Arcane", 2, 1},
		{`D:\series\Arcane\Season 02\Arcane_S02E08.mp4`, "S02E08", 0, "Arcane", 2, 8},

		// Episode under a Season folder but named "Libro N ... Capitulo NN" (no SxxExx).
		{`D:\series\Avatar - La leyenda de Aang\Season 02\Avatar La leyenda de Aang - Libro 2 La Tierra - Capitulo 05 - El Dia del Avatar.avi`, "S02E05", 0, "Avatar - La leyenda de Aang", 2, 5},

		// Episode nested in its own subfolder under Season NN, with [Cap.201].
		{`D:\series\One Piece Live Action\Season 02\One Piece [HDTV 1080p][Cap.201]\One Piece [HDTV 1080p][Cap.201].mkv`, "S02E201", 0, "One Piece Live Action", 2, 201},
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
