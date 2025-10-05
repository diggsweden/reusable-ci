// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/httpretry"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

const (
	certificatePEMType = "CERTIFICATE"
	envSSLCAFile       = "SSL_CERT_FILE"
	envCurlCABundle    = "CURL_CA_BUNDLE"
	envGitSSLCAInfo    = "GIT_SSL_CAINFO"
	envRequestsBundle  = "REQUESTS_CA_BUNDLE"
	envNodeExtraCA     = "NODE_EXTRA_CA_CERTS"
)

// CredentialScope selects the single credential authority class available to a
// spawned process. The zero value is the forge/API scope used by ordinary CLI
// scenarios.
type CredentialScope uint8

const (
	// CredentialScopeForge permits only the selected forge/API roots.
	CredentialScopeForge CredentialScope = iota
	// CredentialScopeRegistry permits only OCI and its endpoint token root.
	CredentialScopeRegistry
	// CredentialScopeOIDC permits only the mapped Fulcio origin.
	CredentialScopeOIDC
)

func credentialAuthorities(target Target, scope CredentialScope) ([]string, error) {
	var (
		authorities, expected []string
		err                   error
	)

	switch scope {
	case CredentialScopeForge:
		authorities = target.ForgeAuthorities
		expected, err = contractAuthorities("projected forge credential authority", target.BaseURL())
	case CredentialScopeRegistry:
		authorities = target.RegistryAuthorities
		if target.RegistryOrigin != "" {
			expected, err = contractAuthorities("projected registry credential authority", target.RegistryOrigin, target.BaseURL())
		}
	case CredentialScopeOIDC:
		authorities = target.OIDCAuthorities
		expected, err = oidcAuthorities(target.FulcioURL)
	default:
		return nil, fmt.Errorf("unknown credential authority scope %d: %w", scope, errs.ErrValidation)
	}

	if err != nil {
		return nil, err
	}

	if len(authorities) == 0 || !slices.Equal(authorities, expected) {
		return nil, fmt.Errorf("target credential scope %d does not match its projected contract roots: %w", scope, errs.ErrValidation)
	}

	return authorities, nil
}

type authorityTransport struct {
	allowed map[string]struct{}
	inner   http.RoundTripper
}

func (transport *authorityTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if err := transport.validateURL(request.URL); err != nil {
		return nil, err
	}

	return transport.inner.RoundTrip(request)
}

func (transport *authorityTransport) validateURL(requestURL *url.URL) error {
	key, err := credentialAuthorityKey(requestURL)
	if err != nil {
		return err
	}

	if _, ok := transport.allowed[key]; !ok {
		return fmt.Errorf("credential-bearing request authority %q is not explicitly approved by the target contract: %w",
			requestURL.Host, errs.ErrValidation)
	}

	return nil
}

func newAuthorityTransport(authorities []string, caPEM []byte) (*authorityTransport, error) { //nolint:cyclop // Each authority invariant fails closed.
	allowed := make(map[string]struct{}, len(authorities))
	for _, authority := range authorities {
		parsed, err := url.Parse(authority)
		if err != nil {
			return nil, fmt.Errorf("parse approved credential authority: %w", err)
		}

		if (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, fmt.Errorf("approved credential authority must be an HTTPS origin: %w", errs.ErrValidation)
		}

		key, err := credentialAuthorityKey(parsed)
		if err != nil {
			return nil, err
		}

		allowed[key] = struct{}{}
	}

	if len(allowed) == 0 {
		return nil, fmt.Errorf("target contract approves no credential authorities: %w", errs.ErrValidation)
	}

	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, fmt.Errorf("default HTTP transport has unexpected type: %w", errs.ErrValidation)
	}

	base := defaultTransport.Clone()
	base.Proxy = nil
	base.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}

	if len(caPEM) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("build target CA pool: %w", errs.ErrValidation)
		}

		base.TLSClientConfig.RootCAs = pool
	}

	return &authorityTransport{
		allowed: allowed,
		inner:   base,
	}, nil
}

func credentialAuthorityKey(value *url.URL) (string, error) {
	if value == nil || value.Scheme != "https" || value.User != nil || value.Hostname() == "" {
		return "", fmt.Errorf("credential authority must be an HTTPS origin without userinfo: %w", errs.ErrValidation)
	}

	port := value.Port()
	if port == "" {
		port = "443"
	}

	return net.JoinHostPort(strings.ToLower(value.Hostname()), port), nil
}

