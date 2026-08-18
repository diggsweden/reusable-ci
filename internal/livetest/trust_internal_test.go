// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

func serverCertificatePEM(server *httptest.Server) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
}

func independentCAPEM(t *testing.T) []byte {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "unrelated test CA"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}

	certificate, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate})
}

func withCAFacts(t *testing.T, target Target) Target {
	t.Helper()

	_, facts, err := readCAFileBound(target.CAFile, nil)
	if err != nil {
		t.Fatal(err)
	}

	target.CAFacts = facts

	return target
}

func TestRegistryTransport_RejectsChallengeRealmExfiltration(t *testing.T) {
	t.Parallel()

	registry := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("WWW-Authenticate", `Bearer realm="https://fulcio.example.invalid/token",service="evil"`)
		response.WriteHeader(http.StatusUnauthorized)
	}))
	defer registry.Close()

	caFile := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caFile, serverCertificatePEM(registry), 0o600); err != nil {
		t.Fatal(err)
	}

	target := withCAFacts(t, Target{
		CAFile:              caFile,
		Host:                strings.TrimPrefix(registry.URL, "https://"),
		CredentialUsername:  "fixture-user",
		Token:               "fixture-token",
		RegistryOrigin:      registry.URL,
		RegistryAuthorities: []string{registry.URL},
		OIDCAuthorities:     []string{"https://fulcio.example.invalid"},
	})

	options, err := registryRemoteOptions(target)
	if err != nil {
		t.Fatal(err)
	}

	repository, err := name.NewRepository(strings.TrimPrefix(registry.URL, "https://") + "/fixture")
	if err != nil {
		t.Fatal(err)
	}

	_, err = remote.List(repository, options...)
	if err == nil || !strings.Contains(err.Error(), "not explicitly approved") {
		t.Fatalf("challenge to separately approved Fulcio authority result = %v", err)
	}
}

func TestRegistryTransport_RejectsRedirectToSeparatelyApprovedFulcioAuthority(t *testing.T) {
	t.Parallel()

	var fulcioRequests atomic.Int32

	fulcio := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		fulcioRequests.Add(1)
	}))
	defer fulcio.Close()

	registry := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Location", fulcio.URL+"/collect")
		response.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer registry.Close()

	caFile := filepath.Join(t.TempDir(), "ca.pem")

	caPEM := append(serverCertificatePEM(registry), serverCertificatePEM(fulcio)...)
	if err := os.WriteFile(caFile, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}

	target := withCAFacts(t, Target{
		CAFile:              caFile,
		Host:                strings.TrimPrefix(registry.URL, "https://"),
		RegistryOrigin:      registry.URL,
		RegistryAuthorities: []string{registry.URL},
		OIDCAuthorities:     []string{fulcio.URL},
	})

	transport, err := targetRoundTripper(target)
	if err != nil {
		t.Fatal(err)
	}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, registry.URL+"/v2/", nil)
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("Authorization", "Basic fixture-secret")

	response, requestErr := (&http.Client{Transport: transport, Timeout: time.Second}).Do(request)
	if response != nil {
		_ = response.Body.Close()
	}

	if requestErr == nil || !strings.Contains(requestErr.Error(), "not explicitly approved") {
		t.Fatalf("redirect to separately approved Fulcio authority result = %v", requestErr)
	}

	if fulcioRequests.Load() != 0 {
		t.Fatalf("Fulcio authority received %d registry credential request(s)", fulcioRequests.Load())
	}
}

func TestCredentialTransport_RejectsRedirectBeforeRequest(t *testing.T) {
	t.Parallel()

	var exfiltrationRequests atomic.Int32

	evil := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		exfiltrationRequests.Add(1)
	}))
	defer evil.Close()

	registry := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Location", evil.URL+"/collect")
		response.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer registry.Close()

	transport, err := newAuthorityTransport([]string{registry.URL},
		append(serverCertificatePEM(registry), serverCertificatePEM(evil)...))
	if err != nil {
		t.Fatal(err)
	}

	client := &http.Client{Transport: transport, Timeout: time.Second}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, registry.URL+"/v2/", nil)
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("Authorization", "Basic fixture-secret")

	response, requestErr := client.Do(request)
	if response != nil {
		_ = response.Body.Close()
	}

	if requestErr == nil || !strings.Contains(requestErr.Error(), "not explicitly approved") {
		t.Fatalf("redirect result = %v", requestErr)
	}

	if exfiltrationRequests.Load() != 0 {
		t.Fatalf("unapproved redirect received %d request(s)", exfiltrationRequests.Load())
	}
}

func TestCredentialProxy_RejectsRedirectAuthorityBeforeRequest(t *testing.T) {
	t.Parallel()

	var exfiltrationRequests atomic.Int32

	evil := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		exfiltrationRequests.Add(1)
	}))
	defer evil.Close()

	registry := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Location", evil.URL+"/collect")
		response.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer registry.Close()

	proxy, err := StartCredentialProxy([]string{registry.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		_ = proxy.Close(ctx)
	}()

	proxyURL, err := url.Parse(proxy.URL())
	if err != nil {
		t.Fatal(err)
	}

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(append(serverCertificatePEM(registry), serverCertificatePEM(evil)...))

	defaultTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatal("default HTTP transport has unexpected type")
	}

	transport := defaultTransport.Clone()
	transport.Proxy = http.ProxyURL(proxyURL)
	transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, registry.URL+"/v2/", nil)
	if err != nil {
		t.Fatal(err)
	}

	request.Header.Set("Authorization", "Basic fixture-secret")

	response, requestErr := (&http.Client{Transport: transport, Timeout: time.Second}).Do(request)
	if response != nil {
		_ = response.Body.Close()
	}

	if requestErr == nil || !strings.Contains(requestErr.Error(), "Forbidden") {
		t.Fatalf("proxied redirect result = %v", requestErr)
	}

	if exfiltrationRequests.Load() != 0 {
		t.Fatalf("unapproved redirect received %d request(s)", exfiltrationRequests.Load())
	}
}

