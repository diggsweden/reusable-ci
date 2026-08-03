// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package ci_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	appci "github.com/diggsweden/reusable-ci/v3/internal/app/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

type fakeRefGit struct {
	out string
	err error
}

func (f fakeRefGit) Run(_ context.Context, _ ...string) (string, error) { return f.out, f.err }

func TestResolveRef_UsesRemoteSHAWhenFound(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	var out bytes.Buffer

	got, err := appci.ResolveRef(context.Background(), fakeRefGit{out: "abc123\trefs/tags/v1"}, sink, &out, appci.ResolveRefInput{RemoteURL: "https://example.com/o/r", Ref: "v1"})
	if err != nil {
		t.Fatal(err)
	}

	if got != "abc123" || sink.Single("sha") != "abc123" {
		t.Errorf("got %q output %q", got, sink.Single("sha"))
	}
}

func TestResolveRef_RequiresRemoteURL(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	_, err := appci.ResolveRef(context.Background(), fakeRefGit{}, sink, nil, appci.ResolveRefInput{Ref: "v1"})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("expected ErrUsage, got %v", err)
	}
}

func TestResolveRef_UsesPeeledTagSHAWhenFound(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	got, err := appci.ResolveRef(context.Background(), fakeRefGit{out: "tagsha\trefs/tags/v1\ncommitsha\trefs/tags/v1^{}"}, sink, nil, appci.ResolveRefInput{RemoteURL: "https://example.com/o/r", Ref: "v1"})
	if err != nil {
		t.Fatal(err)
	}

	if got != "commitsha" || sink.Single("sha") != "commitsha" {
		t.Errorf("got %q output %q", got, sink.Single("sha"))
	}
}

func TestResolveRef_PassesThroughSHAWhenNoRemoteRow(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)
	sha := "0123456789abcdef0123456789abcdef01234567"

	got, err := appci.ResolveRef(context.Background(), fakeRefGit{}, sink, nil, appci.ResolveRefInput{RemoteURL: "https://example.com/o/r", Ref: sha})
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

	_, err := appci.ResolveRef(context.Background(), fakeRefGit{}, sink, nil, appci.ResolveRefInput{RemoteURL: "https://example.com/o/r", Ref: "deadbeef"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveRef_FailsWhenLookupErrors(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	_, err := appci.ResolveRef(context.Background(), fakeRefGit{err: errors.New("offline")}, sink, nil, appci.ResolveRefInput{RemoteURL: "https://example.com/o/r", Ref: "v1"}) //nolint:err113 // test mock error
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveRef_FailsWhenNamedRefNotFound(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	_, err := appci.ResolveRef(context.Background(), fakeRefGit{}, sink, nil, appci.ResolveRefInput{RemoteURL: "https://example.com/o/r", Ref: "v1"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveRef_RejectsUnsafeOutputKey(t *testing.T) {
	t.Parallel()
	sink := fakeoutputsink.New(t)

	_, err := appci.ResolveRef(context.Background(), fakeRefGit{out: "abc123\trefs/tags/v1"}, sink, nil, appci.ResolveRefInput{
		RemoteURL: "https://example.com/o/r",
		Ref:       "v1",
		OutputKey: "bad\nkey",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
