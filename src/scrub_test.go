package main

import "testing"

func TestScrub(t *testing.T) {
	cases := []struct {
		in, title     string
		res, vcodec   string
		acodec, chans string
	}{
		{"Sample.Movie.One.2023.2160p.4K.WEB.x265.10bit.AAC5.1-[GRP.XX]", "Sample Movie One", "2160P", "X265", "AAC", ""},
		{"Sample Movie Two 2022 1080p Korean WEB-DL HEVC x265 5.1 GRP", "Sample Movie Two", "1080P", "HEVC", "", "5.1"},
		{"Sample.Movie.Three.2018.1080p.WEB-DL.MVO", "Sample Movie Three", "1080P", "", "", ""},
		{"Sample.Movie.In.The.Long.Title.2022.1080p.WEB-DL.DDP5.1.Atmos.H.264-GRP", "Sample Movie In The Long Title", "1080P", "H.264", "EAC3", ""},
		{"Sample Movie Four BD1080.[www.example.com]", "Sample Movie Four", "", "", "", ""},
		{"Sample Movie Five (2019) (4K-HD) (Spanish.English.Subs) HD 2160p x265-AC3-5.1 by Group", "Sample Movie Five", "4K", "X265", "AC3", "5.1"},
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
