// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// The tunnel lifecycle runs through the real http.Server, CONNECT parsing and
// hijacking, but over net.Pipe: the proxy serves an in-memory listener and
// dials in-memory upstreams, so no socket is opened.

const lifecycleBound = 5 * time.Second

// pipeListener accepts connections created by its own dial.
type pipeListener struct {
	connections chan net.Conn
	done        chan struct{}
	once        sync.Once
}

func newPipeListener() *pipeListener {
	return &pipeListener{connections: make(chan net.Conn), done: make(chan struct{})}
}

func (listener *pipeListener) Accept() (net.Conn, error) {
	select {
	case connection := <-listener.connections:
		return connection, nil
	case <-listener.done:
		return nil, net.ErrClosed
	}
}

func (listener *pipeListener) Close() error {
	listener.once.Do(func() { close(listener.done) })

	return nil
}

func (*pipeListener) Addr() net.Addr { return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)} }

func (listener *pipeListener) dial() (net.Conn, error) {
	client, server := net.Pipe()

	select {
	case listener.connections <- server:
		return client, nil
	case <-listener.done:
		return nil, net.ErrClosed
	}
}

// upstreams records every dial and answers each tunnel from its own end of a
// pipe, prefixing what it echoes with the authority it was dialled for.
type upstreams struct {
	mu     sync.Mutex
	dialed []string
	ends   []net.Conn
}

func (fake *upstreams) dial(_ context.Context, _, address string) (net.Conn, error) {
	proxySide, upstreamSide := net.Pipe()

	fake.mu.Lock()
	fake.dialed = append(fake.dialed, address)
	fake.ends = append(fake.ends, upstreamSide)
	fake.mu.Unlock()

	go func() {
		reader := bufio.NewReader(upstreamSide)

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}

			if _, err = fmt.Fprintf(upstreamSide, "%s|%s", address, line); err != nil {
				return
			}
		}
	}()

	return proxySide, nil
}

func (fake *upstreams) snapshot() ([]string, []net.Conn) {
	fake.mu.Lock()
	defer fake.mu.Unlock()

	return slices.Clone(fake.dialed), slices.Clone(fake.ends)
}

func serveInMemory(t *testing.T, dial func(context.Context, string, string) (net.Conn, error), authorities ...string) (*CredentialProxy, *pipeListener) {
	t.Helper()

	proxy, err := newCredentialProxy(authorities, dial)
	if err != nil {
		t.Fatal(err)
	}

	listener := newPipeListener()
	proxy.serve(listener)

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), lifecycleBound)
		defer cancel()

		_ = proxy.Close(ctx)
	})

	return proxy, listener
}

// sendConnect opens a client connection and sends a CONNECT for authority.
func sendConnect(t *testing.T, listener *pipeListener, authority string) (net.Conn, *bufio.Reader) {
	t.Helper()

	client, err := listener.dial()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = client.Close() })

	if err = client.SetDeadline(time.Now().Add(lifecycleBound)); err != nil {
		t.Fatal(err)
	}

	if _, err = fmt.Fprintf(client, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", authority, authority); err != nil {
		t.Fatal(err)
	}

	return client, bufio.NewReader(client)
}

func requireEstablished(t *testing.T, reader *bufio.Reader, authority string) {
	t.Helper()

	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("CONNECT %s: %v", authority, err)
	}

	_ = response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT %s: status %d, want an established tunnel", authority, response.StatusCode)
	}
}

// requireEnded fails unless the connection's peer has closed it. A pipe whose
// peer already closed refuses a new deadline, and its read ends at once.
func requireEnded(t *testing.T, what string, connection net.Conn, reader io.Reader) {
	t.Helper()

	_ = connection.SetReadDeadline(time.Now().Add(lifecycleBound))

	if _, err := io.ReadAll(reader); err != nil {
		t.Errorf("%s was not closed: %v", what, err)
	}
}

// TestCredentialProxy_ConcurrentTunnelsCarryOnlyTheirOwnBytes holds three
// tunnels open at once, two to the same authority, and exchanges data through
// them in the reverse of the order they were opened. Each tunnel receives only
// the answer of the upstream dialled for its own authority, and the proxy
// dials exactly the requested authorities.
func TestCredentialProxy_ConcurrentTunnelsCarryOnlyTheirOwnBytes(t *testing.T) {
	t.Parallel()

	fake := &upstreams{}
	_, listener := serveInMemory(t, fake.dial, "https://registry.example.test", "https://token.example.test:8443")

	requested := []string{"registry.example.test:443", "token.example.test:8443", "registry.example.test:443"}

	type tunnel struct {
		client net.Conn
		reader *bufio.Reader
	}

	tunnels := make([]tunnel, len(requested))
	for index, authority := range requested {
		client, reader := sendConnect(t, listener, authority)
		requireEstablished(t, reader, authority)

		tunnels[index] = tunnel{client: client, reader: reader}
	}

	for index := len(tunnels) - 1; index >= 0; index-- {
		payload := fmt.Sprintf("payload-%d\n", index)

		if _, err := io.WriteString(tunnels[index].client, payload); err != nil {
			t.Fatal(err)
		}

		got, err := tunnels[index].reader.ReadString('\n')
		if want := requested[index] + "|" + payload; err != nil || got != want {
			t.Errorf("tunnel %d received %q, %v; want %q", index, got, err, want)
		}
	}

	dialed, _ := fake.snapshot()
	slices.Sort(dialed)

	want := slices.Clone(requested)
	slices.Sort(want)

	if !slices.Equal(dialed, want) {
		t.Errorf("dialled %q, want exactly %q", dialed, want)
	}
}

