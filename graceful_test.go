package graceful

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net"
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
}

func TestServerConnClose(t *testing.T) {
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

	// Make a request
	client := http.Client{}
	_, err := client.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}

	// Make sure there is only 1 connection
	s.mu.Lock()
	if len(s.conns) < 1 {
		t.Fatal("Should have 1 connections")
	}
	s.mu.Unlock()

	// Stop the server
	s.Stop()

	// Make sure there are zero connections
	s.mu.Lock()
	if len(s.conns) > 0 {
		t.Fatal("Should have 0 connections")
	}
	s.mu.Unlock()

	// Try to connect to the server after it's closed
	_, err = client.Get(ts.URL)

	// This should always error because new connections are not allowed
	if err == nil {
		t.Fatal("Should not accept new connections after close")
	}
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

func TestServerCloseBlocking(t *testing.T) {
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "Hello, client")
	}))
	defer ts.Close()

	setHandler := func(s *Server) {
		s.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "hello")
		})
	}

	// // Set short GracefulTimeout for testing
	setTimeout := func(s *Server) {
		s.GracefulTimeout = 1 * time.Millisecond
	}
	s := NewServer("", setHandler, setTimeout)

	// Set the test server config to the Server
	ts.Config = &s.Server
	ts.Start()

	// Set listener
	s.listener = ts.Listener

	dial := func() net.Conn {
		c, err := net.Dial("tcp", ts.Listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		return c
	}

	// Dial to open a StateNew but don't send anything
	cnew := dial()
	defer cnew.Close()

	// Dial another connection but idle after a request to have StateIdle
	cidle := dial()
	defer cidle.Close()
	cidle.Write([]byte("HEAD / HTTP/1.1\r\nHost: foo\r\n\r\n"))
	_, err := http.ReadResponse(bufio.NewReader(cidle), nil)
	if err != nil {
		t.Fatal(err)
	}

	// Make sure we don't block forever.
	s.Stop()

	// Make sure there are zero connections
	s.mu.Lock()
	if len(s.conns) > 0 {
		t.Fatal("Should have 0 connections")
	}
	s.mu.Unlock()
}

func TestListenAndServe(t *testing.T) {
	s := NewServer("")
	stopc := make(chan struct{})
	errc := make(chan error, 1)
	go func() { errc <- s.ListenAndServe() }()
	go func() { errc <- s.Stop(); close(stopc) }()

	select {
	case err := <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-stopc:
		return
	}
}

func TestListenAndServeTLS(t *testing.T) {
	s := NewServer("")
	stopc := make(chan struct{})
	errc := make(chan error, 1)

	s.TLSConfig = &tls.Config{}
	go func() { errc <- s.ListenAndServe() }()
	go func() { errc <- s.Stop(); close(stopc) }()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-stopc:
		return
	}
}

func TestListenAndServeTLSAddress(t *testing.T) {
	s := NewServer("127.0.0.1:9000")
	stopc := make(chan struct{})
	errc := make(chan error, 1)

	s.TLSConfig = &tls.Config{}
	go func() { errc <- s.ListenAndServe() }()
	go func() { errc <- s.Stop(); close(stopc) }()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-stopc:
		return
	}
}