func TestTargetHTTPClient_UsesSelectedCAInsteadOfAmbientTrust(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	dir := t.TempDir()

	caFile := filepath.Join(dir, "target-ca.pem")
	if err := os.WriteFile(caFile, serverCertificatePEM(server), 0o600); err != nil {
		t.Fatal(err)
	}

	target := withCAFacts(t, Target{
		Host:             strings.TrimPrefix(server.URL, "https://"),
		CAFile:           caFile,
		ForgeAuthorities: []string{server.URL},
	})

	client, err := targetHTTPClient(target, time.Second)
	if err != nil {
		t.Fatal(err)
	}

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatal(err)
	}

	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("selected CA was not used: %v", err)
	}

	_ = response.Body.Close()

	if writeErr := os.WriteFile(caFile, independentCAPEM(t), 0o400); writeErr != nil {
		t.Fatal(writeErr)
	}

	if _, err = targetHTTPClient(target, time.Second); err == nil || !strings.Contains(err.Error(), "does not match preflight facts") {
		t.Fatalf("changed frozen CA result = %v", err)
	}
}

func TestTrustLabCA_Base64EncodesValidatedCertificates(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.NotFoundHandler())
	defer server.Close()

	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, serverCertificatePEM(server), 0o400); err != nil {
		t.Fatal(err)
	}

	target := withCAFacts(t, Target{CAFile: path, accepted: true})

	shell := TrustLabCA(target)
	if strings.Contains(shell, "BEGIN CERTIFICATE") || strings.Contains(shell, "LAB_CA_PEM") ||
		!strings.Contains(shell, "base64 -d") {
		t.Fatalf("unsafe CA shell rendering:\n%s", shell)
	}
}

func TestTrustLabCA_RejectsShellTextAfterCertificate(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.NotFoundHandler())
	defer server.Close()

	path := filepath.Join(t.TempDir(), "ca.pem")

	body := append(serverCertificatePEM(server), []byte("\nLAB_CA_PEM\ntouch /tmp/exfiltrated\n")...)
	if err := os.WriteFile(path, body, 0o400); err != nil {
		t.Fatal(err)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("TrustLabCA accepted non-certificate shell text")
		}
	}()

	_ = TrustLabCA(Target{CAFile: path, CAFacts: "invalid-content", accepted: true})
}

func TestFrozenCA_ReplacementIsRejectedBeforePathUse(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "ca.pem")

	original := independentCAPEM(t)
	if err := os.WriteFile(path, original, 0o400); err != nil {
		t.Fatal(err)
	}

	target := withCAFacts(t, Target{CAFile: path})

	captured, err := captureTargetCA(target)
	if err != nil {
		t.Fatal(err)
	}

	if renameErr := os.Rename(path, path+".old"); renameErr != nil {
		t.Fatal(renameErr)
	}

	if writeErr := os.WriteFile(path, independentCAPEM(t), 0o400); writeErr != nil {
		t.Fatal(writeErr)
	}

	if verifyErr := verifyTargetCAPath(captured); verifyErr == nil || !strings.Contains(verifyErr.Error(), "changed after preflight") {
		t.Fatalf("replaced frozen CA result = %v", verifyErr)
	}

	body, err := loadTargetCAPEM(captured)
	if err != nil || string(body) != string(original) {
		t.Fatalf("captured CA changed: err=%v", err)
	}
}

func TestCredentialProxy_RejectsNonOriginAuthorities(t *testing.T) {
	t.Parallel()

	for _, authority := range []string{
		"https://fulcio.example.test/api",
		"https://fulcio.example.test?query=value",
		"https://fulcio.example.test#fragment",
	} {
		if proxy, err := StartCredentialProxy([]string{authority}); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_ = proxy.Close(ctx)

			cancel()
			t.Errorf("accepted non-origin authority %q", authority)
		}
	}
}

func TestCLIEnv_UsesFrozenTargetTrustNotAmbientCA(t *testing.T) {
	t.Setenv("SSL_CERT_FILE", "/ambient/ca.pem")

	target := Target{Forge: "forgejo", Host: "forgejo.compose.forgelab", Owner: "owner", CAFile: "/frozen/ca.pem"}
	environment := cliEnv(target, "repo")

	joined := strings.Join(environment, "\n")
	for _, key := range []string{"SSL_CERT_FILE", "CURL_CA_BUNDLE", "GIT_SSL_CAINFO", "REQUESTS_CA_BUNDLE", "NODE_EXTRA_CA_CERTS"} {
		if !strings.Contains(joined, key+"=/frozen/ca.pem") {
			t.Errorf("%s does not use frozen CA: %s", key, joined)
		}
	}

	if strings.Contains(joined, "/ambient/ca.pem") {
		t.Fatalf("ambient CA leaked into product environment: %s", joined)
	}
}
