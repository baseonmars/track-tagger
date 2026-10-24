package webui

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestServeListenerDrainsActiveRequests(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	served := make(chan error, 1)
	go func() {
		served <- ServeListener(ctx, l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(entered)
			<-release
			_, _ = io.WriteString(w, "complete")
		}))
	}()
	requested := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Get("http://" + l.Addr().String())
		if err == nil {
			_, err = io.ReadAll(resp.Body)
			resp.Body.Close()
		}
		requested <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("request did not enter handler")
	}
	cancel()
	select {
	case err := <-served:
		close(release)
		t.Fatalf("server returned before handler finished: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-requested; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-served:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not finish draining")
	}
}
