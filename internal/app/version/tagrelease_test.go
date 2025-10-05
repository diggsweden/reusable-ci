// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	appversion "github.com/diggsweden/reusable-ci/v3/internal/app/version"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

// fakeTagReleaseRepo records the create-once tag operations.
type fakeTagReleaseRepo struct {
	destinationErr    error
	destinationChecks int
	revRefs           []string
	tagType           string
	typeErr           error
	signatureErr      error
	typeRefs          []string
	verified          []string
	exists            bool
	remoteExists      bool
	localSHA          string
	remoteSHA         string
	remote            string
	remoteTag         string
	created           string
	createdRef        string
	createdSign       bool
	pushed            string
	pushedToken       runcontext.Credential
}

func (f *fakeTagReleaseRepo) CheckOriginPushDestination(_ context.Context) error {
	f.destinationChecks++

	return f.destinationErr
}

func (f *fakeTagReleaseRepo) TagExists(_ context.Context, _ string) (bool, error) {
	return f.exists, nil
}

func (f *fakeTagReleaseRepo) TagSHA(_ context.Context, _ string) (string, error) {
	return f.localSHA, nil
}

func (f *fakeTagReleaseRepo) RemoteTagCommitIfExists(_ context.Context, remote, tag string, _ runcontext.Credential) (string, bool, error) {
	f.remote = remote
	f.remoteTag = tag

	return f.remoteSHA, f.remoteExists, nil
}

func (f *fakeTagReleaseRepo) RemoteTagObjectIfExists(_ context.Context, _, _ string, _ runcontext.Credential) (string, bool, error) {
	return tagObject, f.remoteExists, nil
}

func (f *fakeTagReleaseRepo) CatFileType(_ context.Context, ref string) (string, error) {
	f.typeRefs = append(f.typeRefs, ref)
	if f.tagType != "" {
		return f.tagType, f.typeErr
	}

	return "tag", f.typeErr
}

func (f *fakeTagReleaseRepo) VerifyConfiguredTagSignature(_ context.Context, tag string) error {
	f.verified = append(f.verified, tag)

	return f.signatureErr
}

func (f *fakeTagReleaseRepo) CreateTag(_ context.Context, tag, ref string, signed bool) error {
	f.created, f.createdRef, f.createdSign = tag, ref, signed

	return nil
}

func (f *fakeTagReleaseRepo) PushTagNoForce(_ context.Context, tag string, cred runcontext.Credential) error {
	f.pushed = tag
	f.pushedToken = cred

	return nil
}

func (f *fakeTagReleaseRepo) RevParse(_ context.Context, ref string) (string, error) {
	f.revRefs = append(f.revRefs, ref)
	if ref != "HEAD" {
		return tagObject, nil
	}

	return headSHA, nil
}

const (
	// headSHA is what RevParse answers for HEAD -- the commit a new tag
	// would be created at.
	headSHA = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	// tagObject is what an existing tag object resolves to.
	tagObject = "0123456789abcdef0123456789abcdef01234567"
)