// CredentialProxy is a loopback-only CONNECT tunnel constrained to exact HTTPS
// authorities. It never parses or logs tunneled request bytes.
type CredentialProxy struct {
	allowed map[string]struct{}
	dial    func(ctx context.Context, network, address string) (net.Conn, error)
	server  *http.Server
	url     string
	done    chan error
	stop    context.CancelFunc
	mu      sync.Mutex
	closed  bool
	open    map[net.Conn]struct{}
}

// StartCredentialProxy starts a loopback-only credential containment proxy.
func StartCredentialProxy(authorities []string) (*CredentialProxy, error) {
	proxy, err := newCredentialProxy(authorities, (&net.Dialer{Timeout: 10 * time.Second}).DialContext)
	if err != nil {
		return nil, err
	}

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for credential containment proxy: %w", err)
	}

	address, ok := listener.Addr().(*net.TCPAddr)
	if !ok || !address.IP.IsLoopback() {
		_ = listener.Close()

		return nil, fmt.Errorf("credential containment proxy did not bind loopback: %w", errs.ErrValidation)
	}

	proxy.serve(listener)

	return proxy, nil
}

// newCredentialProxy validates the approved authorities and prepares a proxy
// that reaches them through dial. Its lifetime context is the base of every
// request, so Close also ends dials still in flight.
func newCredentialProxy(authorities []string, dial func(context.Context, string, string) (net.Conn, error)) (*CredentialProxy, error) {
	allowed := make(map[string]struct{}, len(authorities))
	for _, authority := range authorities {
		parsed, err := url.Parse(authority)
		if err != nil {
			return nil, fmt.Errorf("parse approved credential authority: %w", err)
		}

		if (parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, fmt.Errorf("approved credential authority must be an HTTPS origin: %w", errs.ErrValidation)
		}

		key, err := credentialAuthorityKey(parsed)
		if err != nil {
			return nil, err
		}

		allowed[key] = struct{}{}
	}

	if len(allowed) == 0 {
		return nil, fmt.Errorf("target contract approves no credential authorities: %w", errs.ErrValidation)
	}

	lifetime, stop := context.WithCancel(context.Background())

	proxy := &CredentialProxy{
		allowed: allowed,
		dial:    dial,
		done:    make(chan error, 1),
		stop:    stop,
		open:    make(map[net.Conn]struct{}),
	}

	proxy.server = &http.Server{
		Handler:           proxy,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       15 * time.Second,
		ErrorLog:          log.New(io.Discard, "", 0),
		BaseContext:       func(net.Listener) context.Context { return lifetime },
	}

	return proxy, nil
}

// URL returns the loopback HTTP proxy URL.
func (proxy *CredentialProxy) URL() string { return proxy.url }

// Done reports the terminal serve result.
func (proxy *CredentialProxy) Done() <-chan error { return proxy.done }

// Close stops acceptance and closes every active tunnel within ctx's bound.
// Tunnels are closed first and no tunnel can be registered afterwards, so one
// whose dial or hijack completes during shutdown is refused rather than left
// running; cancelling the lifetime ends dials that are still waiting.
func (proxy *CredentialProxy) Close(ctx context.Context) error {
	proxy.mu.Lock()

	proxy.closed = true

	connections := make([]net.Conn, 0, len(proxy.open))
	for connection := range proxy.open {
		connections = append(connections, connection)
	}

	proxy.mu.Unlock()

	proxy.stop()

	for _, connection := range connections {
		_ = connection.Close()
	}

	shutdownErr := proxy.server.Shutdown(ctx)

	select {
	case serveErr := <-proxy.done:
		return errors.Join(shutdownErr, serveErr)
	case <-ctx.Done():
		_ = proxy.server.Close()

		return errors.Join(shutdownErr, ctx.Err())
	}
}

func (proxy *CredentialProxy) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodConnect {
		http.Error(response, "only HTTPS CONNECT is permitted", http.StatusMethodNotAllowed)

		return
	}

	parsed, err := url.Parse("https://" + request.Host)
	if err != nil {
		http.Error(response, "invalid CONNECT authority", http.StatusBadRequest)

		return
	}

	authority, err := credentialAuthorityKey(parsed)
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)

		return
	}

	if _, approved := proxy.allowed[authority]; !approved {
		http.Error(response, "credential authority is not explicitly approved", http.StatusForbidden)

		return
	}

	upstream, err := proxy.dial(request.Context(), "tcp", authority)
	if err != nil {
		http.Error(response, "approved authority is unavailable", http.StatusBadGateway)

		return
	}

	hijacker, ok := response.(http.Hijacker)
	if !ok {
		_ = upstream.Close()

		http.Error(response, "CONNECT tunnelling is unavailable", http.StatusInternalServerError)

		return
	}

	downstream, buffered, err := hijacker.Hijack()
	if err != nil {
		_ = upstream.Close()

		return
	}

	proxy.tunnel(downstream, buffered, upstream)
}

