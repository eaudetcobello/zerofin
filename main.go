// Command zerofin serves a video file, or every video and subtitle file in
// a folder, over HTTP on the LAN, with Range support for seeking.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var contentTypes = map[string]string{
	".mkv":  "video/x-matroska",
	".mp4":  "video/mp4",
	".m4v":  "video/mp4",
	".webm": "video/webm",
	".avi":  "video/x-msvideo",
	".srt":  "application/x-subrip",
	".ass":  "text/x-ssa",
	".ssa":  "text/x-ssa",
	".vtt":  "text/vtt",
}

// server serves allowed files from root. In single-file mode, only that file and
// subtitles sharing its base name are allowed, and there is no index page.
type server struct {
	root    *os.Root
	dir     string
	single  string
	tmdb    string
	ripped  sync.Map
	posters sync.Map
}

// entry is one listed file. Play is an Android intent:// link that opens the
// stream in mpv-android, set for videos only.
type entry struct {
	Name, URL string
	Play      string
	Mod       time.Time
}
type group struct {
	Dir   string
	Files []entry
}

func main() {
	port := flag.Int("port", 8080, "port to listen on")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: %s [-port N] [video file or folder, default ~/Downloads]\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() > 1 {
		flag.Usage()
		os.Exit(2)
	}

	arg := flag.Arg(0)
	if arg == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		arg = filepath.Join(home, "Downloads")
	}
	target, err := filepath.Abs(arg)
	if err != nil {
		log.Fatal(err)
	}
	st, err := os.Stat(target)
	if err != nil {
		log.Fatal(err)
	}

	s := &server{tmdb: os.Getenv("TMDB_API_KEY")}
	dir := target
	if !st.IsDir() {
		dir = filepath.Dir(target)
		s.single = filepath.Base(target)
	}
	s.dir = dir
	if s.root, err = os.OpenRoot(dir); err != nil {
		log.Fatal(err)
	}

	base := fmt.Sprintf("http://%s:%d", lanIP(), *port)
	shown := base + "/"
	if s.single != "" {
		shown = base + escape(s.single)
		fmt.Printf("Serving %s\n\n%s\n", s.single, shown)
		for _, g := range s.list(base) {
			for _, f := range g.Files {
				if f.Name != s.single {
					fmt.Println(f.URL)
				}
			}
		}
	} else {
		fmt.Printf("Serving folder %s\n\nWeb UI: %s\n", target, shown)
	}
	fmt.Println()

	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), s))
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/" && s.single == "" {
		s.servePage(w, r)
		return
	}

	if r.URL.Path == "/api/library" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(s.library(""))
		return
	}

	if r.URL.Path == "/api/poster" {
		s.servePoster(w, r)
		return
	}

	if r.URL.Path == "/api/list" {
		s.serveList(w)
		return
	}

	rel := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if !s.allowed(rel) {
		http.NotFound(w, r)
		return
	}
	f, err := s.root.Open(rel)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	log.Printf("%s %s %s %s", r.RemoteAddr, r.Method, rel, r.Header.Get("Range"))
	if strings.ToLower(path.Ext(rel)) == ".mkv" {
		s.ripSubs(rel)
	}
	w.Header().Set("Content-Type", contentTypes[strings.ToLower(path.Ext(rel))])
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}

// ripSubs turns the PGS (image) subtitles of an .mkv into .srt files next to it
// with pgsrip, in the background, the first time the video is played. TVs
// cannot show PGS. It does nothing if pgsrip is not installed or the video
// already has an .srt or .vtt.
func (s *server) ripSubs(rel string) {
	if _, done := s.ripped.LoadOrStore(rel, true); done {
		return
	}
	stem := strings.TrimSuffix(path.Base(rel), path.Ext(rel))
	entries, _ := fs.ReadDir(s.root.FS(), path.Dir(rel))
	for _, e := range entries {
		ext := strings.ToLower(path.Ext(e.Name()))
		if (ext == ".srt" || ext == ".vtt") && strings.HasPrefix(e.Name(), stem) {
			return
		}
	}
	bin, err := exec.LookPath("pgsrip")
	if err != nil {
		return
	}
	go func() {
		log.Printf("ripping subtitles of %s", rel)
		out, err := exec.Command(bin, "rip", filepath.Join(s.dir, filepath.FromSlash(rel))).CombinedOutput()
		if err != nil {
			log.Printf("pgsrip %s: %v\n%s", rel, err, out)
			return
		}
		log.Printf("ripped subtitles of %s", rel)
	}()
}

