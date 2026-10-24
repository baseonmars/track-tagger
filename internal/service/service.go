// Package service owns the review server, watcher, and their shared storage.
package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"track-tagger/internal/app"
	"track-tagger/internal/webui"
)

type Options struct {
	app.Options
	Addr         string
	Remote       bool
	Token        string
	Poll         bool
	Reconcile    time.Duration
	FallbackPort bool
}

type Session struct {
	url    string
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	err    error
}

// Start validates configuration and binds before starting any watcher work.
func Start(ctx context.Context, opts Options, log func(string)) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.Settle < 0 || opts.Reconcile < 0 {
		return nil, errors.New("invalid time interval")
	}
	if opts.Addr == "" {
		opts.Addr = "127.0.0.1:8787"
	}
	e, err := app.New(opts.Options)
	if err != nil {
		return nil, err
	}
	cfg := webui.Config{Addr: opts.Addr, Remote: opts.Remote, Token: opts.Token, Writable: e.Mode != app.DryRun}
	h, err := webui.New(cfg, app.WebBackend{E: e})
	if err != nil {
		return nil, err
	}
	listener, err := listen(opts)
	if err != nil {
		return nil, err
	}
	if err = e.OpenStorage(); err != nil {
		_ = listener.Close()
		_ = e.Close()
		return nil, err
	}
	return startSession(ctx, opts, e, listener, h, log), nil
}

func startSession(ctx context.Context, opts Options, e *app.Engine, listener net.Listener, h http.Handler, log func(string)) *Session {
	ctx, cancel := context.WithCancel(ctx)
	s := &Session{url: "http://" + listener.Addr().String(), cancel: cancel, done: make(chan struct{})}
	results := make(chan error, 2)
	go func() { results <- webui.ServeListener(ctx, listener, h) }()
	go func() { results <- e.Watch(ctx, opts.Poll, opts.Reconcile, log) }()
	go func() {
		first := <-results
		cancel()
		second := <-results
		// Both request handlers and watcher transactions have finished now.
		closeErr := e.Close()
		s.mu.Lock()
		s.err = errors.Join(normalError(first), normalError(second), closeErr)
		s.mu.Unlock()
		close(s.done)
	}()
	return s
}

func listen(opts Options) (net.Listener, error) {
	listener, err := net.Listen("tcp", opts.Addr)
	if err == nil || !opts.FallbackPort || !addressInUse(err) {
		return listener, err
	}
	host, _, splitErr := net.SplitHostPort(opts.Addr)
	if splitErr != nil || host != "localhost" && (net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback()) {
		return nil, fmt.Errorf("fallback requires a loopback address: %w", err)
	}
	return net.Listen("tcp", net.JoinHostPort(host, "0"))
}

func normalError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return nil
	}
	return err
}

func (s *Session) URL() string { return s.url }

// Stop waits without a forced timeout so active media work retains its storage.
// It is safe to call concurrently or more than once.
func (s *Session) Stop() { s.cancel(); <-s.done }

func (s *Session) Done() <-chan struct{} { return s.done }

// Err returns the final lifecycle error after Done is closed.
func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}
