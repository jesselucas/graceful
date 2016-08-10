package graceful

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStop(t *testing.T) {
	// Create Server
	s := NewServer(":1234")

	if err := s.Stop(); err != nil {
		t.Fatal("Server errored while trying to Stop", err)
	}

	if err := s.Stop(); err == nil {
		t.Fatal("Stop should error")
	}
}

func TestServer(t *testing.T) {
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Hello, client")
	}))
	defer ts.Close()

	setHandler := func(s *Server) {
		s.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "hello")
		})
	}

	// Set short GracefulTimeout for testing
	setTimeout := func(s *Server) {
		s.GracefulTimeout = 1 * time.Millisecond
	}
	s := NewServer("", setHandler, setTimeout)

	// Set the test server config to the Server
	ts.Config = &s.Server
	ts.Start()

	// Set listener
	s.listener = ts.Listener

	client := http.Client{}
	res, err := client.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}

	got, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}

	if string(got) != "hello" {
		t.Errorf("got %q, want hello", string(got))
	}

	// Make sure there is only 1 connection
	s.mu.Lock()
	if len(s.conns) < 1 {
		t.Fatal("Should have 1 connections")
	}
	s.mu.Unlock()

	// Stop the server
	s.Stop()

	// Try to connect to the server after it's closed
	_, err = client.Get(ts.URL)

	// This should always error because new connections are not allowed
	if err == nil {
		t.Fatal("Should not accept new connections after close")
	}

	// Make sure there are zero connections
	s.mu.Lock()
	if len(s.conns) < 0 {
		t.Fatal("Should have 0 connections")
	}
	s.mu.Unlock()
}

func TestServerWithContext(t *testing.T) {
	// Setup context to stop server in .5 second
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Create Option function to set server context
	setContext := func(s *Server) {
		s.Context = ctx
	}
	s := NewServer("", setContext)

	select {
	case <-time.After(s.GracefulTimeout + (1 * time.Millisecond)):
		t.Fatal("Context timeout did not happen")
	case <-ctx.Done():
		if err := ctx.Err(); err != nil {
			if err != context.DeadlineExceeded {
				t.Fatal("Context deadline should have exceeded")
			}
		}
		return
	}
}