func (proxy *CredentialProxy) serve(listener net.Listener) {
	proxy.url = "http://" + listener.Addr().String()

	go func() {
		serveErr := proxy.server.Serve(listener)
		if errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = nil
		}

		proxy.done <- serveErr

		close(proxy.done)
	}()
}

// tunnel copies bytes both ways until either side ends, then closes both. A
// tunnel that cannot register because Close has begun is closed before the
// client is told it is established.
func (proxy *CredentialProxy) tunnel(downstream net.Conn, buffered *bufio.ReadWriter, upstream net.Conn) {
	defer func() {
		proxy.untrack(downstream, upstream)

		_ = downstream.Close()
		_ = upstream.Close()
	}()

	if !proxy.track(downstream, upstream) {
		return
	}

	if _, err := buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}

	if err := buffered.Flush(); err != nil {
		return
	}

	done := make(chan struct{}, 2)

	go func() {
		_, _ = io.Copy(upstream, buffered)

		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(downstream, upstream)

		done <- struct{}{}
	}()

	<-done
}

// track registers a tunnel's connections for Close, refusing once Close has
// begun so a tunnel cannot start after its connections were collected.
func (proxy *CredentialProxy) track(connections ...net.Conn) bool {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()

	if proxy.closed {
		return false
	}

	for _, connection := range connections {
		proxy.open[connection] = struct{}{}
	}

	return true
}

func (proxy *CredentialProxy) untrack(connections ...net.Conn) {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()

	for _, connection := range connections {
		delete(proxy.open, connection)
	}
}

func targetHTTPClient(target Target, timeout time.Duration) (*http.Client, error) {
	caPEM, err := loadTargetCAPEM(target)
	if err != nil {
		return nil, err
	}

	authorities, err := credentialAuthorities(target, CredentialScopeForge)
	if err != nil {
		return nil, err
	}

	transport, err := newAuthorityTransport(authorities, caPEM)
	if err != nil {
		return nil, err
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: httpretry.NewTransport(httpretry.Config{Inner: transport}),
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			return transport.validateURL(request.URL)
		},
	}, nil
}

// HTTPClient returns a target-CA client constrained to explicit contract
// authorities. It is exported for the independent live oracle package.
func HTTPClient(tb TB, target Target, timeout time.Duration) *http.Client {
	tb.Helper()

	client, err := targetHTTPClient(target, timeout)
	if err != nil {
		tb.Fatalf("livetest: configure target HTTP client: %v", err)
	}

	return client
}

func registryRemoteOptions(target Target) ([]remote.Option, error) {
	transport, err := targetRoundTripper(target)
	if err != nil {
		return nil, err
	}

	return []remote.Option{
		remote.WithAuth(&authn.Basic{Username: target.CredentialUsername, Password: target.Token}),
		remote.WithTransport(transport),
	}, nil
}

func targetRoundTripper(target Target) (http.RoundTripper, error) {
	caPEM, err := loadTargetCAPEM(target)
	if err != nil {
		return nil, err
	}

	authorities, err := credentialAuthorities(target, CredentialScopeRegistry)
	if err != nil {
		return nil, err
	}

	return newAuthorityTransport(authorities, caPEM)
}

func loadTargetCAPEM(target Target) ([]byte, error) {
	if target.CAFile == "" {
		return nil, nil
	}

	if len(target.caPEM) > 0 {
		return bytes.Clone(target.caPEM), nil
	}

	captured, err := captureTargetCA(target)
	if err != nil {
		return nil, err
	}

	return bytes.Clone(captured.caPEM), nil
}

func captureTargetCA(target Target) (Target, error) {
	if target.CAFile == "" {
		return target, nil
	}

	body, facts, err := readCAFileBound(target.CAFile, nil)
	if err != nil {
		return Target{}, err
	}

	if target.CAFacts == "" || facts != target.CAFacts {
		return Target{}, fmt.Errorf("frozen ca_file does not match preflight facts: %w", errs.ErrValidation)
	}

	target.caPEM = bytes.Clone(body)

	return target, nil
}

func verifyTargetCAPath(target Target) error {
	if target.CAFile == "" {
		return nil
	}

	body, facts, err := readCAFileBound(target.CAFile, nil)
	if err != nil {
		return err
	}

	if target.CAFacts == "" || facts != target.CAFacts ||
		(len(target.caPEM) > 0 && !bytes.Equal(body, target.caPEM)) {
		return fmt.Errorf("frozen ca_file changed after preflight: %w", errs.ErrValidation)
	}

	return nil
}

