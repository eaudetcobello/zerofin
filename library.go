package main

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	episodeRe = regexp.MustCompile(`(?i)\bS(\d{1,2})[ .-]?E(\d{1,3})\b|\b(\d{1,2})x(\d{2,3})\b`)
	seasonRe  = regexp.MustCompile(`(?i)\b(S\d{1,2}|Season|Complete)\b`)
	qualityRe = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|480p|blu-?ray|web-?dl|webrip|hdtv|dvdrip|brrip|remux|x26[45]|h[ .]?26[45]|hevc)\b`)
	yearRe    = regexp.MustCompile(`\b(19|20)\d{2}\b`)
)

// libTitle is one movie or show, as shown on the index page and in
// /api/library. Poster is a path on this server, empty without a TMDB key.
// Added is the newest file time, in Unix seconds.
type libTitle struct {
	ID     string     `json:"id"`
	Title  string     `json:"title"`
	Year   int        `json:"year"`
	Show   bool       `json:"show"`
	Poster string     `json:"poster"`
	Added  int64      `json:"added"`
	Videos []libVideo `json:"videos"`
}

// libVideo is one video file of a title. Title is the episode title, and Play
// is the link that opens the video in mpv-android.
type libVideo struct {
	Name    string       `json:"name"`
	Title   string       `json:"title"`
	Season  int          `json:"season"`
	Episode int          `json:"episode"`
	URL     string       `json:"url"`
	Play    template.URL `json:"-"`
	Subs    []link       `json:"subs"`
}

// link is a labelled URL. On marks the selected choice in a row of options.
type link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
	On    bool   `json:"-"`
}

// Seasons returns the season numbers of a title in order.
func (t libTitle) Seasons() []int {
	var out []int
	for _, v := range t.Videos {
		if !slices.Contains(out, v.Season) {
			out = append(out, v.Season)
		}
	}
	return out
}

// Meta is the line under a title: its year and, for a show, its size.
func (t libTitle) Meta() string {
	var parts []string
	if t.Year > 0 {
		parts = append(parts, strconv.Itoa(t.Year))
	}
	if t.Show {
		parts = append(parts, plural(len(t.Seasons()), "season"), plural(len(t.Videos), "episode"))
	}
	return strings.Join(parts, " · ")
}

func plural(n int, word string) string {
	if n != 1 {
		word += "s"
	}
	return strconv.Itoa(n) + " " + word
}

// parsed is what the path of a video says about it.
type parsed struct {
	Title, EpTitle  string
	Year            int
	Show            bool
	Season, Episode int
}

// spaced turns the dots and underscores of a name without spaces into spaces.
func spaced(s string) string {
	if strings.Contains(s, " ") {
		return s
	}
	return strings.NewReplacer(".", " ", "_", " ").Replace(s)
}

// parseTitle splits "Title (2015) 1080p..." into the title and its year. A
// number that starts the name or lies in the future is part of the title.
func parseTitle(s string) (string, int) {
	if m := qualityRe.FindStringIndex(s); m != nil {
		s = s[:m[0]]
	}
	year := 0
	all := yearRe.FindAllStringIndex(s, -1)
	for i := len(all) - 1; i >= 0; i-- {
		y, _ := strconv.Atoi(s[all[i][0]:all[i][1]])
		if all[i][0] > 0 && y <= time.Now().Year()+1 {
			s, year = s[:all[i][0]], y
			break
		}
	}
	return strings.Trim(s, " -([._"), year
}

// parseName reads the title, year, season and episode from a video's path
// (slash-separated, relative to root). A file name without a title takes the
// title of its top folder.
func parseName(rel string) parsed {
	stem := spaced(strings.TrimSuffix(path.Base(rel), path.Ext(rel)))
	var p parsed
	head := stem
	if m := episodeRe.FindStringSubmatchIndex(stem); m != nil {
		sub := episodeRe.FindStringSubmatch(stem)
		p.Show = true
		p.Season, _ = strconv.Atoi(sub[1] + sub[3])
		p.Episode, _ = strconv.Atoi(sub[2] + sub[4])
		head = stem[:m[0]]
		tail := stem[m[1]:]
		if i := strings.IndexAny(tail, "(["); i >= 0 {
			tail = tail[:i]
		}
		if q := qualityRe.FindStringIndex(tail); q != nil {
			tail = tail[:q[0]]
		}
		p.EpTitle = strings.Trim(tail, " -._")
	}
	p.Title, p.Year = parseTitle(head)
	if top, _, nested := strings.Cut(rel, "/"); p.Title == "" && nested {
		top = spaced(top)
		if m := seasonRe.FindStringIndex(top); m != nil {
			top = top[:m[0]]
		}
		p.Title, p.Year = parseTitle(top)
	}
	if p.Title == "" {
		p.Title = stem
	}
	return p
}

//go:embed index.html
var indexHTML string

var indexTmpl = template.Must(template.New("index").Parse(indexHTML))

// library groups the videos into movies and shows. Episodes of a show are
// sorted by season and episode.
func (s *server) library(base string) []libTitle {
	titles := []libTitle{}
	index := map[string]int{}
	for _, g := range s.list(base) {
		for _, f := range g.Files {
			if f.Play == "" {
				continue
			}
			p := parseName(path.Join(g.Dir, f.Name))
			id := "movie:" + strings.ToLower(p.Title) + ":" + strconv.Itoa(p.Year)
			if p.Show {
				id = "show:" + strings.ToLower(p.Title)
			}
			i, ok := index[id]
			if !ok {
				i = len(titles)
				index[id] = i
				titles = append(titles, libTitle{ID: id, Title: p.Title, Show: p.Show})
			}
			t := &titles[i]
			if t.Year == 0 {
				t.Year = p.Year
			}
			t.Added = max(t.Added, f.Mod.Unix())
			v := libVideo{
				Name: f.Name, Title: p.EpTitle, Season: p.Season, Episode: p.Episode,
				URL: f.URL, Play: template.URL(f.Play), Subs: []link{},
			}
			stem := strings.TrimSuffix(f.Name, path.Ext(f.Name))
			for _, u := range subsOf(g, f.Name) {
				name, _ := url.PathUnescape(path.Base(u))
				v.Subs = append(v.Subs, link{Label: strings.TrimPrefix(strings.TrimPrefix(name, stem), "."), URL: u})
			}
			t.Videos = append(t.Videos, v)
		}
	}
	for i := range titles {
		t := &titles[i]
		sort.SliceStable(t.Videos, func(a, b int) bool {
			x, y := t.Videos[a], t.Videos[b]
			if x.Season != y.Season {
				return x.Season < y.Season
			}
			return x.Episode < y.Episode
		})
		if s.tmdb != "" {
			q := url.Values{"kind": {"movie"}, "title": {t.Title}}
			if t.Show {
				q.Set("kind", "tv")
			}
			if t.Year > 0 {
				q.Set("year", strconv.Itoa(t.Year))
			}
			t.Poster = "/api/poster?" + q.Encode()
		}
	}
	return titles
}

// pageData is what the index page shows: the grid of Titles, or one Title
// with the Videos of its selected season.
type pageData struct {
	Q                     string
	Kind, Sort            string
	Kinds, Sorts, Seasons []link
	Titles                []libTitle
	Title                 *libTitle
	Videos                []libVideo
}

// servePage renders the library. The query selects a title (t) and season (s),
// or filters (q, kind) and sorts (sort) the grid.
func (s *server) servePage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lib := s.library("http://" + r.Host)
	d := pageData{Q: q.Get("q"), Kind: q.Get("kind"), Sort: q.Get("sort")}
	for i := range lib {
		if lib[i].ID == q.Get("t") {
			d.Title = &lib[i]
		}
	}
	if t := d.Title; t != nil {
		seasons := t.Seasons()
		season := seasons[0]
		if n, err := strconv.Atoi(q.Get("s")); err == nil && slices.Contains(seasons, n) {
			season = n
		}
		for _, n := range seasons {
			label := "Specials"
			if n > 0 {
				label = "Season " + strconv.Itoa(n)
			}
			u := "/?" + url.Values{"t": {t.ID}, "s": {strconv.Itoa(n)}}.Encode()
			d.Seasons = append(d.Seasons, link{label, u, n == season})
		}
		for _, v := range t.Videos {
			if v.Season == season {
				d.Videos = append(d.Videos, v)
			}
		}
	} else {
		option := func(key, val, label string) link {
			v := url.Values{"q": {d.Q}, "kind": {d.Kind}, "sort": {d.Sort}}
			v.Set(key, val)
			return link{label, "/?" + v.Encode(), q.Get(key) == val}
		}
		d.Kinds = []link{option("kind", "", "All"), option("kind", "movie", "Movies"), option("kind", "show", "Shows")}
		d.Sorts = []link{option("sort", "", "Title"), option("sort", "added", "Recently added"), option("sort", "year", "Year")}
		for _, t := range lib {
			kindOK := d.Kind == "" || (d.Kind == "show") == t.Show
			if kindOK && strings.Contains(strings.ToLower(t.Title), strings.ToLower(d.Q)) {
				d.Titles = append(d.Titles, t)
			}
		}
		sort.SliceStable(d.Titles, func(a, b int) bool {
			x, y := d.Titles[a], d.Titles[b]
			if d.Sort == "added" && x.Added != y.Added {
				return x.Added > y.Added
			}
			if d.Sort == "year" && x.Year != y.Year {
				return x.Year > y.Year
			}
			return strings.ToLower(x.Title) < strings.ToLower(y.Title)
		})
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := indexTmpl.Execute(w, d); err != nil {
		log.Print(err)
	}
}

// servePoster redirects to the TMDB poster of a title. Lookups are kept in
// memory, and browsers keep the redirect for a day.
func (s *server) servePoster(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	kind := q.Get("kind")
	if s.tmdb == "" || (kind != "tv" && kind != "movie") || q.Get("title") == "" {
		http.NotFound(w, r)
		return
	}
	key := q.Encode()
	poster, ok := s.posters.Load(key)
	if !ok {
		found, err := s.findPoster(kind, q.Get("title"), q.Get("year"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		poster = found
		s.posters.Store(key, found)
	}
	if poster == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "max-age=86400")
	http.Redirect(w, r, poster.(string), http.StatusFound)
}

var tmdbClient = &http.Client{Timeout: 10 * time.Second}

// findPoster searches TMDB for a movie or tv title and returns the poster URL
// of the first result that has one, or "". With no result for the year, it
// searches again without it.
func (s *server) findPoster(kind, title, year string) (string, error) {
	q := url.Values{"query": {title}}
	if year != "" && kind == "tv" {
		q.Set("first_air_date_year", year)
	} else if year != "" {
		q.Set("year", year)
	}
	// A v4 read access token is a JWT and goes in a header; a v3 key is a query parameter.
	bearer := strings.Contains(s.tmdb, ".")
	if !bearer {
		q.Set("api_key", s.tmdb)
	}
	req, err := http.NewRequest("GET", "https://api.themoviedb.org/3/search/"+kind+"?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	if bearer {
		req.Header.Set("Authorization", "Bearer "+s.tmdb)
	}
	resp, err := tmdbClient.Do(req)
	if err != nil {
		return "", errors.New("tmdb: request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("tmdb: %s", resp.Status)
	}
	var body struct {
		Results []struct {
			PosterPath string `json:"poster_path"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	for _, res := range body.Results {
		if res.PosterPath != "" {
			return "https://image.tmdb.org/t/p/w342" + res.PosterPath, nil
		}
	}
	if year != "" {
		return s.findPoster(kind, title, "")
	}
	return "", nil
}
