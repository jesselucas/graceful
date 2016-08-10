package graceful

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// Option function is used to customize Server struct
type Option func(*Server)

// Server setups a graceful shutdown server
type Server struct {
	http.Server
	Context         context.Context
	GracefulTimeout time.Duration
	listener        net.Listener
	wg              *sync.WaitGroup
	mu              sync.Mutex                  // guards closed and conns
	conns           map[net.Conn]http.ConnState // keeps track of idle connections
	closed          bool
}

// NewServer constructor
func NewServer(addr string, opts ...Option) *Server {
	s := new(Server)
	s.Addr = addr
	s.GracefulTimeout = 5 * time.Second
	s.wg = new(sync.WaitGroup)

	for _, opt := range opts {
		opt(s)
	}

	// Track connection state
	s.connState()

	// If there is a context stop server when done
	if s.Context != nil {
		go func() {
			select {
			case <-s.Context.Done():
				s.Stop()
			}
		}()
	}

	// Returns configured HTTP server.
	return s
}

// ListenAndServe serves either https or http depending on TLSConfig
func (s *Server) ListenAndServe() error {
	addr := s.Addr
	if addr == "" {
		if s.TLSConfig != nil {
			addr = ":https"
		} else {
			addr = ":http"
		}
	}

	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	if s.TLSConfig != nil {
		l = tls.NewListener(l, s.TLSConfig)
	}

	return s.Serve(l)
}

// Stop stops s gracefully (or forcefully after timeout) and
// closes its listener.
func (s *Server) Stop() error {
	if s.closed {
		return errors.New("Server has been closed")
	}

	// Make sure a listener was set
	if s.listener != nil {
		// Close the listener to stop all new connections
		if err := s.listener.Close(); err != nil {
			return err
		}
	}

	s.SetKeepAlivesEnabled(false)

	// Wait for any connections to finish
	wait := make(chan struct{})
	go func() {
		defer close(wait)
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()

		s.wg.Wait()
	}()

	// We block until all connections are closed or the connTimeout happens
	// We wait the full length of the timeout to forcefully close any connections
	// to avoid having a ConnState transition race
	select {
	case <-time.After(s.GracefulTimeout):
		// graceful timed out so forcefully close all connections
		s.mu.Lock()
		for c, st := range s.conns {
			// Force close any idle and new connections.
			if st == http.StateIdle || st == http.StateNew {
				c.Close()
			}
		}
		s.mu.Unlock()

		return nil
	case <-wait:
		return nil
	}
}

// connState setups the ConnState tracking hook to know which connections are idle
func (s *Server) connState() {
	// Set our ConnState to track idle connections
	s.ConnState = func(c net.Conn, cs http.ConnState) {
		fmt.Println(cs, c)
		s.mu.Lock()
		defer s.mu.Unlock()

		switch cs {
		case http.StateNew:
			// New connections increment the WaitGroup and are added the the conns dictionary
			s.wg.Add(1)
			if s.conns == nil {
				s.conns = make(map[net.Conn]http.ConnState)
			}
			s.conns[c] = cs
		case http.StateActive:
			// Only update status to StateActive if it's in the conns dictionary
			if _, ok := s.conns[c]; ok {
				s.conns[c] = cs
			}
		case http.StateIdle:
			// Only update status to StateIdle if it's in the conns dictionary
			if _, ok := s.conns[c]; ok {
				s.conns[c] = cs
			}

			// If we've already closed then we need to close this connection.
			// We don't allow connections to become idle after server is closed
			if s.closed {
				c.Close()
			}
		case http.StateHijacked, http.StateClosed:
			// If the connection is hijacked or closed we forget it
			s.forgetConn(c)
		}
	}
}

// forgetConn removes c from conns and decrements WaitGroup
func (s *Server) forgetConn(c net.Conn) {
	if _, ok := s.conns[c]; ok {
		delete(s.conns, c)
		s.wg.Done()
	}
}
