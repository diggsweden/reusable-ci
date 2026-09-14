// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package platform_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	appplatform "github.com/diggsweden/reusable-ci/v3/internal/app/platform"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

// Named so the propagation test asserts identity rather than a message.
var errRefLookupOffline = errors.New("ls-remote: offline")

type fakeRefGit struct {
	out string
	err error
}

func (f fakeRefGit) Run(_ context.Context, _ ...string) (string, error) { return f.out, f.err }

func TestResolveRef_UsesRemoteSHAWhenFound(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	var out bytes.Buffer

	sha := strings.Repeat("a", 40)

	got, err := appplatform.ResolveRef(context.Background(), fakeRefGit{out: sha + "\trefs/tags/v1"}, sink, &out, appplatform.ResolveRefInput{RemoteURL: "https://example.com/o/r", Ref: "v1"})
	if err != nil {
		t.Fatal(err)
	}

	if got != sha || sink.Single("sha") != sha {
		t.Errorf("got %q output %q", got, sink.Single("sha"))
	}
}

func TestResolveRef_RequiresRemoteURL(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	_, err := appplatform.ResolveRef(context.Background(), fakeRefGit{}, sink, nil, appplatform.ResolveRefInput{Ref: "v1"})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("expected ErrUsage, got %v", err)
	}
}

func TestResolveRef_UsesPeeledTagSHAWhenFound(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	sha := strings.Repeat("b", 40)

	got, err := appplatform.ResolveRef(context.Background(), fakeRefGit{out: strings.Repeat("a", 40) + "\trefs/tags/v1\n" + sha + "\trefs/tags/v1^{}"}, sink, nil, appplatform.ResolveRefInput{RemoteURL: "https://example.com/o/r", Ref: "v1"})
	if err != nil {
		t.Fatal(err)
	}

	if got != sha || sink.Single("sha") != sha {
		t.Errorf("got %q output %q", got, sink.Single("sha"))
	}
}

func TestResolveRef_PassesThroughSHAWhenNoRemoteRow(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	sha := "0123456789abcdef0123456789abcdef01234567"

	got, err := appplatform.ResolveRef(context.Background(), fakeRefGit{}, sink, nil, appplatform.ResolveRefInput{RemoteURL: "https://example.com/o/r", Ref: sha})
	if err != nil {
		t.Fatal(err)
	}

	if got != sha || sink.Single("sha") != sha {
		t.Errorf("got %q output %q", got, sink.Single("sha"))
	}
}

func TestResolveRef_FailsShortSHAWhenNoRemoteRow(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	// A short SHA cannot be fetched and is not a name the remote knows, so
	// it is refused rather than passed through like a full one.
	_, err := appplatform.ResolveRef(context.Background(), fakeRefGit{}, sink, nil, appplatform.ResolveRefInput{RemoteURL: "https://example.com/o/r", Ref: "deadbeef"})
	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig", err)
	}
}

func TestResolveRef_FailsWhenLookupErrors(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	_, err := appplatform.ResolveRef(context.Background(), fakeRefGit{err: errRefLookupOffline}, sink, nil, appplatform.ResolveRefInput{RemoteURL: "https://example.com/o/r", Ref: "v1"})
	if !errors.Is(err, errRefLookupOffline) {
		t.Fatalf("err = %v, want git's own failure to survive wrapping", err)
	}
}

func TestResolveRef_FailsWhenNamedRefNotFound(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	_, err := appplatform.ResolveRef(context.Background(), fakeRefGit{}, sink, nil, appplatform.ResolveRefInput{RemoteURL: "https://example.com/o/r", Ref: "v1"})
	if !errors.Is(err, errs.ErrInvalidConfig) {
		t.Fatalf("err = %v, want ErrInvalidConfig — the ref names nothing on the remote", err)
	}
}

func TestResolveRef_RejectsUnsafeOutputKey(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	_, err := appplatform.ResolveRef(context.Background(), fakeRefGit{out: "abc123\trefs/tags/v1"}, sink, nil, appplatform.ResolveRefInput{
		RemoteURL: "https://example.com/o/r",
		Ref:       "v1",
		OutputKey: "bad\nkey",
	})

	// The key reaches a line-oriented output file, so a newline in it would
	// forge a further output.
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	if got := sink.Keys(); len(got) != 0 {
		t.Errorf("emitted %q under an unusable key", got)
	}
}
