// Package webui provides the offline, embedded recording review interface.
package webui

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"track-tagger/internal/catalog"
)

type Config struct {
	Addr     string
	Token    string
	Remote   bool
	Writable bool
}

type Recording struct {
	PlaybackOnly      bool
	DateUnknown       bool
	ID, Path, Status  string
	Fingerprint       string
	Metadata          map[string]string
	Tracks            []Track
	Artwork           []string
	SelectedArtwork   string
	Reasons, Evidence []string
	Notes             []Note
}
type Note struct{ Path, Text string }

type Track struct {
	PlaybackID          string
	File, Title, Artist string
	Track, Disc         int
}
type Review struct {
	Fingerprint string `json:"-"`
	Approve     bool
	DateUnknown bool
	Metadata    map[string]string
	Tracks      []Track
	Artwork     string
}
type Job struct{ ID, RecordingID, Status, Summary string }
type Backend interface {
	List(context.Context) ([]Recording, error)
	Get(context.Context, string) (Recording, error)
	SaveReview(context.Context, string, Review) error
	Apply(context.Context, string) error
	Undo(context.Context, string) error
	History(context.Context) ([]Job, error)
}

//go:embed assets/*
var assets embed.FS

type server struct {
	cfg       Config
	backend   Backend
	csrf      string
	templates *template.Template
}
type page struct {
	Writable   bool
	CSRF       string
	Recordings []Recording
	Recording  *Recording
	Jobs       []Job
	History    bool
}

func validate(c Config) (Config, error) {
	if c.Addr == "" {
		c.Addr = "127.0.0.1:8787"
	}
	host, _, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return c, fmt.Errorf("invalid listen address: %w", err)
	}
	ip := net.ParseIP(host)
	loopback := host == "localhost" || ip != nil && ip.IsLoopback()
	if !loopback && !c.Remote {
		return c, errors.New("non-loopback binding requires explicit remote access")
	}
	if c.Remote && len(c.Token) < 16 {
		return c, errors.New("remote access requires an authentication token of at least 16 characters")
	}
	return c, nil
}