func validateCertificatePEM(body []byte) error {
	rest := bytes.TrimSpace(body)
	certificates := 0

	for len(rest) > 0 {
		end := leadingCertificateBlockEnd(rest)
		if end == 0 {
			return fmt.Errorf("ca_file must contain only PEM CERTIFICATE blocks: %w", errs.ErrValidation)
		}

		block, remaining := pem.Decode(rest[:end])
		if block == nil || block.Type != certificatePEMType || len(block.Headers) != 0 || len(remaining) != 0 {
			return fmt.Errorf("ca_file must contain only PEM CERTIFICATE blocks: %w", errs.ErrValidation)
		}

		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return fmt.Errorf("ca_file contains an invalid certificate: %w: %w", err, errs.ErrValidation)
		}

		certificates++
		rest = bytes.TrimSpace(rest[end:])
	}

	if certificates == 0 {
		return fmt.Errorf("ca_file contains no certificates: %w", errs.ErrValidation)
	}

	return nil
}

// leadingCertificateBlockEnd returns the length of the CERTIFICATE block rest
// starts with, or 0 when it does not start with exactly one BEGIN before its
// END marker. Only that first block is decoded afterwards: pem.Decode skips a
// malformed leading block, even within the slice, so a stray BEGIN line before
// a certificate would otherwise pass.
func leadingCertificateBlockEnd(rest []byte) int {
	const endMarker = "-----END CERTIFICATE-----"

	end := bytes.Index(rest, []byte(endMarker)) + len(endMarker)
	if !bytes.HasPrefix(rest, []byte("-----BEGIN CERTIFICATE-----")) || end < len(endMarker) ||
		bytes.Count(rest[:end], []byte("-----BEGIN ")) != 1 {
		return 0
	}

	return end
}

func targetTrustEnvironment(target Target) map[string]string {
	if target.CAFile == "" {
		return map[string]string{}
	}

	return map[string]string{
		envSSLCAFile:      target.CAFile,
		envCurlCABundle:   target.CAFile,
		envGitSSLCAInfo:   target.CAFile,
		envRequestsBundle: target.CAFile,
		envNodeExtraCA:    target.CAFile,
	}
}

// ToolEnvironment returns a closed, target-scoped environment for a helper that
// can read target credentials. HTTPS is forced through a per-test proxy that
// permits only authorities validated from the frozen contract.
func ToolEnvironment(tb TB, target Target, scope CredentialScope, extra map[string]string) []string {
	tb.Helper()
	requireAccepted(tb, target)

	if err := verifyTargetCAPath(target); err != nil {
		tb.Fatalf("livetest: refusing changed frozen ca_file: %v", err)
	}

	values := targetTrustEnvironment(target)
	for key, value := range credentialProxyEnvironment(tb, target, scope) {
		values[key] = value
	}

	for _, key := range []string{"PATH", "HOME", "TMPDIR"} {
		if value := os.Getenv(key); value != "" {
			values[key] = value
		}
	}

	for key, value := range extra {
		if isTrustEnvironmentKey(key) {
			tb.Fatalf("livetest: helper environment may not override containment variable %s", key)
		}

		values[key] = value
	}

	environment := make([]string, 0, len(values))
	for key, value := range values {
		environment = append(environment, key+"="+value)
	}

	sort.Strings(environment)

	return environment
}

func credentialProxyEnvironment(tb TB, target Target, scope CredentialScope) map[string]string {
	tb.Helper()

	authorities, err := credentialAuthorities(target, scope)
	if err != nil {
		tb.Fatalf("livetest: select credential containment scope: %v", err)
	}

	proxy, err := StartCredentialProxy(authorities)
	if err != nil {
		tb.Fatalf("livetest: start credential containment proxy: %v", err)
	}

	tb.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_ = proxy.Close(ctx)
	})

	return map[string]string{
		"HTTP_PROXY":  proxy.URL(),
		"HTTPS_PROXY": proxy.URL(),
		"http_proxy":  proxy.URL(),
		"https_proxy": proxy.URL(),
		"NO_PROXY":    "",
		"no_proxy":    "",
	}
}

func isTrustEnvironmentKey(key string) bool {
	switch key {
	case envSSLCAFile, envCurlCABundle, envGitSSLCAInfo, envRequestsBundle, envNodeExtraCA,
		"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy":
		return true
	default:
		return false
	}
}
