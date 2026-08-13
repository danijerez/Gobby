package main

import "testing"

// TestScrub locks the title/tech extraction against the messy real names that
// motivated the truncate-at-first-marker approach.
func TestScrub(t *testing.T) {
	cases := []struct {
		in, title      string
		res, vcodec    string
		acodec, chans  string
	}{
		{"John.Wick.Chapter.4.2023.2160p.4K.WEB.x265.10bit.AAC5.1-[YTS.MX]", "John Wick Chapter 4", "2160P", "X265", "AAC", ""},
		{"Alienoid 2022 1080p Korean WEB-DL HEVC x265 5.1 BONE", "Alienoid", "1080P", "HEVC", "", "5.1"},
		{"Durante.La.Tormenta.2018.1080p.WEB-DL.MVO", "Durante La Tormenta", "1080P", "", "", ""},
		{"Doctor.Strange.in.the.Multiverse.of.Madness.2022.1080p.WEB-DL.DDP5.1.Atmos.H.264-EVO", "Doctor Strange in the Multiverse of Madness", "1080P", "H.264", "EAC3", ""},
		{"El Gigante de Hierro BD1080.[www.newpct1.com]", "El Gigante de Hierro", "", "", "", ""},
		{"Men in Black International (2019) (4K-HD) (Spanish.English.Subs) HD 2160p x265-AC3-5.1 by Papa Noel", "Men in Black International", "4K", "X265", "AC3", "5.1"},
	}
	for _, c := range cases {
		title, tech := scrub(c.in)
		if title != c.title {
			t.Errorf("scrub(%q) title = %q, want %q", c.in, title, c.title)
		}
		if tech.Resolution != c.res || tech.VideoCodec != c.vcodec || tech.AudioCodec != c.acodec || tech.Channels != c.chans {
			t.Errorf("scrub(%q) tech = %+v, want res=%q vcodec=%q acodec=%q chans=%q", c.in, tech, c.res, c.vcodec, c.acodec, c.chans)
		}
	}
}