func New(cfg Config, backend Backend) (http.Handler, error) {
	cfg, err := validate(cfg)
	if err != nil {
		return nil, err
	}
	if backend == nil {
		return nil, errors.New("missing web backend")
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	t, err := template.New("index.html").Funcs(template.FuncMap{"base": displayBase, "title": recordingTitle, "status": statusLabel, "issues": issueSummary, "action": recordingAction, "performerReview": performerReview, "googleSearchURL": googleSearchURL}).ParseFS(assets, "assets/index.html")
	if err != nil {
		return nil, err
	}
	s := &server{cfg: cfg, backend: backend, csrf: hex.EncodeToString(b), templates: t}
	return http.HandlerFunc(s.handle), nil
}

// Serve runs until cancellation. Remote access should be placed behind HTTPS.
func Serve(ctx context.Context, cfg Config, backend Backend) error {
	var err error
	cfg, err = validate(cfg)
	if err != nil {
		return err
	}
	h, err := New(cfg, backend)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	return ServeListener(ctx, listener, h)
}

// ServeListener owns listener and waits for all active requests before returning.
// Cancellation stops accepting requests without interrupting media transactions.
func ServeListener(ctx context.Context, listener net.Listener, h http.Handler) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 2 * time.Minute, IdleTimeout: 60 * time.Second}
	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		// A timeout must not let the owner close storage beneath an active write.
		_ = s.Shutdown(context.Background())
		close(done)
	}()
	err := s.Serve(listener)
	cancel()
	<-done
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *server) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; script-src 'self'; media-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("Cache-Control", "no-store")
	if !s.cfg.Remote {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			http.Error(w, "untrusted host", http.StatusForbidden)
			return
		}
	}
	if s.cfg.Token != "" {
		_, password, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(password), []byte(s.cfg.Token)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Track Tagger", charset="UTF-8"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
	}
	if r.URL.Path == "/audio" {
		s.audio(w, r)
		return
	}
	if r.Method == http.MethodPost {
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host != r.Host || (u.Scheme != "http" && u.Scheme != "https") {
				http.Error(w, "untrusted origin", http.StatusForbidden)
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf")), []byte(s.csrf)) != 1 {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}
		s.mutate(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.URL.Path == "/player.js" {
		b, err := assets.ReadFile("assets/player.js")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(b)
		return
	}
	if r.URL.Path == "/style.css" {
		b, _ := assets.ReadFile("assets/style.css")
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = w.Write(b)
		return
	}
	p := page{Writable: s.cfg.Writable, CSRF: s.csrf}
	var err error
	switch r.URL.Path {
	case "/":
		p.Recordings, err = s.backend.List(r.Context())
	case "/recording":
		var rec Recording
		rec, err = s.backend.Get(r.Context(), r.URL.Query().Get("id"))
		p.Recording = &rec
	case "/history":
		p.History = true
		p.Jobs, err = s.backend.History(r.Context())
		if p.Jobs == nil {
			p.Jobs = []Job{}
		}
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.templates.ExecuteTemplate(w, "index.html", p)
}

func (s *server) mutate(w http.ResponseWriter, r *http.Request) {
	id := r.PostForm.Get("id")
	var err error
	switch r.URL.Path {
	case "/review":
		date := strings.TrimSpace(r.PostForm.Get("date"))
		dateUnknown := r.PostForm.Get("date_unknown") == "yes"
		if dateUnknown && date != "" {
			http.Error(w, "clear the concert date or uncheck Date unknown", http.StatusBadRequest)
			return
		}
		if !dateUnknown && date == "" {
			http.Error(w, "enter a concert date or mark Date unknown", http.StatusBadRequest)
			return
		}
		if !dateUnknown {
			if err := catalog.ValidateDate(date); err != nil {
				http.Error(w, "concert date must be YYYY, YYYY-MM, or a valid YYYY-MM-DD date", http.StatusBadRequest)
				return
			}
		}
		review := Review{DateUnknown: dateUnknown, Metadata: map[string]string{"ARTIST": strings.TrimSpace(r.PostForm.Get("artist")), "ALBUM": strings.TrimSpace(r.PostForm.Get("album")), "DATE": date}, Artwork: r.PostForm.Get("artwork")}
		files, titles := r.PostForm["track_file"], r.PostForm["track_title"]
		discs, numbers := r.PostForm["track_disc"], r.PostForm["track_number"]
		artists := r.PostForm["track_artist"]
		if len(artists) != 0 && len(artists) != len(files) {
			http.Error(w, "incomplete track artists", http.StatusBadRequest)
			return
		}
		if len(files) != len(titles) || len(files) != len(discs) || len(files) != len(numbers) {
			http.Error(w, "incomplete track mapping", http.StatusBadRequest)
			return
		}
		for i, file := range files {
			disc, de := strconv.Atoi(discs[i])
			number, ne := strconv.Atoi(numbers[i])
			if de != nil || ne != nil || disc < 1 || number < 1 {
				http.Error(w, "disc and track numbers must be positive integers", http.StatusBadRequest)
				return
			}
			artist := ""
			if len(artists) != 0 {
				artist = artists[i]
			}
			review.Tracks = append(review.Tracks, Track{File: file, Title: titles[i], Artist: artist, Disc: disc, Track: number})
		}
		review.Approve = r.PostForm.Get("approve") == "yes"
		review.Fingerprint = r.PostForm.Get("fingerprint")
		if review.Fingerprint == "" {
			http.Error(w, "missing source fingerprint; refresh before reviewing", http.StatusBadRequest)
			return
		}
		err = s.backend.SaveReview(r.Context(), id, review)
	case "/apply", "/undo":
		if !s.cfg.Writable {
			http.Error(w, "preview mode cannot change media; restart with --write or --copy", http.StatusForbidden)
			return
		}
		if r.URL.Path == "/apply" {
			err = s.backend.Apply(r.Context(), id)
		} else {
			err = s.backend.Undo(r.Context(), id)
		}
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if r.URL.Path == "/review" {
		http.Redirect(w, r, "/recording?id="+url.QueryEscape(id), http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/history", http.StatusSeeOther)
	}
}
