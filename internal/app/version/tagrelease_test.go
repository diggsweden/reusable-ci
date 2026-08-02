// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

// fakeTagReleaseRepo records the create-once tag operations.
type fakeTagReleaseRepo struct {
	exists       bool
	existsErr    error
	remoteExists bool
	remoteErr    error
	remote       string
	remoteTag    string
	createErr    error
	pushErr      error
	created      string
	createdRef   string
	createdSign  bool
	pushed       string
	pushedToken  string
}

func (f *fakeTagReleaseRepo) TagExists(_ context.Context, _ string) (bool, error) {
	return f.exists, f.existsErr
}

func (f *fakeTagReleaseRepo) RemoteTagExists(_ context.Context, remote, tag string) (bool, error) {
	f.remote = remote
	f.remoteTag = tag

	return f.remoteExists, f.remoteErr
}

func (f *fakeTagReleaseRepo) CreateTag(_ context.Context, tag, ref string, signed bool) error {
	f.created, f.createdRef, f.createdSign = tag, ref, signed

	return f.createErr
}

func (f *fakeTagReleaseRepo) PushTagNoForce(_ context.Context, tag, token string) error {
	f.pushed = tag
	f.pushedToken = token

	return f.pushErr
}

func (f *fakeTagReleaseRepo) RevParse(_ context.Context, _ string) (string, error) {
	return "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef", nil
}

func TestTagRelease_CreatesOnceAtHeadAndPushesNoForce(t *testing.T) {
	t.Parallel()

	repo := &fakeTagReleaseRepo{exists: false}

	var out bytes.Buffer

	res, err := appversion.TagRelease(context.Background(), repo,
		appversion.TagReleaseInput{Tag: "v3.5.7", Signed: true, Token: "bot-token"}, fakeoutputsink.New(t), &out)
	if err != nil {
		t.Fatalf("TagRelease: %v", err)
	}

	if repo.created != "v3.5.7" || repo.createdRef != "HEAD" || !repo.createdSign {
		t.Errorf("CreateTag got (%q,%q,%v), want (v3.5.7,HEAD,true)", repo.created, repo.createdRef, repo.createdSign)
	}

	if repo.remote != "origin" || repo.remoteTag != "v3.5.7" {
		t.Errorf("RemoteTagExists got (%q,%q), want (origin,v3.5.7)", repo.remote, repo.remoteTag)
	}

	// The push token is threaded through so a credential-free checkout can push.
	if repo.pushedToken != "bot-token" {
		t.Errorf("push token = %q, want it threaded from the input", repo.pushedToken)
	}

	if repo.pushed != "v3.5.7" {
		t.Errorf("PushTagNoForce got %q, want v3.5.7", repo.pushed)
	}

	if res.Tag != "v3.5.7" || res.ReleaseSHA == "" {
		t.Errorf("output = %+v, want tag v3.5.7 + non-empty release-sha", res)
	}

	if !bytes.Contains(out.Bytes(), []byte("Release tag v3.5.7 created at deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")) {
		t.Errorf("stdout = %q, want legacy success line", out.String())
	}
}

func TestTagRelease_CreatesUnsignedAnnotatedTagWhenRequested(t *testing.T) {
	t.Parallel()

	repo := &fakeTagReleaseRepo{exists: false}

	_, err := appversion.TagRelease(context.Background(), repo,
		appversion.TagReleaseInput{Tag: "v3.5.7", Signed: false}, fakeoutputsink.New(t), &bytes.Buffer{})
	if err != nil {
		t.Fatalf("TagRelease: %v", err)
	}

	if repo.createdSign {
		t.Error("CreateTag signed = true, want false")
	}
}

func TestTagRelease_RefusesWhenTagExists(t *testing.T) {
	t.Parallel()

	repo := &fakeTagReleaseRepo{exists: true}

	_, err := appversion.TagRelease(context.Background(), repo,
		appversion.TagReleaseInput{Tag: "v3.5.7", Signed: true}, nil, &bytes.Buffer{})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation (create-once refusal)", err)
	}

	if repo.created != "" {
		t.Errorf("must not create a tag when it already exists; created %q", repo.created)
	}
}

func TestTagRelease_RefusesWhenRemoteTagExists(t *testing.T) {
	t.Parallel()

	repo := &fakeTagReleaseRepo{remoteExists: true}

	_, err := appversion.TagRelease(context.Background(), repo,
		appversion.TagReleaseInput{Tag: "v3.5.7", Signed: true}, nil, &bytes.Buffer{})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation (remote create-once refusal)", err)
	}

	if repo.created != "" {
		t.Errorf("must not create a tag when it already exists on origin; created %q", repo.created)
	}
}

func TestTagRelease_RequiresTag(t *testing.T) {
	t.Parallel()

	_, err := appversion.TagRelease(context.Background(), &fakeTagReleaseRepo{},
		appversion.TagReleaseInput{Tag: ""}, nil, &bytes.Buffer{})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}
}

func TestTagRelease_RequiresStableSingleLineTag(t *testing.T) {
	t.Parallel()

	for _, tag := range []string{"release-request/v1.2.3", "v1.2.3-rc1", "v1.2.3+build", "v1.2.3\nnext"} {
		t.Run(tag, func(t *testing.T) {
			t.Parallel()

			repo := &fakeTagReleaseRepo{}

			_, err := appversion.TagRelease(context.Background(), repo,
				appversion.TagReleaseInput{Tag: tag}, nil, &bytes.Buffer{})
			if !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}

			if repo.created != "" || repo.remoteTag != "" {
				t.Fatalf("invalid tag should not reach git operations: repo=%+v", repo)
			}
		})
	}
}
