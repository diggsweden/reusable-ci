// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package fakegitlabserver mirrors fakegitserver for GitLab API tests.
//
// The contract is intentionally identical to fakegitserver — only the
// canonical-route helpers (OnGetProject, OnCreateRelease, …) differ,
// since the route shapes are different.
package fakegitlabserver

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// Request and Response mirror fakegitserver's. Kept package-local so each
// adapter test imports the right package and can't accidentally cross-wire.
type Request struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
	Query  map[string][]string
}

// Response is one canned HTTP response returned by a registered Handler.
type Response struct {
	Status int
	Header http.Header
	Body   string
}

// Handler turns an inbound Request into a canned Response.
type Handler func(req Request) Response

// Server is a single in-process httptest.Server with method+path routing.
type Server struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	routes   map[string]Handler
	requests []Request
}

// New starts an httptest.Server scoped to t.
func New(t *testing.T) *Server {
	t.Helper()
	s := &Server{t: t, routes: map[string]Handler{}}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.srv.Close)

	return s
}

// URL returns the base URL of the underlying httptest.Server.
func (s *Server) URL() string { return s.srv.URL }

// On registers a handler for an exact (method, path) pair.
func (s *Server) On(method, path string, h Handler) {
	s.t.Helper()
	s.mu.Lock()
	s.routes[upperASCII(method)+" "+path] = h
	s.mu.Unlock()
}

// OnGet registers h as the GET handler for path.
func (s *Server) OnGet(path string, h Handler) { s.On("GET", path, h) }

// OnPost registers h as the POST handler for path.
func (s *Server) OnPost(path string, h Handler) { s.On("POST", path, h) }

// OnPut registers h as the PUT handler for path.
func (s *Server) OnPut(path string, h Handler) { s.On("PUT", path, h) }

// Requests returns a snapshot of every Request the server has handled.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()

	cp := make([]Request, len(s.requests))
	copy(cp, s.requests)

	return cp
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	body, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()

	// EscapedPath preserves percent-encoding (e.g. "owner%2Frepo") so
	// GitLab project-path lookups match the registered route.
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
	h, ok := s.routes[upperASCII(r.Method)+" "+path] //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
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
	// Test-only GitLab API fake: responses are pre-canned fixture JSON,
	// not HTML or user-supplied data. html/template doesn't apply.
	_, _ = io.WriteString(w, resp.Body) // nosemgrep: go.lang.security.audit.xss.no-io-writestring-to-responsewriter.no-io-writestring-to-responsewriter
}

func upperASCII(s string) string {
	out := make([]byte, len(s))
	for i := range len(s) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 32
		}

		out[i] = c
	}

	return string(out)
}
