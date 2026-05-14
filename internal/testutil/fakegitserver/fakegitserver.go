// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package fakegitserver wraps httptest.NewServer with a small route
// registration helper for testing the GitHub adapter.
//
// Tests register canned responses per (METHOD, PATH); the server is
// closed automatically via t.Cleanup. Canonical-route helpers (e.g.,
// OnGetRepo, OnCreateRelease) are layered on top of the bare On/OnFunc
// primitives here.
package fakegitserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Request mirrors what the test sees on each invocation.
type Request struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
	Query  map[string][]string
}

// Response is what the registered handler returns.
type Response struct {
	Status int
	Header http.Header
	Body   string
}

// Handler is a registered route's response producer.
type Handler func(req Request) Response

// Server wraps httptest.Server with a route table.
type Server struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	routes   map[string]Handler
	requests []Request
}

// New starts a server on a random port and registers cleanup.
func New(t *testing.T) *Server {
	t.Helper()
	s := &Server{
		t:      t,
		routes: map[string]Handler{},
	}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.srv.Close)
	return s
}

// URL returns the server's base URL.
func (s *Server) URL() string { return s.srv.URL }

// On registers a handler for METHOD PATH. Method is case-insensitive;
// path must match exactly (no wildcards). Re-registering replaces.
func (s *Server) On(method, path string, h Handler) {
	s.t.Helper()
	s.mu.Lock()
	s.routes[key(method, path)] = h
	s.mu.Unlock()
}

// OnGet is a convenience wrapper for On("GET", ...).
func (s *Server) OnGet(path string, h Handler) { s.On("GET", path, h) }

// OnPost is a convenience wrapper for On("POST", ...).
func (s *Server) OnPost(path string, h Handler) { s.On("POST", path, h) }

// Requests returns a snapshot of all received requests, in arrival order.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]Request, len(s.requests))
	copy(cp, s.requests)
	return cp
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()

	// EscapedPath preserves percent-encoding (e.g. "owner%2Frepo") so
	// adapter tests can match exact encoded routes.
	path := r.URL.EscapedPath()

	req := Request{
		Method: r.Method,
		Path:   path,
		Header: r.Header.Clone(),
		Body:   body,
		Query:  r.URL.Query(),
	}

	s.mu.Lock()
	s.requests = append(s.requests, req)
	h, ok := s.routes[key(r.Method, path)]
	s.mu.Unlock()

	if !ok {
		http.Error(w, "no route registered for "+r.Method+" "+path, http.StatusNotFound)
		return
	}

	resp := h(req)
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	if resp.Status == 0 {
		resp.Status = http.StatusOK
	}
	w.WriteHeader(resp.Status)
	_, _ = io.WriteString(w, resp.Body)
}

func key(method, path string) string {
	return upperASCII(method) + " " + path
}

func upperASCII(s string) string {
	out := make([]byte, len(s))
	for i := range len(s) {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 32
		}
		out[i] = c
	}
	return string(out)
}