// allowed reports whether rel (slash-separated, relative to root) may be served.
func (s *server) allowed(rel string) bool {
	ext := strings.ToLower(path.Ext(rel))
	if _, ok := contentTypes[ext]; !ok || rel == "" {
		return false
	}
	for part := range strings.SplitSeq(rel, "/") {
		if strings.HasPrefix(part, ".") {
			return false
		}
	}
	if s.single == "" || rel == s.single {
		return true
	}
	isSub := ext == ".srt" || ext == ".ass" || ext == ".ssa" || ext == ".vtt"
	stem := strings.TrimSuffix(s.single, path.Ext(s.single))
	return isSub && !strings.Contains(rel, "/") && strings.HasPrefix(rel, stem)
}

// list walks root and returns the allowed files grouped by directory, with
// absolute URLs built from base, in lexical order.
func (s *server) list(base string) []group {
	var groups []group
	fs.WalkDir(s.root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != "." && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !s.allowed(p) {
			return nil
		}
		dir := path.Dir(p)
		if dir == "." {
			dir = ""
		}
		i := 0
		for i < len(groups) && groups[i].Dir != dir {
			i++
		}
		if i == len(groups) {
			groups = append(groups, group{Dir: dir})
		}
		g := &groups[i]
		e := entry{Name: path.Base(p), URL: base + escape(p)}
		if ct := contentTypes[strings.ToLower(path.Ext(p))]; strings.HasPrefix(ct, "video/") {
			e.Play = "intent://" + strings.TrimPrefix(base, "http://") + escape(p) +
				"#Intent;scheme=http;type=" + ct + ";package=is.xyz.mpv;end"
		}
		if info, err := d.Info(); err == nil {
			e.Mod = info.ModTime()
		}
		g.Files = append(g.Files, e)
		return nil
	})
	return groups
}

// apiVideo is one video in /api/list. Path and Subs are escaped URL paths; Subs
// are the .srt and .vtt files in the same folder whose names start with the
// video's base name.
type apiVideo struct {
	Name string   `json:"name"`
	Path string   `json:"path"`
	Subs []string `json:"subs"`
}

type apiGroup struct {
	Dir    string     `json:"dir"`
	Videos []apiVideo `json:"videos"`
}

// serveList writes the videos as JSON for the TV app, which runs on another
// origin and needs CORS.
func (s *server) serveList(w http.ResponseWriter) {
	var out []apiGroup
	for _, g := range s.list("") {
		ag := apiGroup{Dir: g.Dir, Videos: []apiVideo{}}
		for _, f := range g.Files {
			if f.Play == "" {
				continue
			}
			ag.Videos = append(ag.Videos, apiVideo{Name: f.Name, Path: f.URL, Subs: subsOf(g, f.Name)})
		}
		if len(ag.Videos) > 0 {
			out = append(out, ag)
		}
	}
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}

// subsOf returns the URLs of the .srt and .vtt files in g whose names start
// with the base name of video.
func subsOf(g group, video string) []string {
	subs := []string{}
	stem := strings.TrimSuffix(video, path.Ext(video))
	for _, f := range g.Files {
		ext := strings.ToLower(path.Ext(f.Name))
		if (ext == ".srt" || ext == ".vtt") && strings.HasPrefix(f.Name, stem) {
			subs = append(subs, f.URL)
		}
	}
	return subs
}

// escape turns a slash-separated relative path into an escaped URL path.
func escape(rel string) string {
	return (&url.URL{Path: "/" + rel}).EscapedPath()
}

// lanIP returns the first private IPv4 address of an up interface, or localhost.
func lanIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "localhost"
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && n.IP.IsPrivate() {
			return n.IP.String()
		}
	}
	return "localhost"
}
