// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/git"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeprovider"
)

// stubGit returns canned git answers for every call the orchestrator
// makes. Behavior here is deliberately permissive; tests that need a
// specific failure inject errors.
type stubGit struct {
	uniqueTagsErr error
	tags          []string
	revisions     map[string]string
	tagSHA        string
	tagExists     bool
	verifyOK      bool
	verifyCall    func(context.Context, string, []byte)
}

func (s stubGit) RevParse(_ context.Context, ref string) (string, error) {
	if revision := s.revisions[ref]; revision != "" {
		return revision, nil
	}

	return "abc1234", nil
}
func (s stubGit) TagsPointingAt(_ context.Context, _ string) ([]string, error) {
	if s.uniqueTagsErr != nil {
		return nil, s.uniqueTagsErr
	}

	if s.tags != nil {
		return s.tags, nil
	}

	return []string{"v1.0.0"}, nil //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
}
func (s stubGit) IsAncestor(_ context.Context, _, _ string) (bool, error) { return true, nil }
func (s stubGit) CatFileType(_ context.Context, _ string) (string, error) { return "tag", nil } //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
func (s stubGit) CatFileTag(_ context.Context, _ string) (string, error) {
	return "object 0000\ntype commit\ntag v1.0.0\ntagger x <x@y> 0 +0000\n\nmsg\n-----BEGIN PGP SIGNATURE-----\n", nil
}
func (s stubGit) VerifyTagSignature(ctx context.Context, tag string, armor []byte) (string, string, bool, error) {
	if s.verifyCall != nil {
		s.verifyCall(ctx, tag, armor)
	}

	if len(armor) == 0 || !s.verifyOK {
		return "", "", false, nil
	}

	return "x <x@y>", "ABCDEFABCDEFABCDEFABCDEFABCDEFABCDEFABCD", true, nil
}

func (s stubGit) VerifyTagSSHAgainstAllowedSigners(_ context.Context, _, _ string) (bool, string, error) {
	// Every fixture in this file carries a GPG armor block, so the SSH path
	// is never taken. It used to answer ok=true, which meant a change that
	// routed these tags through SSH verification would have been authorised
	// by the stub and the tests would still have passed. Fail loudly instead.
	return false, "", errUnexpectedSSHVerification
}

// errUnexpectedSSHVerification marks the stub being asked something the
// fixtures in this file cannot answer.
var errUnexpectedSSHVerification = errors.New("stubGit: SSH signature verification is not exercised by these GPG fixtures") //nolint:err113 // test fixture sentinel.
func (s stubGit) TaggerInfo(_ context.Context, _ string) (git.TaggerInfo, error) {
	return git.TaggerInfo{Tagger: "x <x@y>", Date: "2026-01-01"}, nil
}
func (s stubGit) TagMessage(_ context.Context, _ string) (string, error) { return "msg", nil }
func (s stubGit) TagSHA(_ context.Context, _ string) (string, error)     { return s.tagSHA, nil }
func (s stubGit) TagExists(_ context.Context, _ string) (bool, error)    { return s.tagExists, nil }

