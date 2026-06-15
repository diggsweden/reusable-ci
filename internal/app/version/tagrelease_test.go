// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	appversion "github.com/diggsweden/reusable-ci/internal/app/version"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/testutil/fakeoutputsink"
)

// fakeTagReleaseRepo records the create-once tag operations.
type fakeTagReleaseRepo struct {
	exists      bool
	existsErr   error
	createErr   error
	pushErr     error
	created     string
	createdRef  string
	createdSign bool
	pushed      string
}

func (f *fakeTagReleaseRepo) TagExists(_ context.Context, _ string) (bool, error) {
	return f.exists, f.existsErr
}

func (f *fakeTagReleaseRepo) CreateTag(_ context.Context, tag, ref string, signed bool) error {
	f.created, f.createdRef, f.createdSign = tag, ref, signed

	return f.createErr
}

func (f *fakeTagReleaseRepo) PushTagNoForce(_ context.Context, tag string) error {
	f.pushed = tag

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
		appversion.TagReleaseInput{Tag: "v3.5.7", Signed: true}, fakeoutputsink.New(t), &out)
	if err != nil {
		t.Fatalf("TagRelease: %v", err)
	}

	if repo.created != "v3.5.7" || repo.createdRef != "HEAD" || !repo.createdSign {
		t.Errorf("CreateTag got (%q,%q,%v), want (v3.5.7,HEAD,true)", repo.created, repo.createdRef, repo.createdSign)
	}

	if repo.pushed != "v3.5.7" {
		t.Errorf("PushTagNoForce got %q, want v3.5.7", repo.pushed)
	}

	if res.Tag != "v3.5.7" || res.ReleaseSHA == "" {
		t.Errorf("output = %+v, want tag v3.5.7 + non-empty release-sha", res)
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

func TestTagRelease_RequiresTag(t *testing.T) {
	t.Parallel()

	_, err := appversion.TagRelease(context.Background(), &fakeTagReleaseRepo{},
		appversion.TagReleaseInput{Tag: ""}, nil, &bytes.Buffer{})
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}
}
