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

type Response struct {
	Status int
	Header http.Header
	Body   string
}

type Handler func(req Request) Response

type Server struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	routes   map[string]Handler
	requests []Request
}

func New(t *testing.T) *Server {
	t.Helper()
	s := &Server{t: t, routes: map[string]Handler{}}
	s.srv = httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *Server) URL() string { return s.srv.URL }

func (s *Server) On(method, path string, h Handler) {
	s.t.Helper()
	s.mu.Lock()
	s.routes[upperASCII(method)+" "+path] = h
	s.mu.Unlock()
}

func (s *Server) OnGet(path string, h Handler)  { s.On("GET", path, h) }
func (s *Server) OnPost(path string, h Handler) { s.On("POST", path, h) }
func (s *Server) OnPut(path string, h Handler)  { s.On("PUT", path, h) }

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
	h, ok := s.routes[upperASCII(r.Method)+" "+path]
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