func TestPrerequisites_NonTagRefSkipsTagChecks(t *testing.T) {
	fp := fakeprovider.New(t).WithPlatform(provider.ForgeGitHub)
	result, err := appvalidate.Prerequisites(context.Background(), appvalidate.PrerequisitesDeps{
		GitRepo:  stubGit{verifyOK: true, tagSHA: "abc"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Provider: fp,
	}, io.Discard, output.Annotator{}, appvalidate.PrerequisitesInput{ //nolint:gosec // synthetic provider credential; no real provider or key is used.
		RefType:      "branch",
		Tag:          "main",
		Ref:          "refs/heads/main",
		ReleaseToken: "ghp_dummy",  //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Repository:   "owner/repo", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	})

	// RefType("branch") errors with guidance — that's by design.
	// The test asserts skips are recorded for tag-only checks.
	if !errors.Is(err, errs.ErrValidation) {
		t.Errorf("err = %v, want ErrValidation from the ref-type validator", err)
	}

	skipped := map[string]bool{}

	for _, c := range result.Checks {
		if c.Skipped {
			skipped[c.Name] = true
		}
	}

	for _, want := range []string{"tag-format", "tag-uniqueness", "tag-commit", "tag-signature"} {
		if !skipped[want] {
			t.Errorf("expected %s to be skipped on non-tag ref", want)
		}
	}
}

func TestPrerequisites_TagRefRunsAllTagChecks(t *testing.T) {
	t.Chdir(t.TempDir())
	fp := fakeprovider.New(t).WithPlatform(provider.ForgeGitHub).WithBotPermissions(provider.BotPermissions{UserAccessible: true, RepoAccessible: true, BranchesAccessible: true})

	var calls atomic.Int32

	result, err := appvalidate.Prerequisites(context.Background(), appvalidate.PrerequisitesDeps{
		GitRepo: stubGit{verifyOK: true, tagSHA: "abc1234", verifyCall: func(_ context.Context, tag string, key []byte) {
			calls.Add(1)

			if tag != "v1.0.0" || string(key) != "owned-test-key" {
				t.Errorf("unexpected signature request tag=%q key=%q", tag, key)
			}
		}},
		Provider: fp,
	}, io.Discard, output.Annotator{}, appvalidate.PrerequisitesInput{ //nolint:gosec // synthetic key and token are passed only to recording fakes.
		RefType:             "tag",
		Tag:                 "v1.0.0",
		Ref:                 "refs/tags/v1.0.0", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		ReleaseToken:        "github_pat_AAAA",
		Repository:          "owner/repo",
		ReleaseGPGPublicKey: "owned-test-key",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"ref-type", "tag-format", "tag-uniqueness", "tag-commit", "tag-signature", "final-tag-rerun", "gpg-public-key", "release-token", "bot-permissions", "maven-central", "cargo", "jvm-reproducibility"}

	names := make([]string, 0, len(result.Checks))
	for _, check := range result.Checks {
		names = append(names, check.Name)
		if slices.Contains([]string{"ref-type", "tag-format", "tag-uniqueness", "tag-commit", "tag-signature", "release-token", "bot-permissions"}, check.Name) && (check.Skipped || check.Err != nil) {
			t.Fatalf("applicable check did not pass: %+v", check)
		}
	}

	if !slices.Equal(names, want) || calls.Load() != 1 {
		t.Fatalf("checks=%v signature calls=%d", names, calls.Load())
	}
}

func TestPrerequisites_ExactFinalTagRerunAllowsExpectedTagCollision(t *testing.T) {
	t.Parallel()

	const (
		requestTag = "release-request/v1.0.0"
		finalTag   = "v1.0.0"
		revision   = "abc1234"
	)

	fp := fakeprovider.New(t).
		WithPlatform(provider.ForgeGitHub).
		WithBotPermissions(provider.BotPermissions{
			UserAccessible:     true,
			RepoAccessible:     true,
			BranchesAccessible: true,
		})

	result, err := appvalidate.Prerequisites(context.Background(), appvalidate.PrerequisitesDeps{
		GitRepo: stubGit{
			tags:      []string{requestTag, finalTag},
			tagSHA:    revision,
			tagExists: true,
			verifyOK:  true,
			// A bare name resolves elsewhere here, as it would when a branch
			// shares the tag's name; only the qualified tag ref is the release.
			revisions: map[string]string{finalTag + "^{commit}": "0bad0bad0b"},
		},
		Provider: fp,
	}, io.Discard, output.Annotator{}, appvalidate.PrerequisitesInput{ //nolint:gosec // fake token and key markers exercise validation without secrets.
		RefType:             "tag",
		Tag:                 requestTag,
		Ref:                 "refs/tags/" + requestTag,
		Branch:              "main",
		ReleaseToken:        "github_pat_AAAA",
		Repository:          "owner/repo",
		ReleaseGPGPublicKey: "test-key",
	})
	if err != nil {
		t.Fatalf("exact rerun prerequisites failed: %v", err)
	}

	if result.ExistingReleaseSHA != revision {
		t.Fatalf("ExistingReleaseSHA = %q, want verified final-tag commit %q", result.ExistingReleaseSHA, revision)
	}

	for _, check := range result.Checks {
		if check.Name == "final-tag-rerun" {
			if check.Skipped || check.Err != nil {
				t.Fatalf("final-tag-rerun = %+v, want successful check", check)
			}

			return
		}
	}

	t.Fatal("final-tag-rerun check missing")
}

func TestPrerequisites_PolicyFlagsGateOptionalChecks(t *testing.T) {
	fp := fakeprovider.New(t).WithPlatform(provider.ForgeGitHub)
	result, _ := appvalidate.Prerequisites(context.Background(), appvalidate.PrerequisitesDeps{
		GitRepo:  stubGit{verifyOK: true, tagSHA: "abc"},
		Provider: fp,
	}, io.Discard, output.Annotator{}, appvalidate.PrerequisitesInput{
		RefType:               "tag",
		Tag:                   "v1.0.0",
		Ref:                   "refs/tags/v1.0.0",
		ReleaseToken:          "ghp_dummy",
		Repository:            "owner/repo",
		SignArtifacts:         false,
		RequiresGPGSigning:    false,
		HasMavenCentralTarget: false,
		HasCargoTarget:        false,
	})

	skipped := map[string]string{}

	for _, c := range result.Checks {
		if c.Skipped {
			skipped[c.Name] = c.SkipReason
		}
	}

	// Release authorisation no longer has its own validator — it's now
	// folded into tag-signature via the allowed_signers /
	// allowed_gpg_keys.asc files. Only the per-flag skips remain.
	for _, want := range []string{"gpg-public-key", "maven-central", "cargo"} {
		if _, ok := skipped[want]; !ok {
			t.Errorf("expected %s to be skipped (flag disabled)", want)
		}
	}
}

func TestPrerequisites_GPGCheckUsesResolvedSigningRequirement(t *testing.T) {
	t.Parallel()

	fp := fakeprovider.New(t).WithPlatform(provider.ForgeGitHub)
	result, _ := appvalidate.Prerequisites(context.Background(), appvalidate.PrerequisitesDeps{
		GitRepo:  stubGit{verifyOK: true, tagSHA: "abc"},
		Provider: fp,
	}, io.Discard, output.Annotator{}, appvalidate.PrerequisitesInput{
		RefType:            "tag",
		Tag:                "v1.0.0",
		Ref:                "refs/tags/v1.0.0",
		ReleaseToken:       "ghp_dummy",
		Repository:         "owner/repo",
		SignArtifacts:      true,
		RequiresGPGSigning: false,
	})

	found := 0

	for _, check := range result.Checks {
		if check.Name == "gpg-public-key" {
			found++

			if !check.Skipped {
				t.Fatalf("keyless artifact signing with non-GPG git signing must skip GPG key validation: %+v", check)
			}
		}
	}

	if found != 1 {
		t.Fatalf("GPG outcome count=%d", found)
	}
}

// errStubGitBoom is the git failure injected into the aggregate below.
var errStubGitBoom = errors.New("git boom") //nolint:err113 // test fixture sentinel.

// TestPrerequisites_AggregateNamesEveryFailedCheckAndKeepsItsCauses covers
// what the caller gets when several validators fail at once. The aggregate is
// what the CLI exits on, so two things have to survive it: the list of checks
// that failed, and the causes -- both so an operator can act, and so
// errs.ExitCodeFromError can classify the run instead of falling through to
// ExitCodeSoftware ("an error we did not classify, file a bug").
//
// The fixture fails three checks: a git error on tag-uniqueness, and a classic
// PAT which both release-token and bot-permissions refuse.
func TestPrerequisites_AggregateNamesEveryFailedCheckAndKeepsItsCauses(t *testing.T) {
	fp := fakeprovider.New(t).WithPlatform(provider.ForgeGitHub)

	_, err := appvalidate.Prerequisites(context.Background(), appvalidate.PrerequisitesDeps{
		GitRepo:  stubGit{uniqueTagsErr: errStubGitBoom},
		Provider: fp,
	}, io.Discard, output.Annotator{}, appvalidate.PrerequisitesInput{
		RefType:      "tag",
		Tag:          "v1.0.0",
		Ref:          "refs/tags/v1.0.0",
		ReleaseToken: "ghp_dummy",
		Repository:   "owner/repo",
	})
	if err == nil {
		t.Fatal("expected an aggregate failure")
	}

	// Every failed check named, not just the first: a summary that stopped at
	// one would send the operator round the loop again for the next.
	for _, want := range []string{"prerequisites failed", "tag-uniqueness", "release-token", "bot-permissions"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v, want it to mention %q", err, want)
		}
	}

	// The causes are joined, not flattened to text: the injected git failure
	// is still matchable, and the credential refusals still classify the run
	// as ErrPermissionDenied (exit 77) rather than an unclassified 70.
	if !errors.Is(err, errStubGitBoom) {
		t.Errorf("err = %v, want it to carry the injected git failure", err)
	}

	if !errors.Is(err, errs.ErrPermissionDenied) {
		t.Errorf("err = %v, want it to carry ErrPermissionDenied", err)
	}

	if got := errs.ExitCodeFromError(err); got == errs.ExitCodeSoftware {
		t.Errorf("aggregate exits %d (\"file a bug\") for a credential refusal", got)
	}
}

func TestPrerequisites_OutputIsOrdered(t *testing.T) {
	fp := fakeprovider.New(t).WithPlatform(provider.ForgeGitHub)

	var out bytes.Buffer

	_, _ = appvalidate.Prerequisites(context.Background(), appvalidate.PrerequisitesDeps{
		GitRepo:  stubGit{verifyOK: true, tagSHA: "abc"},
		Provider: fp,
	}, &out, output.Annotator{}, appvalidate.PrerequisitesInput{
		RefType:      "tag",
		Tag:          "v1.0.0",
		Ref:          "refs/tags/v1.0.0",
		ReleaseToken: "ghp_dummy",
		Repository:   "owner/repo",
	})

	// Even though validators ran concurrently, output is rendered in
	// the canonical order so log diffs stay deterministic.
	body := out.String()
	posRefType := strings.Index(body, "Triggered by tag")
	posTagFormat := strings.Index(body, "Validating Tag Format")

	posTagUnique := strings.Index(body, "Validating Tag Points to Unique")
	if posRefType == -1 || posTagFormat == -1 || posTagUnique == -1 {
		t.Fatalf("expected log markers in output: %q", body)
	}

	if posRefType >= posTagFormat || posTagFormat >= posTagUnique {
		t.Errorf("ordering violated: ref-type=%d tag-format=%d tag-unique=%d", posRefType, posTagFormat, posTagUnique)
	}
}

func TestPrerequisites_RequiresDeps(t *testing.T) {
	_, err := appvalidate.Prerequisites(context.Background(), appvalidate.PrerequisitesDeps{}, io.Discard, output.Annotator{}, appvalidate.PrerequisitesInput{})
	// A caller that wired up no dependencies is a programming error in the
	// command layer, surfaced as ErrUsage rather than a silent empty result.
	if !errors.Is(err, errs.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}

	if !strings.Contains(err.Error(), "requires GitRepo") {
		t.Errorf("err = %v, want it to name the missing dependency", err)
	}
}
