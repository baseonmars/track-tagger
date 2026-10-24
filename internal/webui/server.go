// Package webui provides the offline, embedded recording review interface.
package webui

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
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
	"track-tagger/internal/manual"
)

type Config struct {
	Addr     string
	Token    string
	Remote   bool
	Writable bool
}

type Recording struct {
	Reviewed          bool
	PlaybackOnly      bool
	DateUnknown       bool
	LastModified      int64 // Unix nanoseconds from cached file evidence; zero when unavailable.
	ID, Path, Status  string
	RelativePath      string
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

// Status is a cached, immutable view of background validation progress.
type Status struct {
	Phase      string      `json:"phase"`
	Validated  bool        `json:"validated"`
	Scanned    int         `json:"scanned"`
	Total      int         `json:"total"`
	Generation uint64      `json:"generation"`
	Error      string      `json:"error,omitempty"`
	Recordings []Recording `json:"-"`
}
type statusBackend interface {
	Status(context.Context) (Status, error)
}
type browseStopper interface{ StopBrowsing() }
type Backend interface {
	List(context.Context) ([]Recording, error)
	Get(context.Context, string) (Recording, error)
	SaveReview(context.Context, string, Review) error
	Apply(context.Context, string) error
	ApplyReview(context.Context, string, Review) error
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
	Status      Status
	ReviewReady bool
	Writable    bool
	CSRF        string
	Recordings  []Recording
	Recording   *Recording
	Jobs        []Job
	LibraryData []statusCard
	Settings    bool
	History     bool
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
	defer func() {
		if stopper, ok := backend.(browseStopper); ok {
			stopper.StopBrowsing()
		}
	}()
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
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; script-src 'self'; connect-src 'self'; media-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
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
	if r.URL.Path == "/player.js" || r.URL.Path == "/review.js" || r.URL.Path == "/status.js" || r.URL.Path == "/layout.js" || r.URL.Path == "/library.js" || r.URL.Path == "/review-layout.js" {
		b, err := assets.ReadFile("assets" + r.URL.Path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		_, _ = w.Write(b)
		return
	}
	if r.URL.Path == "/status" {
		s.status(w, r)
		return
	}
	if r.URL.Path == "/style.css" || r.URL.Path == "/review-layout.css" {
		b, _ := assets.ReadFile("assets" + r.URL.Path)
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		_, _ = w.Write(b)
		return
	}
	if r.URL.Path == "/help" {
		b, err := manual.Render(true)
		if err != nil {
			http.Error(w, "help unavailable", http.StatusInternalServerError)
			return
		}
		// The manual is a single document with inline CSS and embedded PNGs.
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; frame-ancestors 'none'; base-uri 'none'")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(b)
		return
	}
	p := page{Writable: s.cfg.Writable, CSRF: s.csrf, Status: Status{Phase: "ready", Validated: true}}
	if r.URL.Path == "/" || r.URL.Path == "/recording" {
		if b, ok := s.backend.(interface{ RefreshBrowsing() }); ok {
			b.RefreshBrowsing()
		}
	}
	if b, ok := s.backend.(statusBackend); ok {
		var statusErr error
		p.Status, statusErr = b.Status(r.Context())
		if statusErr != nil {
			http.Error(w, statusErr.Error(), http.StatusInternalServerError)
			return
		}
	}
	var err error
	switch r.URL.Path {
	case "/":
		if _, ok := s.backend.(statusBackend); ok {
			p.Recordings = p.Status.Recordings
		} else {
			p.Recordings, err = s.backend.List(r.Context())
		}
	case "/recording":
		var rec Recording
		rec, err = s.backend.Get(r.Context(), r.URL.Query().Get("id"))
		p.Recording = &rec
		p.ReviewReady = rec.Fingerprint != "" && rec.Status != "loading" && rec.Status != "validating"
	case "/settings":
		p.Settings = true
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
	p.LibraryData = libraryCards(p.Recordings)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = s.templates.ExecuteTemplate(w, "index.html", p)
}

type statusCard struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Artist       string   `json:"artist"`
	RelativePath string   `json:"relativePath"`
	PlaybackOnly bool     `json:"playbackOnly"`
	Status       string   `json:"status"`
	StatusCode   string   `json:"statusCode"`
	LastModified int64    `json:"lastModified,string"`
	Ready        bool     `json:"ready"`
	Action       string   `json:"action"`
	Date         string   `json:"date"`
	Tracks       int      `json:"tracks"`
	Issues       []string `json:"issues"`
}
type statusResponse struct {
	Phase      string       `json:"phase"`
	Validated  bool         `json:"validated"`
	Scanned    int          `json:"scanned"`
	Total      int          `json:"total"`
	Generation uint64       `json:"generation"`
	Error      string       `json:"error,omitempty"`
	Recordings []statusCard `json:"recordings"`
}

func (s *server) status(w http.ResponseWriter, r *http.Request) {
	b, ok := s.backend.(statusBackend)
	if !ok {
		http.NotFound(w, r)
		return
	}
	status, err := b.Status(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	out := statusResponse{Phase: status.Phase, Validated: status.Validated, Scanned: status.Scanned, Total: status.Total, Generation: status.Generation, Error: status.Error, Recordings: make([]statusCard, 0, len(status.Recordings))}
	out.Recordings = libraryCards(status.Recordings)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

func libraryCards(recordings []Recording) []statusCard {
	out := make([]statusCard, 0, len(recordings))
	for _, recording := range recordings {
		out = append(out, statusCard{ID: recording.ID, Title: recordingTitle(recording), Artist: recording.Metadata["ARTIST"], RelativePath: relativeDisplayPath(recording.RelativePath), PlaybackOnly: recording.PlaybackOnly, Status: statusLabel(recording.Status), StatusCode: recording.Status, LastModified: recording.LastModified, Ready: recording.Fingerprint != "" && recording.Status != "loading" && recording.Status != "validating", Action: recordingAction(recording), Date: recording.Metadata["DATE"], Tracks: len(recording.Tracks), Issues: issueSummary(recording)})
	}
	return out
}

func (s *server) mutate(w http.ResponseWriter, r *http.Request) {
	id := r.PostForm.Get("id")
	var err error
	switch r.URL.Path {
	case "/review", "/apply":
		if r.URL.Path == "/apply" && !s.cfg.Writable {
			http.Error(w, "preview mode cannot change media; restart with --write or --copy", http.StatusForbidden)
			return
		}
		review, parseErr := parseReview(r.PostForm)
		if parseErr != nil {
			http.Error(w, parseErr.Error(), http.StatusBadRequest)
			return
		}
		if r.URL.Path == "/review" {
			if !review.Approve {
				http.Error(w, "explicit review acknowledgment required", http.StatusConflict)
				return
			}
			err = s.backend.SaveReview(r.Context(), id, review)
		} else {
			err = s.backend.ApplyReview(r.Context(), id, review)
		}
	case "/undo":
		if !s.cfg.Writable {
			http.Error(w, "preview mode cannot change media; restart with --write or --copy", http.StatusForbidden)
			return
		}
		err = s.backend.Undo(r.Context(), id)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		if r.URL.Path == "/undo" {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		fmt.Fprintf(w, "<!doctype html><html lang=\"en\"><meta charset=\"utf-8\"><title>Review action needs attention</title><h1>Review action needs attention</h1><p>%s</p><p><a href=\"/recording?id=%s\">Return to recording to review or retry</a></p></html>", template.HTMLEscapeString(err.Error()), template.HTMLEscapeString(url.QueryEscape(id)))
		return
	}
	if r.URL.Path == "/undo" {
		http.Redirect(w, r, "/history", http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func parseReview(form url.Values) (Review, error) {
	date := strings.TrimSpace(form.Get("date"))
	dateUnknown := form.Get("date_unknown") == "yes"
	if dateUnknown && date != "" {
		return Review{}, fmt.Errorf("clear the concert date or uncheck Date unknown")
	}
	if !dateUnknown && date == "" {
		return Review{}, fmt.Errorf("enter a concert date or mark Date unknown")
	}
	if !dateUnknown {
		if err := catalog.ValidateDate(date); err != nil {
			return Review{}, fmt.Errorf("concert date must be YYYY, YYYY-MM, or a valid YYYY-MM-DD date")
		}
	}
	review := Review{DateUnknown: dateUnknown, Metadata: map[string]string{"ARTIST": strings.TrimSpace(form.Get("artist")), "ALBUM": strings.TrimSpace(form.Get("album")), "DATE": date}, Artwork: form.Get("artwork")}
	files, titles := form["track_file"], form["track_title"]
	discs, numbers := form["track_disc"], form["track_number"]
	artists := form["track_artist"]
	if len(artists) != 0 && len(artists) != len(files) {
		return Review{}, fmt.Errorf("incomplete track artists")
	}
	if len(files) != len(titles) || len(files) != len(discs) || len(files) != len(numbers) {
		return Review{}, fmt.Errorf("incomplete track mapping")
	}
	for i, file := range files {
		disc, de := strconv.Atoi(discs[i])
		number, ne := strconv.Atoi(numbers[i])
		if de != nil || ne != nil || disc < 1 || number < 1 {
			return Review{}, fmt.Errorf("disc and track numbers must be positive integers")
		}
		artist := ""
		if len(artists) != 0 {
			artist = artists[i]
		}
		review.Tracks = append(review.Tracks, Track{File: file, Title: titles[i], Artist: artist, Disc: disc, Track: number})
	}
	review.Approve = form.Get("approve") == "yes"
	review.Fingerprint = form.Get("fingerprint")
	if review.Fingerprint == "" {
		return Review{}, fmt.Errorf("missing source fingerprint; refresh before reviewing")
	}
	return review, nil
}
