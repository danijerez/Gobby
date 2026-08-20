package main

import (
	"html"
	"net/url"
	"regexp"
	"strings"
)

type LinkMeta struct {
	Title  string `json:"title"`
	Note   string `json:"note"`
	Poster string `json:"poster"`
}

var (
	reOG       = regexp.MustCompile(`(?is)<meta[^>]+(?:property|name)=["']og:(title|description|image)["'][^>]*>`)
	reContent  = regexp.MustCompile(`(?is)content=["']([^"']*)["']`)
	reTitleTag = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reGithub   = regexp.MustCompile(`^/([^/]+)/([^/]+?)(?:\.git)?/?$`)
)

func fetchLinkMeta(raw string) LinkMeta {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return LinkMeta{}
	}
	if strings.EqualFold(u.Host, "github.com") {
		if m := reGithub.FindStringSubmatch(u.Path); m != nil {
			return githubMeta(m[1], m[2])
		}
	}
	return htmlMeta(raw)
}

func githubMeta(owner, repo string) LinkMeta {
	var r struct {
		FullName    string `json:"full_name"`
		Description string `json:"description"`
		Owner       struct {
			AvatarURL string `json:"avatar_url"`
		} `json:"owner"`
	}
	if err := getJSON("https://api.github.com/repos/"+owner+"/"+repo, &r); err != nil {
		return LinkMeta{}
	}
	return LinkMeta{Title: r.FullName, Note: r.Description, Poster: r.Owner.AvatarURL}
}

func htmlMeta(raw string) LinkMeta {
	b, err := getBytes(raw)
	if err != nil {
		return LinkMeta{}
	}
	body := string(b)
	var m LinkMeta
	for _, tag := range reOG.FindAllStringSubmatch(body, -1) {
		c := reContent.FindStringSubmatch(tag[0])
		if c == nil {
			continue
		}
		val := strings.TrimSpace(html.UnescapeString(c[1]))
		switch tag[1] {
		case "title":
			m.Title = val
		case "description":
			m.Note = val
		case "image":
			m.Poster = val
		}
	}
	if m.Title == "" {
		if t := reTitleTag.FindStringSubmatch(body); t != nil {
			m.Title = strings.TrimSpace(html.UnescapeString(t[1]))
		}
	}
	return m
}