// TestCredentialProxy_CloseEndsEveryTunnelWithinItsBound closes the proxy with
// two idle tunnels open and a third CONNECT whose authority never answers the
// dial. Close returns without error inside its bound, both ends of every tunnel
// are closed, the waiting CONNECT is not established, Done is closed, and the
// listener accepts nothing more.
func TestCredentialProxy_CloseEndsEveryTunnelWithinItsBound(t *testing.T) {
	t.Parallel()

	const unanswered = "silent.example.test:443"

	fake := &upstreams{}
	dialing := make(chan struct{})

	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != unanswered {
			return fake.dial(ctx, network, address)
		}

		close(dialing)
		<-ctx.Done()

		return nil, ctx.Err()
	}

	proxy, listener := serveInMemory(t, dial, "https://registry.example.test", "https://"+unanswered)

	clients := make([]net.Conn, 0, 2)
	readers := make([]*bufio.Reader, 0, 2)

	for range 2 {
		client, reader := sendConnect(t, listener, "registry.example.test:443")
		requireEstablished(t, reader, "registry.example.test:443")

		clients = append(clients, client)
		readers = append(readers, reader)
	}

	waiting, waitingReader := sendConnect(t, listener, unanswered)

	<-dialing

	// A pipe has no buffer, so the refused CONNECT's answer is read while Close
	// runs rather than after it.
	established := make(chan bool, 1)

	go func() {
		response, err := http.ReadResponse(waitingReader, &http.Request{Method: http.MethodConnect})
		if err != nil {
			established <- false

			return
		}

		_ = response.Body.Close()
		established <- response.StatusCode == http.StatusOK
	}()

	ctx, cancel := context.WithTimeout(context.Background(), lifecycleBound)
	defer cancel()

	if err := proxy.Close(ctx); err != nil {
		t.Fatalf("Close within its bound: %v", err)
	}

	for index, client := range clients {
		requireEnded(t, fmt.Sprintf("tunnel %d client side", index), client, readers[index])
	}

	_, ends := fake.snapshot()
	for index, end := range ends {
		requireEnded(t, fmt.Sprintf("tunnel %d upstream side", index), end, end)
	}

	if <-established {
		t.Error("a CONNECT waiting on its dial was established by Close")
	}

	requireEnded(t, "the waiting CONNECT", waiting, waitingReader)

	// Close consumed the serve result it returned, so Done is left closed.
	select {
	case serveErr, open := <-proxy.Done():
		if open {
			t.Errorf("Done delivered a second serve result after Close: %v", serveErr)
		}
	case <-time.After(lifecycleBound):
		t.Error("Done was not closed after Close")
	}

	if _, err := listener.dial(); err == nil {
		t.Error("the listener accepted a connection after Close")
	}
}

// TestCredentialProxy_ATunnelCompletingDuringCloseIsNeverEstablished lets a
// dial finish only once Close has begun, which is where a hijack lands after
// Close collected the open tunnels. That tunnel must be refused: the client is
// never told it is established and both of its ends are closed.
func TestCredentialProxy_ATunnelCompletingDuringCloseIsNeverEstablished(t *testing.T) {
	t.Parallel()

	fake := &upstreams{}
	dialing := make(chan struct{})

	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		close(dialing)
		<-ctx.Done()

		return fake.dial(context.WithoutCancel(ctx), network, address)
	}

	proxy, listener := serveInMemory(t, dial, "https://registry.example.test")

	client, reader := sendConnect(t, listener, "registry.example.test:443")

	<-dialing

	ctx, cancel := context.WithTimeout(context.Background(), lifecycleBound)
	defer cancel()

	if err := proxy.Close(ctx); err != nil {
		t.Fatalf("Close within its bound: %v", err)
	}

	if status, err := reader.ReadString('\n'); strings.Contains(status, "200") {
		t.Fatalf("a tunnel completing during Close was established: %q, %v", status, err)
	}

	requireEnded(t, "the late tunnel's client side", client, reader)

	_, ends := fake.snapshot()
	if len(ends) != 1 {
		t.Fatalf("dialled %d upstreams, want the one late dial", len(ends))
	}

	requireEnded(t, "the late tunnel's upstream side", ends[0], ends[0])
}
