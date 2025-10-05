// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

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
	"slices"
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

// Server routes method+path pairs to canned responses, entirely in memory.
//
// It used to be an httptest.Server, which meant every test that touched the
// GitLab adapter bound a loopback port and ran a listener goroutine. Nothing
// needed the network: the adapter takes an *http.Client, so the same routing
// and the same recorded requests work through a RoundTripper, and the test no
// longer depends on a port being available or on the OS scheduling a server.
//
// The recorded Request keeps method, path, header, body and query, so what a
// test can assert is unchanged.
type Server struct {
	t        *testing.T
	mu       sync.Mutex
	routes   map[string]Handler
	requests []Request
}

// New returns a Server scoped to t. Nothing is started and no port is bound.
func New(t *testing.T) *Server {
	t.Helper()

	return &Server{t: t, routes: map[string]Handler{}}
}

// URL is the synthetic base every request is addressed to. It resolves to
// nothing: requests reach this Server through Client, not through the network,
// and a request that escaped to the real network would fail to resolve rather
// than silently reaching somewhere.
func (s *Server) URL() string { return "https://gitlab.invalid" }

// Client returns an *http.Client whose transport dispatches to this Server.
func (s *Server) Client() *http.Client {
	return &http.Client{Transport: roundTripFunc(s.roundTrip)}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

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
	for index, req := range s.requests {
		cp[index] = req.clone()
	}

	return cp
}

// roundTrip runs the same routing the HTTP handler did, against an in-memory
// recorder, and returns the recorded result as a response.
func (s *Server) roundTrip(r *http.Request) (*http.Response, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	// A client request built with a nil body has Body == nil; a server handler
	// is always given one. Normalising here keeps handle identical to what it
	// was when a real listener fed it.
	if r.Body == nil {
		r = r.Clone(r.Context())
		r.Body = http.NoBody
	}

	recorder := httptest.NewRecorder()
	s.handle(recorder, r)

	resp := recorder.Result()
	resp.Request = r

	return resp, nil
}

func (r Request) clone() Request {
	r.Header = r.Header.Clone()
	r.Body = slices.Clone(r.Body)
	// Query has the same map-of-slices shape as Header, including nil values.
	r.Query = http.Header(r.Query).Clone()

	return r
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
	s.requests = append(s.requests, req.clone())
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
