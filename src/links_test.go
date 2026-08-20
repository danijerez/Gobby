package main

import "testing"

func TestCleanEANTitle(t *testing.T) {
	cases := map[string]string{
		"The Matrix (Blu-ray)":       "The Matrix",
		"Inception [4K UHD]":         "Inception",
		"Dune (Blu-ray + Digital)":   "Dune",
		"Alien (Steelbook Region B)": "Alien",
		"Plain Title":                "Plain Title",
	}
	for in, want := range cases {
		if got := cleanEANTitle(in); got != want {
			t.Errorf("cleanEANTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGuessKind(t *testing.T) {
	cases := map[string]string{
		"The Matrix (Blu-ray)":      "movie",
		"Zelda for Nintendo Switch": "game",
		"Abbey Road (Vinyl LP)":     "audio",
		"Some Random Thing":         "other",
	}
	for in, want := range cases {
		if got := guessKind(in); got != want {
			t.Errorf("guessKind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHTMLMetaParse(t *testing.T) {
	// exercised via the same regexes htmlMeta uses; no network here.
	body := `<html><head>
	<title>Fallback Title</title>
	<meta property="og:title" content="OG &amp; Title">
	<meta name="og:description" content="A description">
	<meta property="og:image" content="https://x/i.png">
	</head></html>`
	var m LinkMeta
	for _, tag := range reOG.FindAllStringSubmatch(body, -1) {
		c := reContent.FindStringSubmatch(tag[0])
		if c == nil {
			continue
		}
		switch tag[1] {
		case "title":
			m.Title = c[1]
		case "description":
			m.Note = c[1]
		case "image":
			m.Poster = c[1]
		}
	}
	if m.Title != "OG &amp; Title" || m.Note != "A description" || m.Poster != "https://x/i.png" {
		t.Fatalf("og parse wrong: %+v", m)
	}
	if got := reTitleTag.FindStringSubmatch(body); got == nil || got[1] != "Fallback Title" {
		t.Fatalf("title fallback wrong: %v", got)
	}
}

func TestGithubPathRegex(t *testing.T) {
	cases := map[string][2]string{
		"/danijerez/Gobby":   {"danijerez", "Gobby"},
		"/foo/bar.git":       {"foo", "bar"},
		"/foo/bar/":          {"foo", "bar"},
		"/foo/bar/tree/main": {"", ""}, // sub-paths shouldn't match owner/repo
	}
	for path, want := range cases {
		m := reGithub.FindStringSubmatch(path)
		if want[0] == "" {
			if m != nil {
				t.Errorf("%s: expected no match, got %v", path, m)
			}
			continue
		}
		if m == nil || m[1] != want[0] || m[2] != want[1] {
			t.Errorf("%s: got %v want %v", path, m, want)
		}
	}
}