func TestTagRelease_CreatesOnceAtHeadAndPushesNoForce(t *testing.T) {
	t.Parallel()

	repo := &fakeTagReleaseRepo{exists: false}

	var out bytes.Buffer

	res, err := appversion.TagRelease(context.Background(), repo,
		appversion.TagReleaseInput{Tag: "v3.5.7", Signed: true, Token: runcontext.OperatorCredential("bot-token")}, fakeoutputsink.New(t), &out)
	if err != nil {
		t.Fatalf("TagRelease: %v", err)
	}

	if repo.created != "v3.5.7" || repo.createdRef != headSHA || !repo.createdSign {
		t.Errorf("CreateTag got (%q,%q,%v), want (v3.5.7,%s,true)", repo.created, repo.createdRef, repo.createdSign, headSHA)
	}

	if repo.remote != "origin" || repo.remoteTag != "v3.5.7" {
		t.Errorf("RemoteTagExists got (%q,%q), want (origin,v3.5.7)", repo.remote, repo.remoteTag)
	}

	// The push token is threaded through so a credential-free checkout can push.
	// An explicit --token is unrestricted, so it reaches whatever remote the
	// push targets -- that is what "threaded from the input" now means.
	if got := repo.pushedToken.For("https://forge.example/o/r"); got != "bot-token" {
		t.Errorf("push token = %q, want it threaded from the input", got)
	}

	if repo.pushed != "v3.5.7" {
		t.Errorf("PushTagNoForce got %q, want v3.5.7", repo.pushed)
	}

	if res.Tag != "v3.5.7" || res.ReleaseSHA == "" {
		t.Errorf("output = %+v, want tag v3.5.7 + non-empty release-sha", res)
	}

	if !bytes.Contains(out.Bytes(), []byte("Release tag v3.5.7 created at "+headSHA)) {
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

func TestTagRelease_DryRunSkipsMutationsAndNarrates(t *testing.T) {
	t.Parallel()

	repo := &fakeTagReleaseRepo{}

	var out bytes.Buffer

	res, err := appversion.TagRelease(context.Background(), repo,
		appversion.TagReleaseInput{Tag: "v3.5.7", Signed: true, Token: runcontext.OperatorCredential("bot-token"), DryRun: true}, fakeoutputsink.New(t), &out)
	if err != nil {
		t.Fatalf("TagRelease: %v", err)
	}

	// (a) The git mutations are not invoked.
	if repo.created != "" || repo.pushed != "" {
		t.Errorf("dry-run must not create/push tags: created=%q pushed=%q", repo.created, repo.pushed)
	}

	// (b) Each skipped mutation is narrated.
	for _, want := range []string{
		"[dry-run] would create signed tag v3.5.7 at HEAD (" + headSHA + ")",
		"[dry-run] would push tag v3.5.7 to origin (no force)",
	} {
		if !bytes.Contains(out.Bytes(), []byte(want)) {
			t.Errorf("output %q missing narration %q", out.String(), want)
		}
	}

	if bytes.Contains(out.Bytes(), []byte("Release tag v3.5.7 created")) {
		t.Errorf("dry-run must not claim the tag was created: %q", out.String())
	}

	// The preview still reports the SHA the tag would point at.
	if res.Tag != "v3.5.7" || res.ReleaseSHA != headSHA {
		t.Errorf("output = %+v, want tag + HEAD sha", res)
	}
}

func TestTagRelease_DryRunExistingExactLocalTagOnlyNarratesPush(t *testing.T) {
	t.Parallel()

	repo := &fakeTagReleaseRepo{exists: true, localSHA: headSHA}

	var out bytes.Buffer

	_, err := appversion.TagRelease(context.Background(), repo,
		appversion.TagReleaseInput{Tag: "v3.5.7", Signed: true, DryRun: true}, nil, &out)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(out.String(), "would create") || !strings.Contains(out.String(), "would push") {
		t.Errorf("dry-run output = %q, want push only", out.String())
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

func TestTagRelease_ExactRemoteTagIsIdempotent(t *testing.T) {
	t.Parallel()

	repo := &fakeTagReleaseRepo{exists: true, localSHA: headSHA, remoteExists: true, remoteSHA: headSHA}

	var out bytes.Buffer

	res, err := appversion.TagRelease(context.Background(), repo,
		appversion.TagReleaseInput{Tag: "v3.5.7", Signed: true}, fakeoutputsink.New(t), &out)
	if err != nil {
		t.Fatal(err)
	}

	if repo.created != "" || repo.pushed != "" {
		t.Fatalf("exact rerun mutated tag: created=%q pushed=%q", repo.created, repo.pushed)
	}

	if res.ReleaseSHA != headSHA || !strings.Contains(out.String(), "rerun is a no-op") {
		t.Errorf("result=%+v output=%q", res, out.String())
	}
}

func TestTagRelease_ExactLocalTagIsPushedAfterInterruptedRun(t *testing.T) {
	t.Parallel()

	repo := &fakeTagReleaseRepo{exists: true, localSHA: headSHA}

	_, err := appversion.TagRelease(context.Background(), repo,
		appversion.TagReleaseInput{Tag: "v3.5.7", Signed: true}, nil, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}

	if repo.created != "" || repo.pushed != "v3.5.7" {
		t.Errorf("created=%q pushed=%q, want existing local tag pushed only", repo.created, repo.pushed)
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

func TestTagRelease_RefusesUninspectedDestination(t *testing.T) {
	t.Parallel()

	for _, dry := range []bool{false, true} {
		for _, remote := range []string{"upstream", "https://forge.example/owner/repo.git", "origin"} {
			repo := &fakeTagReleaseRepo{destinationErr: errs.ErrValidation}
			sink := fakeoutputsink.New(t)

			var out bytes.Buffer

			res, err := appversion.TagRelease(t.Context(), repo, appversion.TagReleaseInput{Tag: "v1.2.3", Remote: remote, DryRun: dry}, sink, &out)

			want, checks := errs.ErrUsage, 0
			if remote == "origin" {
				want, checks = errs.ErrValidation, 1
			}

			if !errors.Is(err, want) || res != nil || repo.destinationChecks != checks || len(repo.revRefs) != 0 || repo.remoteTag != "" || repo.created != "" || repo.pushed != "" || len(sink.Keys()) != 0 || out.Len() != 0 {
				t.Fatalf("remote=%s dry=%v err=%v checks=%d: unexpected inspection or mutation", remote, dry, err, repo.destinationChecks)
			}
		}
	}
}
